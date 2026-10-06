package health

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Finder has a reduced PATH. Resolve the installed CLI without a login shell.
func commandPath(name string) (string, error) {
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	paths := []string{"/opt/homebrew/bin", "/usr/local/bin", filepath.Join(home, ".local/bin"), filepath.Join(home, ".npm-global/bin")}
	matches, _ := filepath.Glob(filepath.Join(home, ".nvm/versions/node/*/bin"))
	paths = append(paths, matches...)
	for _, p := range paths {
		candidate := filepath.Join(p, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", errors.New(name + " CLI not found")
}

func runCommand(ctx context.Context, workspace, name string, args ...string) ([]byte, error) {
	path, err := commandPath(name)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	prepareCommand(cmd)
	defer stopCommand(cmd)
	cmd.Dir = workspace
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "TERM=dumb", "PATH="+filepath.Dir(path)+":/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:"+os.Getenv("PATH"))
	var output bytes.Buffer
	cmd.Stdout = &limitedWriter{writer: &output, remaining: 1024 * 1024}
	cmd.Stderr = io.Discard
	err = cmd.Run()
	if ctx.Err() != nil {
		return output.Bytes(), ctx.Err()
	}
	return output.Bytes(), err
}

type limitedWriter struct {
	writer    io.Writer
	remaining int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	n := len(p)
	take := min(n, w.remaining)
	if take > 0 {
		if _, err := w.writer.Write(p[:take]); err != nil {
			return 0, err
		}
		w.remaining -= take
	}
	return n, nil
}

func safeDetail(message string) string {
	// Provider diagnostics can contain credentials. Only keep a public error class.
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "expired") || strings.Contains(lower, "needs authentication") || strings.Contains(lower, "401"):
		return "Sign-in expired or authentication is required"
	case strings.Contains(lower, "403") || strings.Contains(lower, "permission"):
		return "Access denied"
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out"):
		return "Connection timed out"
	case strings.Contains(lower, "closed") || strings.Contains(lower, "refused"):
		return "Connection closed or refused; check the service and tunnel"
	default:
		return "Connection failed; re-check or open the provider settings"
	}
}
