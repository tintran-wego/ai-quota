# AI quota

A lightweight macOS menu bar app for tracking Codex, Claude Code, and Google Antigravity CLI subscription quotas. It shows the remaining percentage, reset time, and low-quota alerts with a deliberately simple interface.

## Features

- Tracks Codex **session** and **weekly** quota windows.
- Tracks Claude Code **5-hour** and **7-day** quota windows.
- Tracks every model quota bucket reported by Google Antigravity CLI.
- Shows relative reset countdowns, including days for weekly limits.
- Shows the most urgent remaining quota in the menu bar.
- Sends native macOS alerts at 20%, 5%, and 0% remaining.
- Requires no server, database, or separate account.
- Never reads or stores provider authentication tokens.
- Provides a `Provider` interface for adding more AI providers.

## Official data sources

AI quota uses only integration surfaces documented by each provider:

- **Codex:** starts the local `codex app-server` process and calls `account/rateLimits/read` over JSON-RPC. See [Codex App Server](https://developers.openai.com/codex/app-server).
- **Claude Code:** receives `rate_limits.five_hour` and `rate_limits.seven_day` through the official status line JSON payload. See [Claude Code status line](https://code.claude.com/docs/en/statusline).
- **Google Antigravity:** receives model quota buckets via the local Antigravity Language Server (IDE & Desktop) through its localhost service, or via the official Antigravity CLI custom status line JSON payload. See [Antigravity CLI status line customization](https://antigravity.google/docs/cli/statusline/).

For a complete breakdown of design principles, architecture diagrams, and end-to-end data flows, see [Architecture & Design](docs/ARCHITECTURE.md).

The app does not call undocumented cloud endpoints, scrape web pages, or proxy AI requests through another server.

## Requirements

- macOS 12 or later.
- Go 1.24 or later.
- Xcode Command Line Tools:

  ```bash
  xcode-select --install
  ```

- An installed and authenticated Codex CLI for Codex tracking.
- Claude Code 2.1.251 or later with a Claude Pro or Max plan for Claude Code tracking.
- An installed and authenticated Google Antigravity CLI for Antigravity tracking.

## Build the macOS app

From the project directory, run:

```bash
./scripts/build.sh
```

The script will:

1. Download Go dependencies.
2. Run the full test suite.
3. Generate the `.icns` app icon.
4. Build the CGO application.
5. Create and locally sign the app bundle.
6. Create a ZIP archive for distribution.

Outputs:

```text
dist/AIQuota.app
dist/AIQuota-0.1.0-macos.zip
```

Launch the app:

```bash
open dist/AIQuota.app
```

Set a custom version:

```bash
VERSION=0.2.0 BUILD_NUMBER=2 ./scripts/build.sh
```

If you have an Apple Developer certificate, sign with your identity:

```bash
CODESIGN_IDENTITY="Developer ID Application: Your Name (TEAMID)" ./scripts/build.sh
```

The default signing identity is `-`, which creates an ad-hoc signature suitable for running the app on the Mac that built it.

## Publish a GitHub release

Install and authenticate GitHub CLI once:

```bash
brew install gh
gh auth login
```

Publish a release from a clean branch that exactly matches its remote branch:

```bash
make release VERSION=0.2.0
```

You can also run the script directly, with or without the `v` prefix:

```bash
./scripts/release.sh 0.2.0
./scripts/release.sh v0.2.0
```

Before building, the release script checks local tags, remote Git tags, and GitHub Releases using both `v0.2.0` and `0.2.0`. If any match already exists, the script exits with an error. Authentication, network, or GitHub API failures also stop the release instead of being treated as a missing version.

For a new version, the script:

1. Verifies the Git working tree is clean and synchronized with its remote branch.
2. Runs the standard test and macOS build pipeline.
3. Creates `AIQuota-0.2.0-macos.zip` and `SHA256SUMS`.
4. Creates tag `v0.2.0` at the current commit.
5. Publishes a GitHub Release with generated notes and both assets.

The default build number is a UTC timestamp. Override signing or the build number when needed:

```bash
CODESIGN_IDENTITY="Developer ID Application: Your Name (TEAMID)" \
BUILD_NUMBER=2026083101 \
make release VERSION=0.2.0
```

Ad-hoc signing is suitable for local builds. Distribution to other Macs without Gatekeeper warnings requires a Developer ID signature and Apple notarization.

## Development

```bash
go mod download
go test ./...
CGO_ENABLED=1 go run ./cmd/aiquota
```

Or use the Makefile:

```bash
make test
make run
make build
```

## Usage

### Codex

When the Codex CLI is installed and authenticated, AI quota discovers the `codex` executable and refreshes its quota once per minute. It checks common Homebrew, npm, nvm, and `~/.local/bin` locations because apps launched from Finder often receive a smaller `PATH` than Terminal sessions.

For a custom Codex location during development, set `AIQUOTA_CODEX_PATH` before launching the app.

### Claude Code

1. Open AI quota from the macOS menu bar.
2. Open **🧩 Providers → Claude Code**.
3. Select **Enable tracking**.
4. Open Claude Code and send at least one prompt.

When tracking is enabled, AI quota:

- Creates a timestamped backup of `~/.claude/settings.json`.
- Separately saves the current `statusLine` value.
- Installs its bridge at `~/Library/Application Support/AIQuota/bin/aiquota-bridge`.
- Passes through output from an existing status line command so the current Claude Code display remains unchanged.

Selecting **Disable tracking** restores the previous `statusLine` exactly. If another app changed the setting after tracking was enabled, AI quota stops without overwriting the newer configuration.

### Google Antigravity CLI

1. Open AI quota from the macOS menu bar.
2. Open **🧩 Providers → Google Antigravity**.
3. Select **Enable tracking**.
4. Open Antigravity CLI and send at least one prompt.

When tracking is enabled, AI quota:

- Creates a timestamped backup of `~/.gemini/antigravity-cli/settings.json`.
- Separately saves the current `statusLine` value.
- Installs its bridge at `~/Library/Application Support/AIQuota/bin/aiquota-antigravity-bridge`.
- Reads only the `quota` object from Google's documented status-line payload.
- Passes the original payload to an existing status-line command so its output remains available.
- Preserves the built-in Antigravity status line when no custom command was previously configured.

Selecting **Disable tracking** restores the previous `statusLine` exactly. If another app changed the setting after tracking was enabled, AI quota stops without overwriting the newer configuration.

The quota area only lists providers that have active quota data. Providers that still require setup are managed under **🧩 Providers**; when none are ready, the quota area shows a single waiting message.

## Local data

AI quota stores local state under:

```text
~/Library/Application Support/AIQuota/
```

This directory contains the latest quota snapshots, alert deduplication state, provider bridges, and status line backups. It never contains conversation content, project source code, email addresses, API keys, or OAuth tokens.

## Status indicators

- 🟢 More than 20% remaining.
- 🟡 6% to 20% remaining.
- 🔴 1% to 5% remaining.
- ⛔ Exhausted.
- ⚪ The window reset and is waiting for fresh provider data.

Remaining quota is calculated as `100 - used_percentage` from provider-reported data.

## Current limitations

- Claude Code sends quota fields only after the first API response in a session.
- Claude documents these status line fields for Pro and Max plans; Team and Enterprise accounts may not provide them.
- Google Antigravity CLI sends quota data through the status-line hook after the CLI has refreshed its model quota state.
- Antigravity IDE and Desktop apps require the background Language Server process to be running to retrieve live quota.
- Version 0.1 targets macOS and builds for the architecture of the Mac running the script.
- AI quota displays provider-reported values and does not estimate quota usage.

## Project structure

```text
cmd/aiquota/                 App entry point and status-line bridge modes
cmd/icon-gen/                macOS iconset generator
docs/ARCHITECTURE.md         Comprehensive system architecture and flow documentation
internal/alerts/             Alert thresholds and deduplication
internal/appcore/            Refresh orchestration and in-memory state
internal/config/             Application data and provider paths
internal/icon/               Programmatic tray and app icon rendering
internal/model/              Normalized providers, windows, and severity
internal/notify/             Native macOS UserNotifications bridge
internal/provider/           Provider extension interface
internal/provider/codex/     Codex App Server connector & installer
internal/provider/claude/    Claude status line connector, cache, and settings backup
internal/provider/antigravity/ Antigravity Language Server & CLI status line connectors
internal/storage/            Atomic JSON persistence
internal/tray/               Menu bar UI and provider management menus
packaging/macos/Info.plist   macOS bundle metadata
scripts/build.sh             Test, build, package, and sign the .app
scripts/release.sh           Validate, build, and publish a GitHub Release
Makefile                     Development and build shortcuts
go.mod                       Go module and dependency versions
```

## License

MIT

## Connection readiness

AIQuota also monitors MCP connections, local fs-log-data readiness, and the maintained Flights Shopping Hub preflight. Open the native readiness window for scoped results and repair actions. See [readiness configuration and verification](docs/readiness.md).
