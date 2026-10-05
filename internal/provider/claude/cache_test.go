package claude

import (
	"context"
	"github.com/chuongtrh/ai-quota/internal/config"
	"github.com/chuongtrh/ai-quota/internal/model"
	"github.com/chuongtrh/ai-quota/internal/storage"
	"testing"
	"time"
)

func quotaReport(session string, used float64, now, reset time.Time) model.ProviderStatus {
	return model.ProviderStatus{Provider: model.ProviderClaudeCode, UpdatedAt: now, Metadata: map[string]string{"session_id": session}, Windows: []model.Window{{Kind: model.WindowWeekly, UsedPercent: used, ResetsAt: reset}}}
}
func TestManualResetCanLowerUsageInSameWindow(t *testing.T) {
	now := time.Now()
	reset := now.Add(time.Hour)
	got := mergeQuota(quotaReport("old", 98, now.Add(-time.Minute), reset), quotaReport("fresh", 0, now, reset), now)
	if got.Windows[0].UsedPercent != 0 {
		t.Fatal("manual reset remained at 2% left")
	}
	got = mergeQuota(got, quotaReport("fresh", 1, now.Add(time.Minute), reset.Add(-time.Minute)), now.Add(time.Minute))
	if got.Windows[0].UsedPercent != 1 || !got.Windows[0].ResetsAt.Equal(reset.Add(-time.Minute)) {
		t.Fatal("provider reset time must be accepted even when earlier")
	}
}
func TestOldSessionRepaintCannotUndoReset(t *testing.T) {
	now := time.Now()
	reset := now.Add(time.Hour)
	paths := config.Paths{DataDir: t.TempDir()}
	old := quotaReport("old", 98, now, reset)
	if err := CacheQuota(paths, old, now); err != nil {
		t.Fatal(err)
	}
	fresh := quotaReport("fresh", 0, now.Add(time.Second), reset)
	if err := CacheQuota(paths, fresh, fresh.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	old.UpdatedAt = now.Add(2 * time.Second)
	if err := CacheQuota(paths, old, old.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	var got model.ProviderStatus
	storage.ReadJSON(paths.ClaudeCache(), &got)
	if got.Windows[0].UsedPercent != 0 || !got.UpdatedAt.Equal(fresh.UpdatedAt) {
		t.Fatal("old repaint restored exhausted quota")
	}
	old.Windows[0].UsedPercent = 2
	old.UpdatedAt = now.Add(3 * time.Second)
	if err := CacheQuota(paths, old, old.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	storage.ReadJSON(paths.ClaudeCache(), &got)
	if got.Windows[0].UsedPercent != 2 {
		t.Fatal("changed report must be accepted")
	}
}
func TestIncomingReportReplacesOmittedWindows(t *testing.T) {
	now := time.Now()
	reset := now.Add(time.Hour)
	old := quotaReport("old", 98, now.Add(-time.Minute), reset)
	old.Windows = append(old.Windows, model.Window{Kind: model.WindowSession, UsedPercent: 90, ResetsAt: reset})
	fresh := quotaReport("new", 0, now, reset)
	got := mergeQuota(old, fresh, now)
	if len(got.Windows) != 1 {
		t.Fatal("omitted stale window was carried forward")
	}
}
func TestProviderHidesExpiredAndLegacyCache(t *testing.T) {
	now := time.Now()
	paths := config.Paths{DataDir: t.TempDir()}
	for _, test := range []struct {
		name   string
		status model.ProviderStatus
	}{
		{"legacy", quotaReport("old", 98, now, now.Add(time.Hour))},
		{"expired", quotaReport("new", 98, now, now.Add(-time.Second))},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.name == "expired" {
				test.status.Metadata["cache_version"] = "2"
			}
			storage.WriteJSON(paths.ClaudeCache(), test.status, 0600)
			status, err := NewCacheProvider(paths).Fetch(context.Background())
			if err == nil || len(status.Windows) != 0 {
				t.Fatal("stale quota must not be displayed as current")
			}
		})
	}
}
