# AI readiness monitor

AIQuota keeps quota and connection readiness in one macOS app. The menu badge shows the most urgent quota. A question mark means at least one check is unknown. `AI !N` means N checks need action. Open **Readiness** for each result, its scope, its timestamp, and available repair buttons.

The compact status item uses a stable macOS autosave name and an initial position near the system controls. Display and Space changes restore visibility. macOS can still hide status items when a display has insufficient menu space. Reopen AIQuota from Applications to open the readiness window on the display under the pointer.

## Checks

- Claude Code: `claude mcp list` in each monitored workspace. A successful handshake does not prove backend permissions. Failures must occur twice before an alert.
- Codex: a separate app-server reads MCP auth status and the tool catalog. Enabled app connectors remain Unknown because the catalog does not prove live OAuth or backend access.
- fs-log-data: the local `/api/readiness?force=1` endpoint runs BigQuery dry-run, Athena, Glue, Redis, and API probes. Only loopback URLs are allowed. Stale results remain Unknown.
- Flights Shopping Hub: the maintained checkout's read-only `preflight.py` checks the selected repository profiles. It checks VPN, tunnels, Jenkins, Docker, AWS, and Cognito as required by those profiles. MCP registration alone remains Unknown.

Checks run every five minutes, after wake, on Check now, and after a completed Terminal login. Alerts occur on a change to a confirmed failure or expired auth. Unknown results do not claim recovery. A successful check clears the issue badge. Credentials remain in the original host or probe store. AIQuota saves sanitized results, not raw diagnostics or credentials.

## Configuration

Use **Monitor workspace** to add a project directory. Use **Monitor settings** to edit `~/Library/Application Support/AIQuota/health-config.json`. The next check reloads valid settings. Up to eight workspaces are supported.

```json
{
  "readiness_url": "http://localhost:3000/api/readiness",
  "workspaces": ["/path/to/project"],
  "interval_minutes": 5,
  "hub_checkout": "/path/to/flights-shopping-hub",
  "hub_repos": ["curiosity", "wego-fares", "pennyworth"]
}
```

An empty readiness URL disables that adapter. Hub checks are disabled until a checkout and repo list are configured. The adapter runs from `ai/tool`; it does not install plugins or alter Hub configuration. Probe tasks have a 90-second deadline. The Hub adapter runs up to four canonical checks at a time and retains completed results if the deadline expires. MCP registration is checked separately to avoid a second slow sweep. A timeout remains Unknown.

Repair actions use a fixed allowlist. MCP login opens the correct host CLI in Terminal. AWS SSO login requires an explicit profile from the readiness response or the fs-log-data credential source selector. No profile is inferred from the environment name. GCP login is offered only for an explicit ADC source. Cognito refresh uses the maintained Hub browser script. Docker and VPN buttons open their apps. Tunnel buttons run the existing `blackhole_us_staging` or `blackhole_us_production` shell function. Missing Jenkins secrets and unverified cloud connectors require repair in their existing settings.

## Claude quota

Claude status-line quota can lag the account usage page. AIQuota retains the highest observed usage within each reset cycle, under a cross-process file lock, so an older session cannot reduce usage or remove an active window. A new reset cycle can start at lower usage. The window shows the last quota change instead of treating every status-line render as fresh quota. This source is still session-reported; it is not a live account API. A manual quota reset or account switch can require clearing the Claude quota cache, because usage can legitimately fall in the same cycle.

## Verification

Run `go test ./...`, `go vet ./...`, and `go test -race ./internal/health ./internal/provider/claude`. Regression tests prove an expired auth probe raises an issue, a repeated connection failure sends one alert, recovery clears the badge, registration does not count as auth proof, stale data stays Unknown, and old Claude sessions cannot overwrite newer quota.
