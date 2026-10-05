package health

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/chuongtrh/ai-quota/internal/storage"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var targetPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.:/-]*$`)

var cloudTargetPattern = regexp.MustCompile(`^claude\.ai [A-Za-z0-9_][A-Za-z0-9_ .:/-]*$`)

func validTarget(target string) bool { return targetPattern.MatchString(target) }
func shellQuote(s string) string     { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

// LaunchAction opens the host's supported interactive login in Terminal.
// AIQuota does not read, copy, clear, or store OAuth credentials.
func LaunchAction(ctx context.Context, a Action, dataDir string) error {
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
		return exec.CommandContext(ctx, "/usr/bin/open", "-a", a.Target).Run()
	case "tunnel":
		if a.Target != "production" && a.Target != "staging" {
			return fmt.Errorf("unsupported tunnel")
		}
		name = "zsh"
		args = []string{"-lic", "blackhole_us_" + a.Target}
	case "gcloud-adc":
		name = "gcloud"
		args = []string{"auth", "application-default", "login"}
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
	path, err := commandPath(name)
	if err != nil {
		return err
	}
	command := shellQuote(path)
	for _, arg := range args {
		command += " " + shellQuote(arg)
	}
	workspace := a.Workspace
	if workspace == "" {
		workspace, _ = os.UserHomeDir()
	}
	if info, err := os.Stat(workspace); err != nil || !info.IsDir() {
		return fmt.Errorf("workspace unavailable")
	}
	// A .command file lets Terminal own the interactive browser callback and consent flow.
	script := "#!/bin/sh\ncd " + shellQuote(workspace) + " || exit 1\nexport PATH=" + shellQuote(filepath.Dir(path)+":/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin") + "\n" + command + "\nresult=$?\nif [ \"$result\" -eq 0 ]; then touch " + shellQuote(filepath.Join(dataDir, "auth-completed")) + "; fi\nprintf '\\nReturn to AIQuota and choose Check now. Press Enter to close.\\n'\nread answer\nexit \"$result\"\n"
	digest := sha256.Sum256([]byte(a.Kind + "/" + a.Host + "/" + a.Target + "/" + workspace))
	file := filepath.Join(dataDir, "bin", fmt.Sprintf("aiquota-auth-%x.command", digest[:8]))
	if err := storage.WriteFileAtomic(file, []byte(script), 0o700); err != nil {
		return err
	}
	return exec.CommandContext(ctx, "/usr/bin/open", "-a", "Terminal", file).Run()
}
