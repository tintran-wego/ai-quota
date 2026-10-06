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
	if status.Running || status.Verifying || status.Error == "" || s.Snapshot().Checking || s.afterAuth {
		t.Fatalf("failed login must remain retryable without claiming recovery: %+v", status)
	}
}

func TestCompletedSignInRechecksAfterAnExistingProbe(t *testing.T) {
	for _, want := range []State{OK, NeedsAuth} {
		t.Run(string(want), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			first, second := make(chan struct{}), make(chan struct{})
			releaseFirst, releaseSecond := make(chan struct{}), make(chan struct{})
			var firstOnce, secondOnce sync.Once
			var probes, logins atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				state, detail := "ok", ""
				release := releaseSecond
				if probes.Add(1) == 1 {
					close(first)
					release = releaseFirst
					state, detail = "failed", "Token expired"
				} else {
					close(second)
					if want == NeedsAuth {
						state, detail = "failed", "Token expired"
					}
				}
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				fmt.Fprintf(w, `{"generatedAt":%q,"stores":[{"store":"athena","environment":"production","state":%q,"detail":%q,"facts":{"identity":"profile:test-profile"}}]}`, time.Now().Format(time.RFC3339Nano), state, detail)
			}))
			defer func() {
				cancel()
				firstOnce.Do(func() { close(releaseFirst) })
				secondOnce.Do(func() { close(releaseSecond) })
				server.Close()
			}()
			s := New(t.TempDir(), nil)
			config := Config{ReadinessURL: server.URL, IntervalMinutes: 5}
			if err := storage.WriteJSON(filepath.Join(s.dataDir, "health-config.json"), config, 0600); err != nil {
				t.Fatal(err)
			}
			s.launchAction = func(context.Context, Action, string) error {
				logins.Add(1)
				return nil
			}
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
			a := Action{Kind: "aws-login", Target: "test-profile"}
			if err := s.RunAction(ctx, a); err != nil {
				t.Fatal(err)
			}
			if status := s.ActionStatus(a); status.Running || !status.Verifying {
				t.Fatalf("completed callback must wait for a fresh probe: %+v", status)
			}
			if err := s.RunAction(ctx, a); err != nil || logins.Load() != 1 {
				t.Fatal("verification must prevent another sign-in callback")
			}
			firstOnce.Do(func() { close(releaseFirst) })
			select {
			case <-second:
			case <-ctx.Done():
				t.Fatal("completed sign-in lost its follow-up probe")
			}
			if !s.ActionStatus(a).Verifying {
				t.Fatal("pre-login probe must not clear the completed login's verification")
			}
			if report := s.Snapshot(); !report.Checking || len(report.Checks) != 1 || report.Checks[0].State != NeedsAuth {
				t.Fatalf("login completion must not fabricate a healthy result: %+v", report)
			}
			if err := s.RunAction(ctx, a); err != nil || logins.Load() != 1 {
				t.Fatal("queued fresh probe must keep the sign-in button blocked")
			}
			secondOnce.Do(func() { close(releaseSecond) })
			select {
			case <-checkDone:
			case <-ctx.Done():
				t.Fatal("follow-up probe did not finish")
			}
			if status := s.ActionStatus(a); status.Running || status.Verifying || status.Error != "" {
				t.Fatalf("fresh probe did not clear verification progress: %+v", status)
			}
			if report := s.Snapshot(); report.Checking || len(report.Checks) != 1 || report.Checks[0].State != want {
				t.Fatalf("only the fresh probe may determine recovery: %+v", report)
			}
		})
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
