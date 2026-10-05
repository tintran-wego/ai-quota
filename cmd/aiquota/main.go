package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/chuongtrh/ai-quota/internal/appcore"
	"github.com/chuongtrh/ai-quota/internal/config"
	"github.com/chuongtrh/ai-quota/internal/health"
	"github.com/chuongtrh/ai-quota/internal/notify"
	"github.com/chuongtrh/ai-quota/internal/provider/antigravity"
	"github.com/chuongtrh/ai-quota/internal/provider/claude"
	"github.com/chuongtrh/ai-quota/internal/provider/codex"
	"github.com/chuongtrh/ai-quota/internal/tray"
)

var version = "dev"

func main() {
	paths, err := config.DefaultPaths()
	if err != nil {
		fatal(err)
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case antigravity.BridgeFlag:
			if err := antigravity.RunBridge(paths, os.Stdin, os.Stdout); err != nil {
				os.Exit(1)
			}
			return
		case claude.BridgeFlag:
			if err := claude.RunBridge(paths, os.Stdin, os.Stdout); err != nil {
				// Claude Code still receives a useful status line; the failure is signaled by the exit code.
				os.Exit(1)
			}
			return
		case "--version":
			fmt.Println(version)
			return
		}
	}
	if err := paths.Ensure(); err != nil {
		fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		fatal(err)
	}

	runtime.LockOSThread()
	service := appcore.New(paths, version)
	codexInstaller := codex.Installer{Paths: paths}
	claudeInstaller := claude.Installer{Paths: paths, Executable: executable}
	antigravityInstaller := antigravity.Installer{Paths: paths, Executable: executable}
	if connected, err := claudeInstaller.IsConnected(); err == nil && connected {
		if err := claudeInstaller.Connect(); err != nil {
			fmt.Fprintln(os.Stderr, "Update Claude bridge:", err)
		}
	}
	if connected, err := antigravityInstaller.IsConnected(); err == nil && connected {
		if err := antigravityInstaller.Connect(); err != nil {
			fmt.Fprintln(os.Stderr, "Update Antigravity bridge:", err)
		}
	}
	tray.New(service, health.New(paths.DataDir, notify.Send), codexInstaller, claudeInstaller, antigravityInstaller, version).Run()
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, "AI quota:", err)
	os.Exit(1)
}
