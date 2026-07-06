---
name: pp-changedetection
description: "Every changedetection.io watch, tag, and notification from one CLI — plus offline cross-watch queries the web UI can't do. Trigger phrases: `what changed on my watches`, `list changedetection watches`, `which monitors are stale`, `show errored watches`, `use changedetection`, `run changedetection`."
author: "Filippo Merante Caparrotta"
license: "Apache-2.0"
argument-hint: "<command> [args] | install cli|mcp"
allowed-tools: "Read Bash"
metadata:
  openclaw:
    requires:
      bins:
        - changedetection-pp-cli
    install:
      - kind: go
        bins: [changedetection-pp-cli]
        module: github.com/mvanhorn/printing-press-library/library/monitoring/changedetection/cmd/changedetection-pp-cli
---

# changedetection.io — Printing Press CLI

## Prerequisites: Install the CLI

This skill drives the `changedetection-pp-cli` binary. **You must verify the CLI is installed before invoking any command from this skill.** If it is missing, install it first:

1. Install via the Printing Press installer. It defaults binaries to `$HOME/.local/bin` on macOS/Linux and `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows:
   ```bash
   npx -y @mvanhorn/printing-press-library install changedetection --cli-only
   ```
2. Verify: `changedetection-pp-cli --version`
3. Ensure the reported install directory is on `$PATH` for the agent/runtime that will invoke this skill.

If the `npx` install fails (no Node, offline, etc.), fall back to a direct Go install (requires Go 1.26.4 or newer). This installs into `$GOPATH/bin` (default `$HOME/go/bin`), so add that directory to `$PATH` instead:

```bash
go install github.com/mvanhorn/printing-press-library/library/monitoring/changedetection/cmd/changedetection-pp-cli@latest
```

If `--version` reports "command not found" after install, the runtime cannot see the binary directory on `$PATH`. Do not proceed with skill commands until verification succeeds.

Drive any self-hosted changedetection.io instance from the terminal or an agent: manage watches, tags, and notifications through the official API, and answer questions the UI never surfaces in one call. 'since' rolls up what changed recently, 'stale' finds dead monitors, 'errored' triages broken ones, and 'diff' reads the latest text change directly.

## When to Use This CLI

Use this CLI to operate a self-hosted changedetection.io instance from scripts or an agent: create and edit watches, manage tags and notification targets, and answer cross-watch questions (what changed recently, which watches are stale or errored) without clicking through the web UI. It is the right tool when you manage more than a handful of watches or want change data piped into other tooling.

## Anti-triggers

Do not use this CLI for:
- Do not use it to browse arbitrary websites; it only talks to your changedetection.io instance API.
- Do not use it to configure the changedetection.io server itself (data dir, ports, proxies) — that is server config, not the API.
- Do not use it as a general web-scraping tool; changedetection.io defines what is watched and how.

## Unique Capabilities

These capabilities aren't available in any other tool for this API.

### Local state that compounds
- **`since`** — Show every watch that changed in the last N hours or days in one call.

  _Pick this when an agent needs 'what moved since yesterday' instead of polling each watch._

  ```bash
  changedetection-pp-cli since 24h --agent
  ```
- **`stale`** — List watches that have not changed (or not been checked) in N days.

  _Use it to prune dead watches or find monitors that silently stopped firing._

  ```bash
  changedetection-pp-cli stale --days 30 --agent
  ```
- **`errored`** — List watches currently in an error/fetch-failed state.

  _Reach for it to triage broken monitors before trusting a 'no change' result._

  ```bash
  changedetection-pp-cli errored --agent
  ```

### Agent-native plumbing
- **`diff`** — Print the unified text difference between a watch's two most recent snapshots.

  _Use it to read what actually changed on a watch without hand-picking timestamps._

  ```bash
  changedetection-pp-cli diff <uuid>
  ```
- **`watch-search`** — Filter watches by text across URL, title, and error message (substring or regex).

  _Use it to locate the right watch UUID fast, including by error text the server search ignores._

  ```bash
  changedetection-pp-cli watch-search pricing --agent
  ```

## Command Reference

**bulk_import** — Manage bulk import

- `changedetection-pp-cli bulk-import` — Import a list of URLs to monitor with optional watch configuration. Accepts line-separated URLs in request body.

**find** — Manage find

- `changedetection-pp-cli find` — Search web page change monitors (watches) by URL or title text

**full-spec** — Manage full spec

- `changedetection-pp-cli full-spec` — Return the fully merged OpenAPI specification for this instance. Unlike the static `api-spec.

**notifications** — Configure global notification endpoints that can be used across all your watches. Supports various 
notification services including email, Discord, Slack, webhooks, and many other popular platforms. 
These settings serve as defaults that can be overridden at the individual watch or tag level.

The notification syntax uses [https://github.com/caronc/apprise](https://github.com/caronc/apprise).

- `changedetection-pp-cli notifications add` — Add one or more notification URLs to the configuration
- `changedetection-pp-cli notifications delete` — Delete one or more notification URLs from the configuration
- `changedetection-pp-cli notifications get` — Return the notification URL list from the configuration
- `changedetection-pp-cli notifications replace` — Replace all notification URLs with the provided list (can be empty)

**systeminfo** — Manage systeminfo

- `changedetection-pp-cli systeminfo` — Return information about the current system state

**tag** — Manage tag

- `changedetection-pp-cli tag create` — Create a single tag/group
- `changedetection-pp-cli tag delete` — Delete a tag/group and remove it from all web page change monitors (watches)
- `changedetection-pp-cli tag get` — Retrieve tag information, set notification_muted status, recheck all web page change monitors (watches) in tag.
- `changedetection-pp-cli tag update` — Update an existing tag using JSON

**tags** — Manage tags

- `changedetection-pp-cli tags` — Return list of available tags/groups

**watch** — Manage watch

- `changedetection-pp-cli watch create` — Create a single web page change monitor (watch). Requires at least `url` to be set.
- `changedetection-pp-cli watch delete` — Delete a web page change monitor (watch) and all related history
- `changedetection-pp-cli watch get` — Retrieve web page change monitor (watch) information and set muted/paused status. Returns the FULL Watch JSON.
- `changedetection-pp-cli watch list-watches` — Return concise list of available web page change monitors (watches) and basic info
- `changedetection-pp-cli watch update` — Update an existing web page change monitor (watch) using JSON.


### Finding the right command

When you know what you want to do but not which command does it, ask the CLI directly:

```bash
changedetection-pp-cli which "<capability in your own words>"
```

`which` resolves a natural-language capability query to the best matching command from this CLI's curated feature index. Exit code `0` means at least one match; exit code `2` means no confident match — fall back to `--help` or use a narrower query.

## Recipes

### Daily change digest

```bash
changedetection-pp-cli since 24h --agent
```

Roll up every watch that changed in the last 24 hours.

### Find dead monitors

```bash
changedetection-pp-cli stale --days 30 --agent
```

Surface watches that have not changed in a month for cleanup.

### Read the latest change

```bash
changedetection-pp-cli diff <uuid>
```

Print the unified text diff of a watch's two most recent snapshots.

### Narrow a large watch list

```bash
changedetection-pp-cli watch list --agent --select url,last_changed,last_error
```

Return only the high-signal fields from a big watch list instead of the full payload.

## Auth Setup

changedetection.io authenticates with an x-api-key header. Copy your key from the dashboard under Settings > API and export it as CHANGEDETECTION_API_KEY (or set it in the CLI config).

Run `changedetection-pp-cli doctor` to verify setup.

## Agent Mode

Add `--agent` to any command. Expands to: `--json --compact --no-input --no-color --yes`.

- **Pipeable** — JSON on stdout, errors on stderr
- **Filterable** — `--select` keeps a subset of fields. Dotted paths descend into nested structures; arrays traverse element-wise. Critical for keeping context small on verbose APIs:

  ```bash
  changedetection-pp-cli notifications get --agent --select id,name,status
  ```
- **Previewable** — `--dry-run` shows the request without sending
- **Offline-friendly** — sync/search commands can use the local SQLite store when available
- **Non-interactive** — never prompts, every input is a flag
- **Explicit retries** — use `--idempotent` only when an already-existing create should count as success, and use `--ignore-missing` only when a missing delete target should count as success

### Response envelope

Commands that read from the local store or the API wrap output in a provenance envelope:

```json
{
  "meta": {"source": "live" | "local", "synced_at": "...", "reason": "..."},
  "results": <data>
}
```

Parse `.results` for data and `.meta.source` to know whether it's live or local. A human-readable `N results (live)` summary is printed to stderr only when stdout is a terminal AND no machine-format flag (`--json`, `--csv`, `--compact`, `--quiet`, `--plain`, `--select`) is set — piped/agent consumers and explicit-format runs get pure JSON on stdout.

## Paths and state

Agents should treat the CLI's path resolver as part of the runtime contract:

- Use `--home <dir>` for one invocation, or set `CHANGEDETECTION_HOME=<dir>` to relocate all four path kinds under one root.
- Use per-kind env vars only when a specific kind must diverge: `CHANGEDETECTION_CONFIG_DIR`, `CHANGEDETECTION_DATA_DIR`, `CHANGEDETECTION_STATE_DIR`, `CHANGEDETECTION_CACHE_DIR`.
- Resolution order is per-kind env var, `--home`, `CHANGEDETECTION_HOME`, XDG (`XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`), then platform defaults.
- `config` contains settings like `config.toml` and profiles. `data` contains `credentials.toml`, `data.db`, cookies, and auth sidecars. `state` contains persisted queries, jobs, and `teach.log`. `cache` contains regenerable HTTP/cache files.
- Stored secrets live in `credentials.toml` under the data dir. Existing legacy `config.toml` secrets are read for compatibility and leave `config.toml` on the first auth write.
- Run `changedetection-pp-cli doctor --fail-on warn` to surface path and credential-location warnings. `agent-context` exposes a schema v4 `paths` block for agents that need the resolved dirs.
- For MCP, pass relocation through the MCP host config. The MCP binary does not inherit CLI flags:

  ```json
  {
    "mcpServers": {
      "changedetection": {
        "command": "changedetection-pp-mcp",
        "env": {
          "CHANGEDETECTION_HOME": "/srv/changedetection"
        }
      }
    }
  }
  ```

Fleet precedence: an inherited per-kind env var overrides an explicit `--home` for that kind. Use `CHANGEDETECTION_HOME` or per-kind vars as durable fleet levers, and use `--home` only for a single invocation. Relocation is not reversible by unsetting env vars; move files manually before clearing `CHANGEDETECTION_HOME`, or `doctor` will not find credentials left under the former root.

## Agent Feedback

When you (or the agent) notice something off about this CLI, record it:

```
changedetection-pp-cli feedback "the --since flag is inclusive but docs say exclusive"
changedetection-pp-cli feedback --stdin < notes.txt
changedetection-pp-cli feedback list --json --limit 10
```

Entries are stored locally as `feedback.jsonl` under the resolved data dir. They are never POSTed unless `CHANGEDETECTION_FEEDBACK_ENDPOINT` is set AND either `--send` is passed or `CHANGEDETECTION_FEEDBACK_AUTO_SEND=true`. Default behavior is local-only.

Write what *surprised* you, not a bug report. Short, specific, one line: that is the part that compounds.

## Output Delivery

Every command accepts `--deliver <sink>`. The output goes to the named sink in addition to (or instead of) stdout, so agents can route command results without hand-piping. Three sinks are supported:

| Sink | Effect |
|------|--------|
| `stdout` | Default; write to stdout only |
| `file:<path>` | Atomically write output to `<path>` (tmp + rename) |
| `webhook:<url>` | POST the output body to the URL (`application/json` or `application/x-ndjson` when `--compact`) |

Unknown schemes are refused with a structured error naming the supported set. Webhook failures return non-zero and log the URL + HTTP status on stderr.

## Named Profiles

A profile is a saved set of flag values, reused across invocations. Use it when a scheduled agent calls the same command every run with the same configuration - HeyGen's "Beacon" pattern.

```
changedetection-pp-cli profile save briefing --json
changedetection-pp-cli --profile briefing notifications get
changedetection-pp-cli profile list --json
changedetection-pp-cli profile show briefing
changedetection-pp-cli profile delete briefing --yes
```

Explicit flags always win over profile values; profile values win over defaults. `agent-context` lists all available profiles under `available_profiles` so introspecting agents discover them at runtime.

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Usage error (wrong arguments) |
| 3 | Resource not found |
| 4 | Authentication required |
| 5 | API error (upstream issue) |
| 7 | Rate limited (wait and retry) |
| 10 | Config error |

## Argument Parsing

Parse `$ARGUMENTS`:

1. **Empty, `help`, or `--help`** → show `changedetection-pp-cli --help` output
2. **Starts with `install`** → ends with `mcp` → MCP installation; otherwise → see Prerequisites above
3. **Anything else** → Direct Use (execute as CLI command with `--agent`)

## MCP Server Installation

1. Install the MCP server:
   ```bash
   go install github.com/mvanhorn/printing-press-library/library/monitoring/changedetection/cmd/changedetection-pp-mcp@latest
   ```
2. Register with Claude Code:
   ```bash
   claude mcp add changedetection-pp-mcp -- changedetection-pp-mcp
   ```
3. Verify: `claude mcp list`

## Direct Use

1. Check if installed: `which changedetection-pp-cli`
   If not found, offer to install (see Prerequisites at the top of this skill).
2. Match the user query to the best command from the Unique Capabilities and Command Reference above.
3. Execute with the `--agent` flag:
   ```bash
   changedetection-pp-cli <command> [subcommand] [args] --agent
   ```
4. If ambiguous, drill into subcommand help: `changedetection-pp-cli <command> --help`.
