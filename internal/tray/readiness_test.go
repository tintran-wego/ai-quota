package tray

import (
	"github.com/chuongtrh/ai-quota/internal/health"
	"testing"
	"time"
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
