package claude

import (
	"github.com/chuongtrh/ai-quota/internal/config"
	"github.com/chuongtrh/ai-quota/internal/model"
	"github.com/chuongtrh/ai-quota/internal/storage"
	"time"
)

// CacheQuota prevents an older Claude session from overwriting newer account quota.
// Within the same reset cycle, usage is cumulative. A new cycle can start lower.
func CacheQuota(paths config.Paths, incoming model.ProviderStatus, now time.Time) error {
	return storage.WithFileLock(paths.ClaudeCache(), func() error {
		var previous model.ProviderStatus
		_ = storage.ReadJSON(paths.ClaudeCache(), &previous)
		return storage.WriteJSON(paths.ClaudeCache(), mergeQuota(previous, incoming, now), 0o600)
	})
}

func mergeQuota(previous, incoming model.ProviderStatus, now time.Time) model.ProviderStatus {
	changed := len(previous.Windows) == 0
	for _, old := range previous.Windows {
		if !old.ResetsAt.After(now) {
			continue
		}
		index := -1
		for i, next := range incoming.Windows {
			if next.Kind == old.Kind {
				index = i
				break
			}
		}
		if index == -1 {
			incoming.Windows = append(incoming.Windows, old)
			continue
		}
		next := incoming.Windows[index]
		if next.ResetsAt.Before(old.ResetsAt) || (next.ResetsAt.Equal(old.ResetsAt) && next.UsedPercent < old.UsedPercent) {
			incoming.Windows[index] = old
		}
	}
	for _, next := range incoming.Windows {
		old, exists := previous.Window(next.Kind)
		if !exists || !next.ResetsAt.Equal(old.ResetsAt) || next.UsedPercent != old.UsedPercent {
			changed = true
		}
	}
	if !changed && !previous.UpdatedAt.IsZero() {
		incoming.UpdatedAt = previous.UpdatedAt
	}
	incoming.Metadata = map[string]string{"source": "Claude Code status line", "received_at": now.Format(time.RFC3339)}
	incoming.Normalize()
	return incoming
}
