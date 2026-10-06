package claude

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/chuongtrh/ai-quota/internal/config"
	"github.com/chuongtrh/ai-quota/internal/model"
	"github.com/chuongtrh/ai-quota/internal/storage"
)

type sessionReport struct {
	Fingerprint string    `json:"fingerprint"`
	ReceivedAt  time.Time `json:"received_at"`
}
type reportHistory struct {
	Sessions map[string]sessionReport `json:"sessions"`
}

// Accept changed provider reports, including lower usage after a manual reset.
// Repainting an unchanged old session must not overwrite a newer report.
func CacheQuota(paths config.Paths, incoming model.ProviderStatus, now time.Time) error {
	return storage.WithFileLock(paths.ClaudeCache(), func() error {
		var previous model.ProviderStatus
		cacheExists := storage.ReadJSON(paths.ClaudeCache(), &previous) == nil && previous.Metadata["cache_version"] == "2"
		historyPath := filepath.Join(paths.DataDir, "claude-report-history.json")
		var history reportHistory
		_ = storage.ReadJSON(historyPath, &history)
		if history.Sessions == nil {
			history.Sessions = map[string]sessionReport{}
		}
		session := incoming.Metadata["session_id"]
		incoming.Normalize()
		data, err := json.Marshal(incoming.Windows)
		if err != nil {
			return err
		}
		fingerprint := fmt.Sprintf("%x", sha256.Sum256(data))
		if session != "" {
			previous, exists := history.Sessions[session]
			history.Sessions[session] = sessionReport{Fingerprint: fingerprint, ReceivedAt: now}
			// Keep live sessions, discard the oldest history entry at a fixed bound.
			if len(history.Sessions) > 1024 {
				oldest := ""
				var oldestTime time.Time
				for id, report := range history.Sessions {
					if id != session && (oldest == "" || report.ReceivedAt.Before(oldestTime)) {
						oldest = id
						oldestTime = report.ReceivedAt
					}
				}
				delete(history.Sessions, oldest)
			}
			if exists && previous.Fingerprint == fingerprint && cacheExists {
				return storage.WriteJSON(historyPath, history, 0o600)
			}
		}
		incoming = mergeQuota(previous, incoming, now)
		if err := storage.WriteJSON(paths.ClaudeCache(), incoming, 0o600); err != nil {
			return err
		}
		return storage.WriteJSON(historyPath, history, 0o600)
	})
}

func mergeQuota(previous, incoming model.ProviderStatus, now time.Time) model.ProviderStatus {
	// Delivery time orders changed reports. Usage and reset times can both decrease.
	if incoming.UpdatedAt.Before(previous.UpdatedAt) {
		return previous
	}
	incoming.Normalize()
	if incoming.Metadata == nil {
		incoming.Metadata = map[string]string{}
	}
	incoming.Metadata["cache_version"] = "2"
	incoming.Metadata["source"] = "Claude Code status line"
	incoming.Metadata["received_at"] = now.Format(time.RFC3339)
	return incoming
}
