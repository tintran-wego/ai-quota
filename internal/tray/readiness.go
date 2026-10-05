package tray

import (
	"fmt"
	"hash/fnv"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/chuongtrh/ai-quota/internal/desktop"
	"github.com/chuongtrh/ai-quota/internal/health"
	"github.com/chuongtrh/ai-quota/internal/model"
)

func badgeTitle(report health.Report, remaining int, available bool) string {
	if n := len(health.Tasks(report)); n > 0 {
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
	view := readinessView(report, statuses, time.Now())
	a.healthSummary.SetTitle(view.Summary)
	a.actions = map[int]health.Action{}
	for i, task := range health.Tasks(report) {
		if task.Check.Action == nil {
			continue
		}
		hash := fnv.New32a()
		_, _ = hash.Write([]byte(task.Key))
		id := 1000 + int(hash.Sum32()&0x3fffffff)
		for {
			if _, exists := a.actions[id]; !exists {
				break
			}
			id++
		}
		a.actions[id] = *task.Check.Action
		view.Tasks[i].Action.ID = id
		applyActionStatus(&view.Tasks[i], a.health.ActionStatus(*task.Check.Action))
	}
	desktop.Update(view)
}

func applyActionStatus(task *desktop.Task, status health.ActionStatus) {
	if status.Running {
		task.Action.Title = "Signing in…"
		task.Action.Disabled = true
		task.Detail = "Complete sign-in in the browser. This connection will be checked again."
	} else if status.Error != "" {
		task.Detail = status.Error
	}
}

func readinessView(report health.Report, statuses map[model.Provider]model.ProviderStatus, now time.Time) desktop.View {
	tasks := health.Tasks(report)
	summary := "No confirmed problems"
	if len(tasks) == 1 {
		summary = "1 problem to fix"
	} else if len(tasks) > 1 {
		summary = fmt.Sprintf("%d problems to fix", len(tasks))
	}
	if n := unknownConnections(report); n > 0 {
		summary += fmt.Sprintf(" · %d unverified", n)
	}
	if report.Checking {
		summary += " · checking"
	} else if report.CheckedAt.IsZero() {
		summary = "Waiting for connection checks"
	}
	view := desktop.View{
		Summary: summary,
		Tasks:   []desktop.Task{},
		Buttons: []desktop.Button{{ID: 501, Title: "Add workspace"}, {ID: 502, Title: "Monitor settings"}, {ID: 506, Title: "Open Codex"}, {ID: 507, Title: "Open Claude"}},
	}
	for _, task := range tasks {
		title, detail, action := taskText(task)
		view.Tasks = append(view.Tasks, desktop.Task{Title: title, Detail: detail, Scope: taskScopes(task), Action: desktop.Button{Title: action}})
	}
	var quota, details strings.Builder
	fmt.Fprintf(&details, "Last connection check: %s\n\n", timestamp(report.CheckedAt))
	for _, provider := range providerOrder {
		status := statuses[provider]
		if len(status.Windows) == 0 && status.UpdatedAt.IsZero() && status.Error == "" {
			continue
		}
		windows := []string{}
		for _, window := range status.Windows {
			text := window.DisplayName() + " waiting for update"
			if window.ResetsAt.After(now) {
				text = fmt.Sprintf("%s %d%% left", window.DisplayName(), window.RoundedRemainingPercent())
			}
			windows = append(windows, text)
			fmt.Fprintf(&details, "%s / %s: %s, %s\n", provider.DisplayName(), window.DisplayName(), text, model.FormatReset(now, window.ResetsAt))
		}
		if len(windows) == 0 {
			windows = append(windows, "waiting for quota")
		}
		if len(windows) > 2 {
			windows = append(windows[:2], fmt.Sprintf("+%d in details", len(windows)-2))
		}
		fmt.Fprintf(&quota, "%s   %s\n", provider.DisplayName(), strings.Join(windows, " · "))
		fmt.Fprintf(&details, "Received: %s\n", timestamp(status.UpdatedAt))
		if status.Error != "" {
			fmt.Fprintln(&details, status.Error)
		}
	}
	if quota.Len() == 0 {
		fmt.Fprintln(&quota, "Waiting for quota data")
	}
	fmt.Fprintln(&details, "\nClaude quota comes from the status line and can lag the account page.\nExpired quota waits for a new report; it is not assumed to be full.\n\nCONNECTION CHECKS")
	for _, check := range report.Checks {
		fmt.Fprintf(&details, "\n[%s] %s / %s\n%s\nScope: %s\nChecked: %s\n", check.State, check.Host, check.Name, check.Detail, check.Scope, timestamp(check.CheckedAt))
	}
	fmt.Fprintln(&details, "\nMCP checks verify a handshake or tool catalog. Backend access uses read-only probes.\nApp connectors without a supported probe remain unverified.")
	view.Quota = strings.TrimSpace(quota.String())
	view.Details = details.String()
	return view
}

func unknownConnections(report health.Report) int {
	groups := map[string]bool{}
	for _, check := range report.Checks {
		if check.State == health.Unknown {
			groups[check.Host+"\x00"+check.Name] = true
		}
	}
	return len(groups)
}

func taskText(task health.Task) (title, detail, action string) {
	check := task.Check
	title = check.Name + " · " + check.Host
	detail = "Connection failed. Open details to inspect the probe."
	if check.State == health.NeedsAuth {
		detail = "Sign-in expired or required."
	}
	if check.Action == nil {
		switch check.Name {
		case "AWS session", "aws-sso":
			title, detail = "AWS session", "Choose the intended AWS profile, then run aws sso login."
		case "Local readiness service":
			title, detail = "fs-log-data dashboard", "Start the local fs-log-data service, then check again."
		default:
			if strings.HasPrefix(check.Name, "jenkins") {
				title, detail = "Jenkins access", "Check the VPN and tunnel, then retry Jenkins."
				if check.State == health.NeedsAuth || strings.Contains(check.Detail, "credential is missing") {
					detail = "Refresh the Jenkins token in Hub onboarding."
				}
			} else if check.Name == "redis" {
				title, detail = "Redis access", "Check the VPN and the tunnel for this environment."
			} else if check.Name == "api" {
				title, detail = "API access", "Check the VPN, tunnel, and API endpoint."
			}
		}
		return title, detail, "Show details"
	}
	switch repair := check.Action; repair.Kind {
	case "mcp-login":
		title, action = strings.TrimPrefix(repair.Target, "claude.ai ")+" · "+check.Host, "Sign in"
	case "aws-login":
		title, action = "AWS · "+repair.Target, "Sign in"
	case "gcloud-adc":
		title, action = "BigQuery access", "Sign in"
	case "cognito-login":
		title, action = "Pennyworth access", "Sign in"
	case "open-app":
		title, action = repair.Target, "Open "+repair.Target
		detail = "Required app is stopped or unavailable."
		if repair.Target == "Pritunl" {
			title, action, detail = "VPN", "Open VPN", "Connect the VPN, then check again."
		}
	case "tunnel":
		title, action = "Tunnel · "+repair.Target, "Start tunnel"
		detail = "Required tunnel is unreachable."
	default:
		action = "Fix connection"
	}
	return title, detail, action
}

func taskScopes(task health.Task) string {
	if len(task.Scopes) == 0 {
		return ""
	}
	if len(task.Scopes) > 2 {
		return fmt.Sprintf("Affects %d workspaces or environments", len(task.Scopes))
	}
	scopes := make([]string, len(task.Scopes))
	for i, scope := range task.Scopes {
		if filepath.IsAbs(scope) {
			scope = filepath.Base(scope)
		}
		scopes[i] = scope
	}
	return strings.Join(scopes, ", ")
}

func timestamp(value time.Time) string {
	if value.IsZero() {
		return "not yet checked"
	}
	return value.Local().Format("Jan 2 15:04")
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
