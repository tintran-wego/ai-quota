package health

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/chuongtrh/ai-quota/internal/storage"
	"github.com/creack/pty"
)

var targetPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.:/-]*$`)

var cloudTargetPattern = regexp.MustCompile(`^claude\.ai [A-Za-z0-9_][A-Za-z0-9_ .:/-]*$`)

func validTarget(target string) bool { return targetPattern.MatchString(target) }
func shellQuote(s string) string     { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

// LaunchAction runs the provider's browser login and waits for its callback.
// Only SSH tunnel commands require a visible Terminal.
// AIQuota does not read, copy, clear, or store OAuth credentials.
func LaunchAction(ctx context.Context, a Action, dataDir string) error {
	return launchAction(ctx, a, dataDir, openAction)
}

type actionOpener func(context.Context, ...string) error

func openAction(ctx context.Context, args ...string) error {
	if err := exec.CommandContext(ctx, "/usr/bin/open", args...).Run(); err != nil {
		return errors.New("The requested application could not be opened")
	}
	return nil
}

func launchAction(ctx context.Context, a Action, dataDir string, open actionOpener) error {
	var name string
	var args []string
	switch a.Kind {
	case "mcp-login":
		if a.Host != "claude" && a.Host != "codex" {
			return fmt.Errorf("unsupported host")
		}
		name = a.Host
		args = []string{"mcp", "login", a.Target}
	case "open-app":
		if a.Target != "Docker" && a.Target != "Pritunl" {
			return fmt.Errorf("unsupported app")
		}
		return open(ctx, "-a", a.Target)
	case "tunnel":
		if a.Target != "production" && a.Target != "staging" {
			return fmt.Errorf("unsupported tunnel")
		}
		name = "zsh"
		args = []string{"-lic", "blackhole_us_" + a.Target}
	case "gcloud-adc":
		name = "gcloud"
		args = []string{"auth", "application-default", "login", "--launch-browser", "--quiet"}
	case "cognito-login":
		name = "node"
		args = []string{filepath.Join(a.Workspace, "ai/skills/pennyworth-tester/scripts/refresh-storage-state.mjs")}
	case "aws-login":
		name = "aws"
		args = []string{"sso", "login", "--profile", a.Target}
	default:
		return fmt.Errorf("unsupported repair action")
	}
	targetOK := validTarget(a.Target) || (a.Kind == "mcp-login" && a.Host == "claude" && cloudTargetPattern.MatchString(a.Target))
	if ((a.Kind == "mcp-login" || a.Kind == "aws-login") && !targetOK) || (a.Target != "" && !targetOK) {
		return fmt.Errorf("invalid action target")
	}
	workspace := a.Workspace
	if workspace == "" {
		workspace, _ = os.UserHomeDir()
	}
	if info, err := os.Stat(workspace); err != nil || !info.IsDir() {
		return fmt.Errorf("workspace unavailable")
	}
	if a.Kind != "tunnel" {
		return runBrowserCommand(ctx, workspace, name, args...)
	}
	path, err := commandPath(name)
	if err != nil {
		return errors.New("The tunnel command is unavailable")
	}
	command := shellQuote(path)
	for _, arg := range args {
		command += " " + shellQuote(arg)
	}
	// Existing tunnel functions live in the user's interactive shell.
	script := "#!/bin/sh\ncd " + shellQuote(workspace) + " || exit 1\nexport PATH=" + shellQuote(filepath.Dir(path)+":/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin") + "\n" + command + "\nresult=$?\nif [ \"$result\" -eq 0 ]; then touch " + shellQuote(filepath.Join(dataDir, "auth-completed")) + "; fi\nprintf '\\nReturn to AIQuota and choose Check now. Press Enter to close.\\n'\nread answer\nexit \"$result\"\n"
	digest := sha256.Sum256([]byte(a.Kind + "/" + a.Host + "/" + a.Target + "/" + workspace))
	file := filepath.Join(dataDir, "bin", fmt.Sprintf("aiquota-tunnel-%x.command", digest[:8]))
	if err := storage.WriteFileAtomic(file, []byte(script), 0o700); err != nil {
		return err
	}
	return open(ctx, "-a", "Terminal", file)
}

// Supported login CLIs open the browser and update their own credential stores.
// Output can contain callback URLs and tokens, so it never reaches logs or disk.
func runBrowserCommand(ctx context.Context, workspace, name string, args ...string) error {
	if info, err := os.Stat(workspace); err != nil || !info.IsDir() {
		return errors.New("workspace unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 11*time.Minute)
	defer cancel()
	path, err := commandPath(name)
	if err != nil {
		return fmt.Errorf("%s sign-in is unavailable; install or update its CLI", name)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	prepareCommand(cmd)
	defer stopCommand(cmd)
	cmd.Dir = workspace
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "TERM=dumb", "PATH="+filepath.Dir(path)+":/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:"+os.Getenv("PATH"))
	if name == "node" {
		// The probe reads this maintained checkout's store, not an inherited override.
		cmd.Env = append(cmd.Env, "PENNYWORTH_STATE_FILE="+filepath.Join(workspace, "ai/skills/pennyworth-tester/.auth/state.json"))
	}
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if name == "claude" {
		err = runTTYBrowserCommand(cmd)
	} else {
		err = cmd.Run()
	}
	if ctx.Err() != nil {
		return signInContextError{cause: ctx.Err()}
	}
	if err != nil {
		return errors.New("Sign-in did not complete; check the provider account and try again")
	}
	return nil
}

type signInContextError struct{ cause error }

func (e signInContextError) Error() string {
	if errors.Is(e.cause, context.DeadlineExceeded) {
		return "Sign-in timed out. Try again."
	}
	return "Sign-in cancelled."
}

func (e signInContextError) Unwrap() error { return e.cause }

// Claude cancels its OAuth callback when stdin is not a TTY. A private PTY
// provides that host requirement without a visible Terminal or a saved transcript.
func runTTYBrowserCommand(cmd *exec.Cmd) error {
	master, slave, err := pty.Open()
	if err != nil {
		return err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	// Setsid makes the CLI the group leader. prepareCommand's cancellation still
	// kills that group, including callback servers and other child processes.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	drained := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, master)
		close(drained)
	}()
	err = cmd.Run()
	_ = slave.Close()
	_ = master.Close()
	<-drained
	return err
}
