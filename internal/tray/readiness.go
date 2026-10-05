package tray

import (
	"fmt"
	"github.com/chuongtrh/ai-quota/internal/desktop"
	"github.com/chuongtrh/ai-quota/internal/health"
	"github.com/chuongtrh/ai-quota/internal/model"
	"hash/fnv"
	"os/exec"
	"strings"
	"time"
)

func badgeTitle(report health.Report, remaining int, available bool) string {
	if n := report.Issues(); n > 0 {
		return fmt.Sprintf("AI !%d", n)
	}
	if report.Unknowns() > 0 || report.CheckedAt.IsZero() {
		if available {
			return fmt.Sprintf("%d%% ?", remaining)
		}
		return "AI ?"
	}
	if available {
		return fmt.Sprintf("%d%%", remaining)
	}
	return "AI"
}

func (a *App) updateReadiness(statuses map[model.Provider]model.ProviderStatus) {
	report := a.health.Snapshot()
	summary := fmt.Sprintf("Connections: %d issues, %d unknown", report.Issues(), report.Unknowns())
	if report.Checking {
		summary += " (checking)"
	}
	a.healthSummary.SetTitle(summary)
	body := strings.Builder{}
	fmt.Fprintln(&body, "QUOTA")
	for _, provider := range providerOrder {
		status := statuses[provider]
		fmt.Fprintln(&body, "\n"+provider.DisplayName())
		if len(status.Windows) == 0 {
			fmt.Fprintln(&body, "  Waiting for provider data")
		} else {
			for _, window := range status.Windows {
				fmt.Fprintf(&body, "  %s: %d%% left, %s\n", window.DisplayName(), window.RoundedRemainingPercent(), model.FormatReset(time.Now(), window.ResetsAt))
			}
		}
		if provider == model.ProviderClaudeCode {
			fmt.Fprintf(&body, "  Source: Claude Code status line. Last quota change: %s\n  Session reports can lag the account usage page.\n", status.UpdatedAt.Local().Format("Jan 2 15:04"))
		} else {
			fmt.Fprintf(&body, "  Received: %s\n", status.UpdatedAt.Local().Format("Jan 2 15:04"))
		}
		if status.Error != "" {
			fmt.Fprintln(&body, "  "+status.Error)
		}
	}
	fmt.Fprintf(&body, "\nCONNECTIONS\n%s\nLast check: %s\n", summary, report.CheckedAt.Local().Format("Jan 2 15:04"))
	buttons := []desktop.Button{{ID: 500, Title: "Check now"}, {ID: 501, Title: "Monitor workspace"}, {ID: 502, Title: "Monitor settings"}, {ID: 506, Title: "Open Codex"}, {ID: 507, Title: "Open Claude"}}
	for _, check := range report.Checks {
		fmt.Fprintf(&body, "\n[%s] %s / %s\n  %s\n  Scope: %s\n  Checked: %s\n", check.State, check.Host, check.Name, check.Detail, check.Scope, check.CheckedAt.Local().Format("Jan 2 15:04"))
		if check.Action != nil && check.NeedsAttention() {
			action := *check.Action
			hash := fnv.New32a()
			fmt.Fprintf(hash, "%s|%s|%s|%s", action.Kind, action.Host, action.Target, action.Workspace)
			id := 1000 + int(hash.Sum32()&0x3fffffff)
			if existing, ok := a.actions[id]; ok && existing != action {
				continue
			}
			a.actions[id] = action
			buttons = append(buttons, desktop.Button{ID: id, Title: "Fix: " + check.Name + " (" + check.Host + ")"})
		}
	}
	fmt.Fprintln(&body, "\nMCP checks verify the handshake or tool catalog. They do not run arbitrary tools.\nBackend access is verified by the fs-log-data read-only probes.\nApp-managed connectors without a supported probe remain Unknown.")
	desktop.Update(body.String(), buttons)
}

func (a *App) nativeAction(id int) {
	switch id {
	case 500:
		go a.service.Refresh(a.ctx)
		go a.health.Check(a.ctx)
	case 502:
		go func() { _ = exec.CommandContext(a.ctx, "/usr/bin/open", "-a", "TextEdit", a.health.ConfigPath()).Run() }()
	case 504:
		a.updateMenu()
	case 506:
		go func() { _ = exec.CommandContext(a.ctx, "/usr/bin/open", "-b", "com.openai.codex").Run() }()
	case 507:
		go func() { _ = exec.CommandContext(a.ctx, "/usr/bin/open", "-b", "com.anthropic.claudefordesktop").Run() }()
	default:
		action, exists := a.actions[id]
		if !exists {
			return
		}
		go func() {
			if err := a.health.RunAction(a.ctx, action); err != nil {
				a.reportActionError(err)
			}
		}()
	}
}
