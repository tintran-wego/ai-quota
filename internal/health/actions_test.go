package health

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeLoginCLI(t *testing.T, name, script string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+script+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func TestSignInActionsRunBrowserCLIWithoutTerminal(t *testing.T) {
	for _, tc := range []struct {
		name string
		a    Action
		args string
	}{
		{"aws", Action{Kind: "aws-login", Target: "flights_us_production"}, "sso\nlogin\n--profile\nflights_us_production\n"},
		{"gcloud", Action{Kind: "gcloud-adc"}, "auth\napplication-default\nlogin\n--launch-browser\n--quiet\n"},
		{"codex", Action{Kind: "mcp-login", Host: "codex", Target: "example"}, "mcp\nlogin\nexample\n"},
		{"claude", Action{Kind: "mcp-login", Host: "claude", Target: "claude.ai Example"}, "mcp\nlogin\nclaude.ai Example\n"},
		{"node", Action{Kind: "cognito-login"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace, dataDir := t.TempDir(), t.TempDir()
			log := filepath.Join(t.TempDir(), "args")
			t.Setenv("AIQUOTA_TEST_ARGS", log)
			fakeLoginCLI(t, tc.name, "printf '%s\\n' \"$@\" > \"$AIQUOTA_TEST_ARGS\"")
			a := tc.a
			a.Workspace = workspace
			if tc.name == "node" {
				tc.args = filepath.Join(workspace, "ai/skills/pennyworth-tester/scripts/refresh-storage-state.mjs") + "\n"
			}
			open := func(context.Context, ...string) error {
				t.Fatal("sign-in must not open Terminal")
				return nil
			}
			if err := launchAction(context.Background(), a, dataDir, open); err != nil {
				t.Fatal(err)
			}
			args, err := os.ReadFile(log)
			if err != nil || string(args) != tc.args {
				t.Fatalf("wrong browser CLI arguments: %q, %v", args, err)
			}
			entries, err := os.ReadDir(dataDir)
			if err != nil || len(entries) != 0 {
				t.Fatal("sign-in must not write command scripts or completion markers")
			}
		})
	}
}

func TestOnlyTunnelActionsOpenTerminal(t *testing.T) {
	for _, target := range []string{"production", "staging"} {
		t.Run(target, func(t *testing.T) {
			dataDir := t.TempDir()
			opened := false
			open := func(_ context.Context, args ...string) error {
				opened = true
				if len(args) != 3 || args[0] != "-a" || args[1] != "Terminal" {
					t.Fatalf("wrong tunnel application: %v", args)
				}
				script, err := os.ReadFile(args[2])
				if err != nil || !strings.Contains(string(script), "blackhole_us_"+target) {
					t.Fatalf("missing supported tunnel command: %v", err)
				}
				return nil
			}
			if err := launchAction(context.Background(), Action{Kind: "tunnel", Target: target, Workspace: t.TempDir()}, dataDir, open); err != nil {
				t.Fatal(err)
			}
			if !opened {
				t.Fatal("tunnel must open Terminal")
			}
		})
	}
}

func TestBrowserSignInWaitsForProviderCompletion(t *testing.T) {
	started, completed := filepath.Join(t.TempDir(), "started"), filepath.Join(t.TempDir(), "completed")
	t.Setenv("AIQUOTA_TEST_STARTED", started)
	t.Setenv("AIQUOTA_TEST_COMPLETED", completed)
	fakeLoginCLI(t, "aws", "touch \"$AIQUOTA_TEST_STARTED\"\nwhile [ ! -f \"$AIQUOTA_TEST_COMPLETED\" ]; do /bin/sleep 0.02; done")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	workspace, dataDir := t.TempDir(), t.TempDir()
	go func() {
		done <- LaunchAction(ctx, Action{Kind: "aws-login", Target: "example", Workspace: workspace}, dataDir)
	}()
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("sign-in returned before callback: %v", err)
		case <-ctx.Done():
			t.Fatal("fake provider did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	select {
	case err := <-done:
		t.Fatalf("sign-in returned while provider was waiting: %v", err)
	default:
	}
	if err := os.WriteFile(completed, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestBrowserSignInCancellationStopsCommand(t *testing.T) {
	for _, host := range []string{"codex", "claude"} {
		t.Run(host, func(t *testing.T) {
			fakeLoginCLI(t, host, "while :; do /bin/sleep 10; done")
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			started := time.Now()
			err := LaunchAction(ctx, Action{Kind: "mcp-login", Host: host, Target: "example", Workspace: t.TempDir()}, t.TempDir())
			if !errors.Is(err, context.DeadlineExceeded) || err.Error() != "Sign-in timed out. Try again." || time.Since(started) > 2*time.Second {
				t.Fatalf("cancellation did not stop browser command: %v", err)
			}
		})
	}
}

func TestCancelledBrowserSignInHasPublicError(t *testing.T) {
	fakeLoginCLI(t, "aws", "exit 0")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := LaunchAction(ctx, Action{Kind: "aws-login", Target: "example", Workspace: t.TempDir()}, t.TempDir())
	if !errors.Is(err, context.Canceled) || err.Error() != "Sign-in cancelled." {
		t.Fatalf("wrong public cancellation error: %v", err)
	}
}

func TestClaudeBrowserSignInHasPrivateTTY(t *testing.T) {
	fakeLoginCLI(t, "claude", "[ -t 0 ] && [ -t 1 ] && [ -t 2 ]")
	if err := LaunchAction(context.Background(), Action{Kind: "mcp-login", Host: "claude", Target: "example", Workspace: t.TempDir()}, t.TempDir()); err != nil {
		t.Fatalf("Claude requires a private TTY for its OAuth callback: %v", err)
	}
}

func TestCognitoBrowserSignInUsesMaintainedCredentialStore(t *testing.T) {
	workspace := t.TempDir()
	want := filepath.Join(workspace, "ai/skills/pennyworth-tester/.auth/state.json")
	t.Setenv("PENNYWORTH_STATE_FILE", "/tmp/unrelated-state.json")
	t.Setenv("AIQUOTA_TEST_EXPECTED_STATE", want)
	fakeLoginCLI(t, "node", "[ ! -t 0 ] && [ \"$PENNYWORTH_STATE_FILE\" = \"$AIQUOTA_TEST_EXPECTED_STATE\" ]")
	if err := LaunchAction(context.Background(), Action{Kind: "cognito-login", Workspace: workspace}, t.TempDir()); err != nil {
		t.Fatalf("Cognito must use its existing callback detection and maintained store: %v", err)
	}
}

func TestFailedBrowserSignInDoesNotExposeOutputOrMarkSuccess(t *testing.T) {
	fakeLoginCLI(t, "gcloud", "printf 'private-token-secret\\n'\nprintf 'https://example.com/callback?code=private-code\\n' >&2\nexit 7")
	dataDir := t.TempDir()
	err := LaunchAction(context.Background(), Action{Kind: "gcloud-adc", Workspace: t.TempDir()}, dataDir)
	if err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "callback") || strings.Contains(err.Error(), "exit status") {
		t.Fatalf("provider diagnostics must not reach the public error: %v", err)
	}
	entries, readErr := os.ReadDir(dataDir)
	if readErr != nil || len(entries) != 0 {
		t.Fatal("failed sign-in must not create completion files")
	}
}

func TestInvalidActionDoesNotLaunchACommand(t *testing.T) {
	for _, a := range []Action{
		{Kind: "aws-login", Target: "--profile"},
		{Kind: "mcp-login", Host: "codex", Target: "example;command"},
		{Kind: "mcp-login", Host: "other", Target: "example"},
		{Kind: "tunnel", Target: "other"},
		{Kind: "open-app", Target: "Terminal"},
		{Kind: "unknown"},
	} {
		a.Workspace = t.TempDir()
		open := func(context.Context, ...string) error {
			t.Fatal("invalid action must not open an application")
			return nil
		}
		if err := launchAction(context.Background(), a, t.TempDir(), open); err == nil {
			t.Fatalf("accepted invalid action: %+v", a)
		}
	}
}
