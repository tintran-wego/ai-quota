package health

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chuongtrh/ai-quota/internal/storage"
)

func TestSignInPreventsDuplicateCallbacksAndRetainsFailure(t *testing.T) {
	s := New(t.TempDir(), nil)
	started, finish := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s.launchAction = func(context.Context, Action, string) error {
		calls.Add(1)
		close(started)
		<-finish
		return errors.New("Sign-in did not complete. Try again.")
	}
	a := Action{Kind: "mcp-login", Host: "claude", Target: "example", Workspace: "/workspace-a"}
	done := make(chan error, 1)
	go func() { done <- s.RunAction(context.Background(), a) }()
	<-started
	if !s.ActionStatus(a).Running {
		t.Fatal("callback wait must remain visible")
	}
	a.Workspace = "/workspace-b"
	if err := s.RunAction(context.Background(), a); err != nil || calls.Load() != 1 {
		t.Fatal("duplicate workspace click started another callback")
	}
	close(finish)
	if err := <-done; err == nil {
		t.Fatal("login failure was lost")
	}
	status := s.ActionStatus(a)
	if status.Running || status.Error == "" || s.Snapshot().Checking || s.afterAuth {
		t.Fatalf("failed login must remain retryable without claiming recovery: %+v", status)
	}
}

func TestCompletedSignInRechecksAfterAnExistingProbe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state, detail := "ok", ""
		if calls.Add(1) == 1 {
			close(first)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			state, detail = "failed", "Token expired"
		}
		fmt.Fprintf(w, `{"generatedAt":%q,"stores":[{"store":"athena","environment":"production","state":%q,"detail":%q}]}`, time.Now().Format(time.RFC3339Nano), state, detail)
	}))
	defer func() {
		cancel()
		releaseOnce.Do(func() { close(release) })
		server.Close()
	}()
	s := New(t.TempDir(), nil)
	config := Config{ReadinessURL: server.URL, IntervalMinutes: 5}
	if err := storage.WriteJSON(filepath.Join(s.dataDir, "health-config.json"), config, 0600); err != nil {
		t.Fatal(err)
	}
	s.launchAction = func(context.Context, Action, string) error { return nil }
	checkDone := make(chan struct{})
	go func() {
		s.Check(ctx)
		close(checkDone)
	}()
	select {
	case <-first:
	case <-ctx.Done():
		t.Fatal("first probe did not start")
	}
	if err := s.RunAction(ctx, Action{Kind: "aws-login", Target: "test-profile"}); err != nil {
		t.Fatal(err)
	}
	releaseOnce.Do(func() { close(release) })
	for {
		report := s.Snapshot()
		if calls.Load() >= 2 && !report.Checking && len(report.Checks) == 1 && report.Checks[0].State == OK {
			select {
			case <-checkDone:
			case <-ctx.Done():
				t.Fatal("follow-up probe did not finish")
			}
			return
		}
		select {
		case <-s.Updates:
		case <-ctx.Done():
			t.Fatalf("completed sign-in lost its follow-up probe: calls=%d report=%+v", calls.Load(), report)
		}
	}
}

func TestAppShutdownWaitsForSignInCleanup(t *testing.T) {
	s := New(t.TempDir(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, cleaned := make(chan struct{}), make(chan struct{})
	s.launchAction = func(ctx context.Context, _ Action, _ string) error {
		close(started)
		<-ctx.Done()
		close(cleaned)
		return ctx.Err()
	}
	a := Action{Kind: "mcp-login", Host: "claude", Target: "example"}
	go func() { _ = s.RunAction(ctx, a) }()
	<-started
	cancel()
	s.WaitActions()
	select {
	case <-cleaned:
	default:
		t.Fatal("app exited before its callback process was cleaned up")
	}
	if s.ActionStatus(a).Running {
		t.Fatal("sign-in was still active after shutdown")
	}
	if err := s.RunAction(context.Background(), a); !errors.Is(err, context.Canceled) {
		t.Fatal("queued sign-in restarted during shutdown")
	}
}
