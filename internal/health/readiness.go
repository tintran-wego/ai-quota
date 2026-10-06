package health

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type readinessReport struct {
	GeneratedAt time.Time `json:"generatedAt"`
	Stores      []struct {
		Store       string         `json:"store"`
		Environment string         `json:"environment"`
		State       string         `json:"state"`
		Detail      string         `json:"detail"`
		Facts       map[string]any `json:"facts"`
	} `json:"stores"`
}

func probeReadiness(ctx context.Context, address string, now time.Time) []Check {
	parent := Check{ID: "fs/readiness", Host: "fs-log-data", Name: "Local readiness service", State: Unknown, CheckedAt: now, Detail: "Not checked"}
	parsed, err := url.Parse(address)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || (parsed.Hostname() != "localhost" && !isLoopback(parsed.Hostname())) || parsed.User != nil {
		parent.Detail = "Readiness URL must use loopback without credentials"
		return []Check{parent}
	}
	values := parsed.Query()
	values.Set("force", "1")
	parsed.RawQuery = values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		parent.Detail = "Invalid readiness URL"
		return []Check{parent}
	}
	client := http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		parent.State = Failed
		parent.Detail = "Local readiness service is unavailable; start fs-log-data and re-check"
		return []Check{parent}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		parent.State = Failed
		parent.Detail = fmt.Sprintf("Readiness returned HTTP %d", response.StatusCode)
		return []Check{parent}
	}
	var report readinessReport
	if err := json.NewDecoder(io.LimitReader(response.Body, 1024*1024)).Decode(&report); err != nil {
		parent.Detail = "Readiness returned invalid JSON"
		return []Check{parent}
	}
	if report.GeneratedAt.IsZero() || now.Sub(report.GeneratedAt) > 10*time.Minute || report.GeneratedAt.After(now.Add(time.Minute)) {
		parent.Detail = "Readiness data is stale or has an invalid timestamp"
		return []Check{parent}
	}
	if len(report.Stores) == 0 {
		parent.Detail = "Readiness has no checks"
		return []Check{parent}
	}
	result := make([]Check, 0, len(report.Stores))
	for _, store := range report.Stores {
		c := Check{ID: "fs/" + store.Environment + "/" + store.Store, Host: "fs-log-data", Scope: store.Environment, Name: store.Store, CheckedAt: report.GeneratedAt, State: Unknown, Detail: "No probe result"}
		switch store.State {
		case "ok":
			c.State = OK
			c.Detail = "Read-only service probe succeeded"
		case "disabled", "not-configured":
			c.State = Disabled
			c.Detail = "Disabled or not configured"
		case "failed":
			c.State = Failed
			c.Detail = safeDetail(store.Detail)
		}
		if store.Store == "glue" && store.Facts["crawler"] == "not configured" {
			c.State = Disabled
			c.Detail = "No crawler configured"
		}
		if c.NeedsAttention() && (store.Store == "athena" || store.Store == "glue") && strings.Contains(strings.ToLower(store.Detail), "expired") {
			identity, _ := store.Facts["identity"].(string)
			profile := strings.TrimPrefix(identity, "profile:")
			c.State = NeedsAuth
			if strings.HasPrefix(identity, "profile:") && validTarget(profile) {
				c.Action = &Action{Kind: "aws-login", Target: profile}
			}
			c.ID = "fs/" + store.Environment + "/aws-auth"
			c.Name = "AWS session"
			c.Detail = "AWS sign-in expired; Athena or Glue cannot run"
		}
		result = append(result, c)
	}
	return uniqueChecks(result)
}
func isLoopback(host string) bool { ip := net.ParseIP(host); return ip != nil && ip.IsLoopback() }
func uniqueChecks(checks []Check) []Check {
	positions := map[string]int{}
	result := []Check{}
	for _, c := range checks {
		if i, exists := positions[c.ID]; exists {
			if c.NeedsAttention() {
				result[i] = c
			}
			continue
		}
		positions[c.ID] = len(result)
		result = append(result, c)
	}
	return result
}
