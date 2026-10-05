package claude

import (
	"github.com/chuongtrh/ai-quota/internal/model"
	"testing"
	"time"
)

func TestOlderSessionCannotReduceUsageOrDropActiveWindow(t *testing.T) {
	now := time.Now()
	reset := now.Add(time.Hour)
	old := model.ProviderStatus{UpdatedAt: now.Add(-time.Minute), Windows: []model.Window{
		{Kind: model.WindowWeekly, UsedPercent: 94, ResetsAt: reset},
		{Kind: model.WindowSession, UsedPercent: 10, ResetsAt: reset},
	}}
	next := model.ProviderStatus{UpdatedAt: now, Windows: []model.Window{{Kind: model.WindowWeekly, UsedPercent: 86, ResetsAt: reset}}}
	got := mergeQuota(old, next, now)
	weekly, _ := got.Window(model.WindowWeekly)
	if weekly.UsedPercent != 94 || len(got.Windows) != 2 || !got.UpdatedAt.Equal(old.UpdatedAt) {
		t.Fatalf("stale session replaced quota: %#v", got)
	}
	next.Windows[0].ResetsAt = reset.Add(7 * 24 * time.Hour)
	next.Windows[0].UsedPercent = 2
	got = mergeQuota(old, next, now)
	weekly, _ = got.Window(model.WindowWeekly)
	if weekly.UsedPercent != 2 {
		t.Fatal("new reset cycle must permit lower usage")
	}
}
