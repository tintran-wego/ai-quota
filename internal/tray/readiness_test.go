package tray

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chuongtrh/ai-quota/internal/health"
	"github.com/chuongtrh/ai-quota/internal/model"
)

func TestBadgeReflectsKnownBadAndRecovery(t *testing.T) {
	report := health.Report{CheckedAt: time.Now(), Checks: []health.Check{{State: health.NeedsAuth}}}
	if got := badgeTitle(report, 6, true); got != "AI !1" {
		t.Fatal(got)
	}
	report.Checks[0].State = health.OK
	if got := badgeTitle(report, 6, true); got != "6%" {
		t.Fatal(got)
	}
	report.Checks[0].State = health.Unknown
	if got := badgeTitle(report, 6, true); got != "6% ?" {
		t.Fatal(got)
	}
}

func TestBadgeCountsRepairsRatherThanWorkspaceDuplicates(t *testing.T) {
	report := health.Report{CheckedAt: time.Now()}
	for _, workspace := range []string{"/repo-a", "/repo-b", "/repo-c"} {
		report.Checks = append(report.Checks, health.Check{ID: workspace, Host: "Claude Code", Name: "slack", Scope: workspace, State: health.NeedsAuth, Action: &health.Action{Kind: "mcp-login", Host: "claude", Target: "slack", Workspace: workspace}})
	}
	if got := badgeTitle(report, 2, true); got != "AI !1" {
		t.Fatalf("one login should produce one badge task, got %q", got)
	}
}

func TestOverviewShowsRepairAndHidesHealthyDiagnostics(t *testing.T) {
	now := time.Now()
	report := health.Report{CheckedAt: now, Checks: []health.Check{
		{ID: "claude/a/slack", Host: "Claude Code", Name: "slack", State: health.NeedsAuth, Scope: "/work/repo-a", Detail: "Raw authentication diagnostics", Action: &health.Action{Kind: "mcp-login", Host: "claude", Target: "slack", Workspace: "/work/repo-a"}},
		{ID: "claude/b/slack", Host: "Claude Code", Name: "slack", State: health.NeedsAuth, Scope: "/work/repo-b", Action: &health.Action{Kind: "mcp-login", Host: "claude", Target: "slack", Workspace: "/work/repo-b"}},
		{ID: "good", Host: "Codex", Name: "healthy-service", State: health.OK, Detail: "Handshake diagnostic"},
		{ID: "app-a", Host: "Codex", Name: "App connectors", State: health.Unknown, Scope: "/work/repo-a"},
		{ID: "app-b", Host: "Codex", Name: "App connectors", State: health.Unknown, Scope: "/work/repo-b"},
	}}
	view := readinessView(report, nil, now)
	if view.Summary != "1 problem to fix · 1 unverified" || len(view.Tasks) != 1 {
		t.Fatalf("overview must summarize duplicate checks: %+v", view)
	}
	task := view.Tasks[0]
	if task.Title != "slack · Claude Code" || task.Action.Title != "Sign in" || task.Scope != "repo-a, repo-b" {
		t.Fatalf("problem must have an adjacent, short repair label: %+v", task)
	}
	if strings.Contains(task.Detail, "Raw authentication") || strings.Contains(view.Summary, "healthy-service") {
		t.Fatal("raw probe detail belongs only in optional diagnostics")
	}
	if !strings.Contains(view.Details, "healthy-service") || !strings.Contains(view.Details, "/work/repo-a") || !strings.Contains(view.Details, "[unknown]") {
		t.Fatal("optional details must retain full checks and scopes")
	}
}

func TestOverviewRetainsEveryProblemForShowAll(t *testing.T) {
	now := time.Now()
	report := health.Report{CheckedAt: now}
	for i := 0; i < 8; i++ {
		report.Checks = append(report.Checks, health.Check{ID: fmt.Sprint(i), Host: "Codex", Name: fmt.Sprintf("service-%d", i), State: health.Failed})
	}
	view := readinessView(report, nil, now)
	if len(view.Tasks) != 8 || view.Summary != "8 problems to fix" {
		t.Fatal("the native initial limit must not discard hidden problem rows")
	}
}

func TestOverviewDoesNotShowExpiredQuotaAsRemaining(t *testing.T) {
	now := time.Now()
	view := readinessView(health.Report{CheckedAt: now}, map[model.Provider]model.ProviderStatus{
		model.ProviderClaudeCode: {Provider: model.ProviderClaudeCode, Windows: []model.Window{{Kind: model.WindowWeekly, UsedPercent: 98, ResetsAt: now.Add(-time.Minute)}}, UpdatedAt: now.Add(-time.Hour)},
		model.ProviderCodex:      {Provider: model.ProviderCodex, Windows: []model.Window{{Kind: model.WindowWeekly, UsedPercent: 10, ResetsAt: now.Add(time.Hour)}}, UpdatedAt: now},
	}, now)
	if strings.Contains(view.Quota, "2%") || !strings.Contains(view.Quota, "Weekly waiting for update") || !strings.Contains(view.Quota, "90% left") {
		t.Fatalf("expired usage needs a new provider report: %q", view.Quota)
	}
}
