package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/chuongtrh/ai-quota/internal/config"
	"github.com/chuongtrh/ai-quota/internal/model"
	"github.com/chuongtrh/ai-quota/internal/storage"
)

const BridgeFlag = "--claude-statusline"

type statusLineInput struct {
	Model struct {
		DisplayName string `json:"display_name"`
	} `json:"model"`
	RateLimits struct {
		FiveHour *rateLimitWindow `json:"five_hour"`
		SevenDay *rateLimitWindow `json:"seven_day"`
	} `json:"rate_limits"`
}

type rateLimitWindow struct {
	UsedPercent float64 `json:"used_percentage"`
	ResetsAt    int64   `json:"resets_at"`
}

func ParseStatusLine(data []byte, now time.Time) (model.ProviderStatus, error) {
	status := model.ProviderStatus{
		Provider:  model.ProviderClaudeCode,
		UpdatedAt: now,
	}
	var input statusLineInput
	if err := json.Unmarshal(data, &input); err != nil {
		return status, fmt.Errorf("decode Claude Code status line: %w", err)
	}
	appendWindow := func(kind model.WindowKind, duration int64, source *rateLimitWindow) {
		if source == nil || source.ResetsAt <= 0 {
			return
		}
		status.Windows = append(status.Windows, model.Window{
			Kind:            kind,
			UsedPercent:     source.UsedPercent,
			DurationMinutes: duration,
			ResetsAt:        time.Unix(source.ResetsAt, 0),
		})
	}
	appendWindow(model.WindowSession, 5*60, input.RateLimits.FiveHour)
	appendWindow(model.WindowWeekly, 7*24*60, input.RateLimits.SevenDay)
	status.Normalize()
	if len(status.Windows) == 0 {
		return status, errors.New("Claude Code has not sent quota data yet; send at least one prompt first")
	}
	return status, nil
}

func RunBridge(paths config.Paths, input io.Reader, output io.Writer) error {
	data, err := io.ReadAll(io.LimitReader(input, 4*1024*1024))
	if err != nil {
		return fmt.Errorf("read Claude Code status line: %w", err)
	}
	status, parseErr := ParseStatusLine(data, time.Now())
	if parseErr == nil {
		_ = CacheQuota(paths, status, time.Now())
	}

	previousOutput := runPreviousStatusLine(paths, data)
	if len(bytes.TrimSpace(previousOutput)) > 0 {
		_, _ = output.Write(previousOutput)
		if previousOutput[len(previousOutput)-1] != '\n' {
			_, _ = io.WriteString(output, "\n")
		}
	} else {
		_, _ = io.WriteString(output, formatStatusLine(status)+"\n")
	}
	return nil
}

func runPreviousStatusLine(paths config.Paths, input []byte) []byte {
	var backup StatusLineBackup
	if err := storage.ReadJSON(paths.ClaudeStatusLineBackup(), &backup); err != nil || !backup.HadValue {
		return nil
	}
	var previous struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(backup.Value, &previous); err != nil || previous.Command == "" {
		return nil
	}
	if strings.Contains(previous.Command, BridgeFlag) {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", previous.Command)
	cmd.Stdin = bytes.NewReader(input)
	var output bytes.Buffer
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		return nil
	}
	return output.Bytes()
}

func formatStatusLine(status model.ProviderStatus) string {
	parts := []string{"[Claude]"}
	if window, ok := status.Window(model.WindowSession); ok {
		parts = append(parts, fmt.Sprintf("5h: %d%%", int(window.UsedPercent+0.5)))
	}
	if window, ok := status.Window(model.WindowWeekly); ok {
		parts = append(parts, fmt.Sprintf("7d: %d%%", int(window.UsedPercent+0.5)))
	}
	return strings.Join(parts, " · ")
}
