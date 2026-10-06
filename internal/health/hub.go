package health

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"
)

const jenkinsNetworkDetail = "Direct Jenkins is unreachable; check VPN and its environment tunnel"

// Run the maintained Hub's read-only preflight. Credentials stay with its probes.
func probeHub(ctx context.Context, config Config, now time.Time) []Check {
	fallback := Check{ID: "hub/preflight", Host: "Flights Shopping Hub", Name: "Preflight", Scope: config.HubCheckout, State: Unknown, CheckedAt: now, Detail: "Hub preflight unavailable or timed out; check the maintained checkout and repo profiles"}
	for _, repo := range config.HubRepos {
		if !validTarget(repo) || strings.ContainsAny(repo, "/:") {
			return []Check{fallback}
		}
	}
	root := filepath.Join(config.HubCheckout, "ai/tool")
	repos, _ := json.Marshal(config.HubRepos)
	data, runErr := runCommand(ctx, root, "python3", "-B", "-u", "-c", hubAdapter, root, string(repos))
	checks := []Check{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		var row json.RawMessage
		if json.Unmarshal(scanner.Bytes(), &row) != nil {
			continue
		}
		parsed, err := parseHub([]byte(`{"checks":[`+string(row)+`]}`), config.HubCheckout, now)
		if err == nil {
			checks = append(checks, parsed...)
		}
	}
	if runErr != nil || len(checks) == 0 {
		checks = append(checks, fallback)
	}
	attachJenkinsTunnelActions(checks)
	return checks
}

// A gateway OAuth grant cannot repair direct Jenkins reachability. Offer the
// existing tunnel repair only when that same environment's tunnel also failed.
func attachJenkinsTunnelActions(checks []Check) {
	tunnels := map[string]Action{}
	for _, c := range checks {
		if c.Host == "Flights Shopping Hub" && c.State == Failed && c.Action != nil && c.Action.Kind == "tunnel" {
			tunnels[c.Scope+"\x00"+c.Action.Target] = *c.Action
		}
	}
	for i := range checks {
		c := &checks[i]
		if c.Host != "Flights Shopping Hub" || c.State != Failed || c.Detail != jenkinsNetworkDetail {
			continue
		}
		env := strings.TrimPrefix(c.Name, "jenkins:")
		if env == "prod" {
			env = "production"
		}
		if action, ok := tunnels[c.Scope+"\x00"+env]; ok {
			c.Action = &action
		}
	}
}

func parseHub(data []byte, checkout string, now time.Time) ([]Check, error) {
	var response struct {
		Checks []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, err
	}
	result := []Check{}
	for _, row := range response.Checks {
		c := Check{ID: "hub/" + row.ID, Host: "Flights Shopping Hub", Scope: checkout, Name: row.ID, State: Unknown, CheckedAt: now, Detail: "Probe result is inconclusive; open Hub preflight for guidance"}
		switch row.Status {
		case "ok":
			c.State = OK
			c.Detail = "Hub read-only probe succeeded"
		case "expired":
			c.State = NeedsAuth
			c.Detail = "Credential refresh failed; sign in again"
		case "missing":
			c.State = Failed
			c.Detail = "Required configuration or credential is missing"
		case "unreachable":
			c.State = Failed
			c.Detail = "Service unreachable; check VPN, tunnel, or daemon"
		}
		if strings.HasPrefix(row.ID, "jenkins:") {
			switch row.Status {
			case "unreachable":
				c.Detail = jenkinsNetworkDetail
			case "missing":
				c.Detail = "Jenkins API user or token is missing; configure it in Hub onboarding"
			case "expired":
				c.Detail = "Jenkins API credentials were rejected; refresh the API token in Hub onboarding"
			}
		}
		// Canonical MCP rows only test registration, never backend permissions.
		if strings.HasSuffix(row.ID, "-mcp") && c.State == OK {
			c.State = Unknown
			c.Detail = "MCP registered; verify connector access in the AI host"
		}
		if c.NeedsAttention() && row.ID == "cognito:pennyworth" {
			c.Action = &Action{Kind: "cognito-login", Workspace: checkout}
		}
		if c.NeedsAttention() {
			switch row.ID {
			case "docker":
				c.Action = &Action{Kind: "open-app", Target: "Docker"}
			case "vpn":
				c.Action = &Action{Kind: "open-app", Target: "Pritunl"}
			case "tunnel:prod", "tunnel:production", "tunnel:staging":
				c.Action = &Action{Kind: "tunnel", Target: strings.TrimPrefix(row.ID, "tunnel:")}
			}
		}
		if c.Action != nil && c.Action.Kind == "tunnel" && c.Action.Target == "prod" {
			c.Action.Target = "production"
		}
		// Ambient AWS preflight identity has no proven profile. Do not guess a login target.
		result = append(result, c)
	}
	return result, nil
}

//go:embed hub_adapter.py
var hubAdapter string
