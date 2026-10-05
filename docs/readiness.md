# AI readiness monitor

AIQuota keeps quota and connection readiness in one macOS app. The menu badge shows the most urgent quota. A question mark means at least one check is unknown. `AI !N` means N checks need action. Open **Readiness** for up to five grouped problems and their adjacent repair buttons. **Show all** reveals additional problems. **Show details** holds the full probe results, scopes, and timestamps. **Manage** holds workspace, settings, and host shortcuts. The badge counts repairs, not duplicate workspace results.

The compact status item uses a stable macOS autosave name and an initial position near the system controls. Display and Space changes restore visibility. macOS can still hide status items when a display has insufficient menu space. Reopen AIQuota from Applications to open the readiness window on the display under the pointer.

## Checks

- Claude Code: `claude mcp list` in each monitored workspace. Cloud connector listings can include available or historical integrations. Historical connection records do not prove that a connector is currently added. A current successful cloud handshake is retained; every other cloud result is one unverified coverage row with no login task. Configured local and plugin MCPs keep their observed auth status. A successful handshake does not prove backend permissions. Failures must occur twice before an alert.
- Codex: a separate app-server reads MCP auth status and the tool catalog. Before offering OAuth login, AIQuota verifies that the transport uses OAuth. Bearer or header credentials remain unverified until a backend probe can check them. Enabled app connectors remain Unknown because the catalog does not prove live OAuth or backend access.
- fs-log-data: the local `/api/readiness?force=1` endpoint runs BigQuery dry-run, Athena, Glue, Redis, and API probes. Only loopback URLs are allowed. Stale results remain Unknown.
- Flights Shopping Hub: the maintained checkout's read-only `preflight.py` checks the selected repository profiles. It checks VPN, tunnels, Jenkins, Docker, AWS, and Cognito as required by those profiles. MCP registration alone remains Unknown.

Checks run every five minutes, after wake, on Check now, and after a completed sign-in. A sign-in that finishes during another probe queues a fresh check. Alerts occur on a change to a confirmed failure or expired auth. Unknown results do not claim recovery. A successful check clears the issue badge. Credentials remain in the original host or probe store. AIQuota saves sanitized results, not raw diagnostics or credentials.

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

Repair actions use a fixed allowlist. **Sign in** opens the browser through the owning host's supported login command, with no Terminal window. AIQuota keeps the callback process alive until completion and shows progress or a short failure beside the button. Duplicate clicks cannot start a second callback for the same repair. Claude receives an invisible PTY because its CLI requires a terminal during the callback; its process group is cancelled on Quit or when the login deadline expires. Login output is discarded. Credentials stay in their existing stores.

AWS SSO login requires an explicit profile from the readiness response or the fs-log-data credential source selector. No profile is inferred from the environment name. GCP login is offered only for an explicit ADC source. Cognito refresh uses the maintained Hub browser script, which saves its own Playwright state after browser sign-in. Docker and VPN buttons open their apps. Only the tunnel buttons open Terminal to run the existing `blackhole_us_staging` or `blackhole_us_production` shell function. Missing Jenkins secrets and unverified cloud connectors require repair in their existing settings.

The supported Codex OAuth command is documented in [OpenAI MCP configuration](https://learn.chatgpt.com/docs/extend/mcp?surface=cli). Opening a browser or completing sign-in does not prove backend access; the subsequent probe must succeed before the issue clears.

## Claude quota

Claude status-line quota can lag the account usage page. AIQuota accepts a changed report even when usage decreases or its reset time moves earlier. It tracks the last reported window values by session, under a cross-process file lock. An unchanged repaint from a known old session cannot replace a newer report. It does not infer a full quota from a reset countdown.

Older caches from the maximum-usage rule are invalidated automatically. Expired quota and omitted windows wait for a new provider report. The session report history contains hashes and receipt times, never transcript content or credentials. Sources without a session identifier cannot suppress old-session repaints. A previously unseen stale session can still send an old first report; this source is session-reported, not a live account API.

## Verification

Run `go test ./...`, `go vet ./...`, and `go test -race ./internal/health ./internal/provider/claude`. Regression tests prove an expired auth probe raises an issue, a repeated connection failure sends one alert, recovery clears the badge, registration does not count as auth proof, stale data stays Unknown, and unchanged old-session repaints cannot undo a manual quota reset.
