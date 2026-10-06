package claude

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/chuongtrh/ai-quota/internal/config"
	"github.com/chuongtrh/ai-quota/internal/model"
	"github.com/chuongtrh/ai-quota/internal/storage"
)

var ErrNoQuotaData = errors.New("Waiting for a fresh quota report from Claude Code")

type CacheProvider struct {
	paths config.Paths
}

func NewCacheProvider(paths config.Paths) *CacheProvider {
	return &CacheProvider{paths: paths}
}

func (p *CacheProvider) ID() model.Provider {
	return model.ProviderClaudeCode
}

func (p *CacheProvider) Fetch(ctx context.Context) (model.ProviderStatus, error) {
	select {
	case <-ctx.Done():
		return model.ProviderStatus{Provider: p.ID()}, ctx.Err()
	default:
	}
	var status model.ProviderStatus
	if err := storage.ReadJSON(p.paths.ClaudeCache(), &status); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return model.ProviderStatus{Provider: p.ID()}, ErrNoQuotaData
		}
		return model.ProviderStatus{Provider: p.ID()}, err
	}
	if status.Metadata["cache_version"] != "2" {
		return model.ProviderStatus{Provider: p.ID()}, ErrNoQuotaData
	}
	active := status.Windows[:0]
	for _, window := range status.Windows {
		if window.ResetsAt.After(time.Now()) {
			active = append(active, window)
		}
	}
	status.Windows = active
	if len(active) == 0 {
		return model.ProviderStatus{Provider: p.ID()}, ErrNoQuotaData
	}
	status.Provider = p.ID()
	status.Normalize()
	return status, nil
}
