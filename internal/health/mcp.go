package health

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func probeClaude(ctx context.Context, workspace string, now time.Time) []Check {
	data, err := runCommand(ctx, workspace, "claude", "mcp", "list")
	checks := parseClaudeMCP(string(data), workspace, now)
	if err != nil || len(checks) == 0 {
		detail := "No MCP results. Plugin or app-managed connectors may need a check in Claude."
		if err != nil {
			detail = "Claude MCP inventory could not be checked; verify CLI installation and re-check"
		}
		checks = append(checks, Check{ID: "claude/" + workspace + "/inventory", Host: "Claude Code", Scope: workspace, Name: "MCP coverage", State: Unknown, Detail: detail, CheckedAt: now})
	}
	return checks
}

// A cloud catalog row does not prove that the user has added the connector.
// Only a successful current handshake can be classified from this inventory.
func parseClaudeMCP(output, workspace string, now time.Time) []Check {
	result := []Check{}
	unverifiedCloud := 0
	for _, line := range strings.Split(ansi.ReplaceAllString(output, ""), "\n") {
		split := strings.LastIndex(line, " - ")
		if split < 0 {
			continue
		}
		nameEnd := strings.Index(line, ": ")
		if nameEnd < 0 || nameEnd > split {
			continue
		}
		name := strings.TrimSpace(line[:nameEnd])
		status := strings.ToLower(line[split+3:])
		connected := strings.TrimSpace(strings.TrimLeft(status, "✓✔✅ ")) == "connected"
		if strings.HasPrefix(name, "claude.ai ") && !connected {
			unverifiedCloud++
			continue
		}
		c := Check{ID: "claude/" + workspace + "/" + name, Host: "Claude Code", Scope: workspace, Name: name, State: Unknown, CheckedAt: now, Detail: "Status unavailable"}
		switch {
		case strings.Contains(status, "needs authentication"):
			c.State = NeedsAuth
			c.Detail = "Sign-in required"
			c.Action = &Action{Kind: "mcp-login", Host: "claude", Target: name, Workspace: workspace}
		case strings.Contains(status, "failed to connect"):
			c.State = Failed
			c.Detail = "MCP connection failed; service permissions are not verified"
		case strings.Contains(status, "pending approval"):
			c.Detail = "Project approval required in Claude Code"
		case strings.Contains(status, "disabled") || strings.Contains(status, "rejected"):
			c.State = Disabled
			c.Detail = "Disabled or rejected for this workspace"
		case connected:
			c.State = OK
			c.Detail = "MCP handshake succeeded; backend access is checked separately"
		}
		result = append(result, c)
	}
	if unverifiedCloud > 0 {
		result = append(result, Check{ID: "claude/" + workspace + "/cloud-coverage", Host: "Claude Code", Scope: workspace, Name: "Claude app connectors", State: Unknown, CheckedAt: now, Detail: "Cloud connector selection and authentication are not verified by this inventory"})
	}
	return result
}

type rpcClient struct {
	encoder *json.Encoder
	scanner *bufio.Scanner
	nextID  int
}

func (c *rpcClient) call(method string, params any) (json.RawMessage, error) {
	c.nextID++
	id := c.nextID
	if err := c.encoder.Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for c.scanner.Scan() {
		var reply struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(c.scanner.Bytes(), &reply) != nil || reply.ID != id {
			continue
		}
		if reply.Error != nil {
			return nil, fmt.Errorf("app-server rejected %s", method)
		}
		return reply.Result, nil
	}
	if err := c.scanner.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}

func probeCodex(ctx context.Context, workspace string, now time.Time) []Check {
	fallback := Check{ID: "codex/" + workspace + "/inventory", Host: "Codex", Scope: workspace, Name: "MCP coverage", State: Unknown, CheckedAt: now, Detail: "Codex MCP inventory could not be checked"}
	path, err := commandPath("codex")
	if err != nil {
		return []Check{fallback}
	}
	cmd := exec.CommandContext(ctx, path, "app-server")
	prepareCommand(cmd)
	cmd.Dir = workspace
	cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(path)+":/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:"+os.Getenv("PATH"))
	in, err := cmd.StdinPipe()
	if err != nil {
		return []Check{fallback}
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return []Check{fallback}
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return []Check{fallback}
	}
	defer func() { _ = in.Close(); stopCommand(cmd); _ = cmd.Wait() }()
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 65536), 64*1024*1024)
	rpc := rpcClient{encoder: json.NewEncoder(in), scanner: scanner}
	if _, err := rpc.call("initialize", map[string]any{"clientInfo": map[string]string{"name": "aiquota_health", "version": "1"}, "capabilities": map[string]bool{"experimentalApi": true}}); err != nil {
		return []Check{fallback}
	}
	if err := rpc.encoder.Encode(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return []Check{fallback}
	}
	checks := []Check{}
	cursor := ""
	for page := 0; page < 100; page++ {
		params := map[string]any{"limit": 100, "detail": "toolsAndAuthOnly"}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := rpc.call("mcpServerStatus/list", params)
		if err != nil {
			fallback.Detail = "Codex MCP inventory unavailable: " + err.Error()
			return append(checks, fallback)
		}
		var response struct {
			Data []struct {
				Name       string                     `json:"name"`
				Auth       string                     `json:"authStatus"`
				Tools      map[string]json.RawMessage `json:"tools"`
				ServerInfo json.RawMessage            `json:"serverInfo"`
			} `json:"data"`
			NextCursor string `json:"nextCursor"`
		}
		if json.Unmarshal(raw, &response) != nil {
			return append(checks, fallback)
		}
		for _, server := range response.Data {
			c := Check{ID: "codex/" + workspace + "/" + server.Name, Host: "Codex", Scope: workspace, Name: server.Name, State: Unknown, CheckedAt: now, Detail: "No tool catalog; backend and authentication are not verified"}
			if server.Auth == "notLoggedIn" {
				switch codexAuthMechanism(ctx, workspace, server.Name) {
				case "oauth":
					c.State = NeedsAuth
					c.Detail = "MCP sign-in required"
					c.Action = &Action{Kind: "mcp-login", Host: "codex", Target: server.Name, Workspace: workspace}
				case "credential":
					c.Detail = "Refresh the configured credential or verify request headers in Codex settings"
				default:
					c.Detail = "Authentication mechanism is unverified; check the server configuration in Codex"
				}
			} else if len(server.Tools) > 0 {
				c.State = OK
				c.Detail = "MCP tool catalog available; backend access is checked separately"
			}
			checks = append(checks, c)
		}
		if response.NextCursor == "" {
			break
		}
		if response.NextCursor == cursor {
			return append(checks, fallback)
		}
		cursor = response.NextCursor
	}
	checks = append(checks, probeCodexApps(&rpc, workspace, now)...)
	if len(checks) == 0 {
		fallback.Detail = "No MCP inventory returned. App-managed connectors require a check in Codex settings."
		checks = append(checks, fallback)
	}
	return checks
}

// Read the effective server through the CLI instead of guessing from authStatus.
// Header values and tokens stay in this process and never enter a report.
func codexAuthMechanism(ctx context.Context, workspace, name string) string {
	data, err := runCommand(ctx, workspace, "codex", "mcp", "get", "--json", "--", name)
	if err != nil {
		return ""
	}
	return parseCodexAuthMechanism(data)
}

func parseCodexAuthMechanism(data []byte) string {
	var config struct {
		Transport struct {
			Type          string                     `json:"type"`
			BearerEnv     string                     `json:"bearer_token_env_var"`
			Headers       map[string]json.RawMessage `json:"http_headers"`
			EnvHeaders    map[string]json.RawMessage `json:"env_http_headers"`
			HeadersHelper json.RawMessage            `json:"http_headers_helper"`
		} `json:"transport"`
	}
	if json.Unmarshal(data, &config) != nil || config.Transport.Type != "streamable_http" {
		return ""
	}
	t := config.Transport
	if t.BearerEnv != "" || len(t.Headers) > 0 || len(t.EnvHeaders) > 0 || (len(t.HeadersHelper) > 0 && string(t.HeadersHelper) != "null") {
		return "credential"
	}
	return "oauth"
}

// App accessibility is inventory, not a live OAuth probe. Never mark it healthy.
func probeCodexApps(rpc *rpcClient, workspace string, now time.Time) []Check {
	fallback := Check{ID: "codex/" + workspace + "/apps", Host: "Codex", Scope: workspace, Name: "App connectors", State: Unknown, CheckedAt: now, Detail: "Connector health requires a check in Codex settings"}
	raw, err := rpc.call("app/list", map[string]any{"limit": 100, "forceRefetch": true})
	if err != nil {
		return []Check{fallback}
	}
	var response struct {
		Data []struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			Enabled    bool   `json:"isEnabled"`
			Accessible bool   `json:"isAccessible"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return []Check{fallback}
	}
	result := []Check{}
	for _, app := range response.Data {
		if !app.Enabled || !app.Accessible {
			continue
		}
		c := fallback
		c.ID = "codex/" + workspace + "/app/" + app.ID
		c.Name = app.Name
		c.Detail = "Connector enabled; live token and backend access are not verified"
		if !app.Accessible {
			c.Detail = "Connector enabled but inaccessible; open Codex connector settings"
		}
		result = append(result, c)
	}
	if len(result) == 0 {
		return []Check{fallback}
	}
	return result
}
