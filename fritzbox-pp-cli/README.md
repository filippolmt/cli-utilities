# FRITZ!Box CLI

**Every FRITZ!Box feature the shell scripts have, plus a local database that remembers what your router forgets.**

FRITZ!Box exposes a large, fully self-describing TR-064 action surface plus a smart-home HTTP interface, but every existing tool is a stateless proxy for one request. This CLI mirrors the router into SQLite, so it answers questions no single call can: what changed on the network since yesterday (hosts diff), how often the line dropped this week (wan history), what the log said before the router rotated it away (log search). It reads the action catalog from your own firmware, so vendor extensions and future releases work without an update.

## Install

The recommended path installs both the `fritzbox-pp-cli` binary and the `pp-fritzbox` agent skill (Claude Code, Codex, Cursor, Gemini CLI, GitHub Copilot, and other agents supported by the upstream [`skills`](https://github.com/vercel-labs/skills) CLI) in one shot:

```bash
npx -y @mvanhorn/printing-press-library install fritzbox
```

For CLI only (no skill):

```bash
npx -y @mvanhorn/printing-press-library install fritzbox --cli-only
```

For skill only — installs the skill into the same agents as the default command above, but skips the CLI binary (use this to update or reinstall just the skill):

```bash
npx -y @mvanhorn/printing-press-library install fritzbox --skill-only
```

To constrain the skill install to one or more specific agents (repeatable — agent names match the [`skills`](https://github.com/vercel-labs/skills) CLI):

```bash
npx -y @mvanhorn/printing-press-library install fritzbox --agent claude-code
npx -y @mvanhorn/printing-press-library install fritzbox --agent claude-code --agent codex
```

### Without Node (Go fallback)

If `npx` isn't available (no Node, offline), install the CLI directly via Go (requires Go 1.26.5 or newer):

```bash
go install github.com/mvanhorn/printing-press-library/library/devices/fritzbox/cmd/fritzbox-pp-cli@latest
```

This installs the CLI only — no skill.

### Pre-built binary

Download a pre-built binary for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/fritzbox-current). On macOS, clear the Gatekeeper quarantine: `xattr -d com.apple.quarantine <binary>`. On Unix, mark it executable: `chmod +x <binary>`.

<!-- pp-hermes-install-anchor -->
## Install for Hermes

Install the CLI binary first. The installer writes binaries to a per-user managed bin directory by default: `$HOME/.local/bin` on macOS/Linux and `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows.

```bash
npx -y @mvanhorn/printing-press-library install fritzbox --cli-only
```

Then install the focused Hermes skill.

From the Hermes CLI:

```bash
hermes skills install mvanhorn/printing-press-library/cli-skills/pp-fritzbox --force
```

Inside a Hermes chat session:

```bash
/skills install mvanhorn/printing-press-library/cli-skills/pp-fritzbox --force
```

Restart the Hermes session or gateway if the newly installed skill is not visible immediately.

## Install for OpenClaw
Install both the CLI binary and the focused OpenClaw skill. The installer defaults binaries to a per-user bin directory (`$HOME/.local/bin` on macOS/Linux, `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows):

```bash
npx -y @mvanhorn/printing-press-library install fritzbox --agent openclaw
```

Restart the OpenClaw session or gateway if the newly installed skill is not visible immediately.

## Use with Claude Desktop

This CLI ships an [MCPB](https://github.com/modelcontextprotocol/mcpb) bundle — Claude Desktop's standard format for one-click MCP extension installs (no JSON config required).

To install:

1. Download the `.mcpb` for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/fritzbox-current).
2. Double-click the `.mcpb` file. Claude Desktop opens and walks you through the install.
3. Fill in `FRITZBOX_PASSWORD` when Claude Desktop prompts you.

Requires Claude Desktop 1.0.0 or later. Pre-built bundles ship for macOS Apple Silicon (`darwin-arm64`) and Windows (`amd64`, `arm64`); for other platforms, use the manual config below.

<details>
<summary>Manual JSON config (advanced)</summary>

If you can't use the MCPB bundle (older Claude Desktop, unsupported platform), install the MCP binary and configure it manually.


```bash
go install github.com/mvanhorn/printing-press-library/library/devices/fritzbox/cmd/fritzbox-pp-mcp@latest
```

Add to your Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "fritzbox": {
      "command": "fritzbox-pp-mcp",
      "env": {
        "FRITZBOX_PASSWORD": "<your-key>"
      }
    }
  }
}
```

</details>

## Authentication

FRITZ!Box uses one credential pair over two different mechanics. TR-064 on port 49000 authenticates with HTTP Digest. The smart-home and web-data surfaces need a session id minted through a challenge-response handshake, using PBKDF2 on modern FRITZ!OS and MD5 on older firmware. Set FRITZBOX_USERNAME and FRITZBOX_PASSWORD, then run auth login to mint and cache the session id; the CLI refreshes it automatically when it expires. TR-064 must be enabled on the router under Home Network, Network Settings, Allow access for applications.

## Quick Start

```bash
# confirm the router is reachable and the credentials work before anything else
fritzbox-pp-cli doctor

# mint the session id the smart-home and web-data surfaces need
fritzbox-pp-cli auth login

# see what is on the network right now
fritzbox-pp-cli hosts list --online

# check the internet connection, external address, and uptime
fritzbox-pp-cli wan status

# record the network, connection, and log into the local database
fritzbox-pp-cli snapshot

# the payoff of snapshotting: what changed since the last one
fritzbox-pp-cli hosts diff --since 24h

```

## Unique Features

These capabilities aren't available in any other tool for this API.

### Local state that compounds
- **`hosts diff`** — See which devices appeared, disappeared, or changed IP on your network since a point in time.

  _Reach for this when the question is what changed rather than what exists — it is the only way to spot a device that joined the network overnight._

  ```bash
  fritzbox-pp-cli hosts diff --since 24h --agent
  ```
- **`wan history`** — Show how often the internet connection dropped and when, reconstructed from stored uptime samples.

  _Use this to prove or disprove a flaky line before calling the ISP; a single status call cannot show a pattern._

  ```bash
  fritzbox-pp-cli wan history --days 7 --agent
  ```
- **`log search`** — Full-text search the router system log, including entries the router itself has already rotated away.

  _Reach for this when investigating an incident that happened earlier than the router's log buffer reaches._

  ```bash
  fritzbox-pp-cli log search internet --since 7d --agent
  ```
- **`presence`** — Report who is home by mapping people to their devices and reading stored last-seen times.

  _Use this when the question is about people rather than devices._

  ```bash
  fritzbox-pp-cli presence --agent
  ```
- **`health`** — One pass, warn, or fail verdict aggregating WAN state, WLAN state, recent log errors, new devices, and firmware currency.

  _Use this as the first call when something is wrong and you do not yet know where to look._

  ```bash
  fritzbox-pp-cli health --agent
  ```
- **`portmap audit`** — Flag port forwarding rules whose target host is no longer known to the router.

  _Reach for this during a security review; stale forwards are a classic self-hosting footgun._

  ```bash
  fritzbox-pp-cli portmap audit --agent
  ```

### Telephony that answers questions
- **`calls digest`** — Roll up recent calls by caller with names resolved from the phonebook.

  _Use this for who has been calling; use the raw call list only when individual timestamps matter._

  ```bash
  fritzbox-pp-cli calls digest --days 7 --agent
  ```
- **`calls unknown`** — List inbound numbers that are not in your phonebook, ranked by how often they called.

  _Use this to find repeat callers worth saving or blocking._

  ```bash
  fritzbox-pp-cli calls unknown --days 30 --agent
  ```

### Discovery over a large action surface
- **`actions search`** — Search the TR-064 action catalog of your own firmware by action, argument, or service name.

  _Reach for this before guessing at an action name — it maps a plain-language intent onto the exact call this firmware supports._

  ```bash
  fritzbox-pp-cli actions search connection --agent
  ```
- **`mesh`** — Render the mesh tree of repeaters and their connected clients, with device names resolved.

  _Reach for this to find out which access point a device is actually attached to._

  ```bash
  fritzbox-pp-cli mesh --agent
  ```
- **`actions diff`** — Show which TR-064 actions a firmware update added or removed.

  _Reach for this after a firmware update to find newly available capabilities._

  ```bash
  fritzbox-pp-cli actions diff --agent
  ```
- **`energy`** — Show power draw broken down by router subsystem, with samples stored for trend queries.

  _Use this to see which subsystem dominates the router's power budget._

  ```bash
  fritzbox-pp-cli energy --agent
  ```

## Recipes

### Find what joined the network overnight

```bash
fritzbox-pp-cli hosts diff --since 24h --agent --select added
```

Narrows a change report down to just the devices that appeared.

### Prove the line is unstable

```bash
fritzbox-pp-cli wan history --days 14 --agent
```

Reconstructs every disconnect in the last two weeks from stored uptime samples, which is the evidence an ISP support call needs.

### Find the right action without a browser

```bash
fritzbox-pp-cli actions search connection --agent --select service,action
```

Searches the action catalog of your own firmware and narrows the output to the two fields you need to make the call.

### Turn the guest network on for an evening

```bash
fritzbox-pp-cli wifi on --band guest --dry-run
```

Shows exactly what would change; add --confirm to apply it.

### Audit port forwarding after a cleanup

```bash
fritzbox-pp-cli portmap audit --agent
```

Cross-references every forwarding rule against the known host list and flags the ones pointing at devices that no longer exist.

## Usage

Run `fritzbox-pp-cli --help` for the full command reference and flag list.

## Paths & environment variables

This CLI separates local files into four path kinds:

| Kind | Contents |
|------|----------|
| `config` | User-editable settings such as `config.toml` and saved profiles |
| `data` | Durable local data: `credentials.toml`, `data.db`, cookies, browser-session proof files, and other auth sidecars |
| `state` | Runtime state such as persisted queries, jobs, and `teach.log` |
| `cache` | Regenerable HTTP/cache files |

Each kind resolves independently. The ladder is:

1. Per-kind env var: `FRITZBOX_CONFIG_DIR`, `FRITZBOX_DATA_DIR`, `FRITZBOX_STATE_DIR`, or `FRITZBOX_CACHE_DIR`
2. `--home <dir>` for this invocation
3. `FRITZBOX_HOME` for a flat relocated root
4. XDG env vars: `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`
5. Platform defaults matching existing installs

For containers and agent sandboxes, prefer a single relocated root:

```bash
export FRITZBOX_HOME=/srv/fritzbox
fritzbox-pp-cli doctor
```

Under `FRITZBOX_HOME=/srv/fritzbox`, the four dirs resolve to `/srv/fritzbox/config`, `/srv/fritzbox/data`, `/srv/fritzbox/state`, and `/srv/fritzbox/cache`.

MCP servers do not receive CLI flags from the host. Put relocation in the host `env` block:

```json
{
  "mcpServers": {
    "fritzbox": {
      "command": "fritzbox-pp-mcp",
      "env": {
        "FRITZBOX_HOME": "/srv/fritzbox"
      }
    }
  }
}
```

Precedence matters in fleets: an ambient per-kind variable such as `FRITZBOX_DATA_DIR` overrides an explicit `--home` for that kind. Use `FRITZBOX_HOME` or the per-kind variables for durable fleet relocation; treat `--home` as the weaker per-invocation lever.

Relocation is one-way. Unsetting `FRITZBOX_HOME` does not move files back to platform defaults, and `doctor` cannot find credentials left under a former root. Move the files manually before unsetting relocation variables.

Existing installs keep working because the platform-default rung matches the legacy layout. On the first auth write, stored secrets leave `config.toml` and are consolidated into `credentials.toml` under the data directory. Run `fritzbox-pp-cli doctor --fail-on warn` to check path and credential-location warnings in automation.

## Commands

### dect

DECT cordless handsets

- **`fritzbox-pp-cli dect`** - List the registered DECT handsets

### hosts

Devices known to the router

- **`fritzbox-pp-cli hosts`** - List every device the router knows, online and offline

### query

Raw FRITZ!OS data-path queries

- **`fritzbox-pp-cli query`** - Run an arbitrary FRITZ!OS data-path expression and print the raw JSON result

### tam

Answering machines

- **`fritzbox-pp-cli tam`** - List the configured answering machines and whether each is active

### wlan

Wireless network settings

- **`fritzbox-pp-cli wlan`** - Read the primary wireless network name and whether the access point is enabled


### Self-learning loop

This CLI caches per-question discovery so repeat queries skip the walk and structurally similar queries get answered via entity substitution. The loop also self-captures: every invocation is journaled locally, and failed-flag corrections plus fresh teaches surface as candidates on the next `recall` for confirm/reject judgment. Agents call `recall` before discovery and fire `teach &` after answering. See the `## Automatic learning` section in `SKILL.md` for the full protocol.

- **`fritzbox-pp-cli recall <query>`** - Look up cached resources for a query before running discovery
- **`fritzbox-pp-cli teach`** - Record a query -> resource mapping (silent on success, safe to background with `&`)
- **`fritzbox-pp-cli learnings list`** - Inspect taught rows
- **`fritzbox-pp-cli learnings forget <query>`** - Undo a teach
- **`fritzbox-pp-cli learnings candidates`** - List auto-captured candidates awaiting confirm/reject
- **`fritzbox-pp-cli learnings stats`** - Local loop metrics: recall hit rate, teach-to-reuse, playbook resolution, candidate counts
- **`fritzbox-pp-cli teach-pattern`** - Install a query/resource template up front
- **`fritzbox-pp-cli teach-lookup`** - Add an entity mapping (e.g. country code, team alias) for pattern substitution

Pass `--no-learn` or set `FRITZBOX_NO_LEARN=true` to disable the loop for deterministic flows.

The local store's schema version stamp is one-way: once this version of `fritzbox-pp-cli` opens the database, older binaries refuse it with a version error — upgrade the binary rather than downgrading.

## Output Formats

```bash
# Human-readable table (default in terminal, JSON when piped)
fritzbox-pp-cli dect

# JSON for scripting and agents
fritzbox-pp-cli dect --json

# Filter to specific fields
fritzbox-pp-cli dect --json --select id,name,status

# Dry run — show the request without sending
fritzbox-pp-cli dect --dry-run

# Agent mode — JSON + compact + no prompts in one flag
fritzbox-pp-cli dect --agent
```

## Agent Usage

This CLI is designed for AI agent consumption:

- **Non-interactive** - never prompts, every input is a flag
- **Pipeable** - `--json` output to stdout, errors to stderr
- **Filterable** - `--select id,name` returns only fields you need
- **Previewable** - `--dry-run` shows the request without sending
- **Read-only by default** - this CLI does not create, update, delete, publish, send, or mutate remote resources
- **Offline-friendly** - sync/search commands can use the local SQLite store when available
- **Agent-safe by default** - no colors or formatting unless `--human-friendly` is set

Exit codes: `0` success, `2` usage error, `3` not found, `4` auth error, `5` API error, `7` rate limited, `10` config error.

## Freshness

This CLI owns bounded freshness for registered store-backed read command paths. In `--data-source auto` mode, covered commands check the local SQLite store before serving results; stale or missing resources trigger a bounded refresh, and refresh failures fall back to the existing local data with a warning. `--data-source local` never refreshes, and `--data-source live` reads the API without mutating the local store.

Set `FRITZBOX_NO_AUTO_REFRESH=1` to disable the pre-read freshness hook while preserving the selected data source.

Covered command paths:
- `fritzbox-pp-cli dect`
- `fritzbox-pp-cli dect get`
- `fritzbox-pp-cli dect list`
- `fritzbox-pp-cli dect search`
- `fritzbox-pp-cli hosts`
- `fritzbox-pp-cli hosts get`
- `fritzbox-pp-cli hosts list`
- `fritzbox-pp-cli hosts search`
- `fritzbox-pp-cli tam`
- `fritzbox-pp-cli tam get`
- `fritzbox-pp-cli tam list`
- `fritzbox-pp-cli tam search`

JSON outputs that use the generated provenance envelope include freshness metadata at `meta.freshness`. This metadata describes the freshness decision for the covered command path; it does not claim full historical backfill or API-specific enrichment.

## Health Check

```bash
fritzbox-pp-cli doctor
```

Verifies configuration, credentials, and connectivity to the API.

## Configuration

Run `fritzbox-pp-cli doctor` to see the resolved config, data, state, and cache directories. The platform-default config path is `~/.config/fritzbox-pp-cli/config.toml`; `--home`, `FRITZBOX_HOME`, and per-kind env vars can relocate it.

Static request headers can be configured under `headers`; per-command header overrides take precedence.

Environment variables:

| Name | Kind | Required | Description |
| --- | --- | --- | --- |
| `FRITZBOX_PASSWORD` | per_call | No | Set to your API credential. |
| `FRITZBOX_SID` | per_call | No | Set to your API credential. |

### agentcookie (optional)

If you use agentcookie to sync secrets across machines, this CLI auto-adopts agentcookie-managed credentials with no extra setup. When the daemon writes to this CLI's config, `fritzbox-pp-cli doctor` reports `agentcookie: detected` and `auth-status` labels the source as `agentcookie`. Skip this section if you don't use agentcookie - the CLI works the same as any other.

## Troubleshooting
**Authentication errors (exit code 4)**
- Run `fritzbox-pp-cli doctor` to check credentials
- Verify the environment variable is set: `echo $FRITZBOX_PASSWORD`
**Not found errors (exit code 3)**
- Check the resource ID is correct
- Run the `list` command to see available items

### API-specific
- **Every TR-064 call returns HTTP 401** — Enable TR-064 on the router under Home Network, Network Settings, Allow access for applications, and confirm FRITZBOX_USERNAME matches a user that exists on the box.
- **Smart-home, log, or mesh commands fail while TR-064 commands work** — The session id has expired or was never minted; run fritzbox-pp-cli auth login.
- **hosts diff, wan history, or log search reports nothing** — They read local history; run fritzbox-pp-cli snapshot at least twice with time in between, ideally on a schedule.
- **fritzbox-pp-cli sync stores zero records** — The generated sync path does not send this API's data-path expression; run fritzbox-pp-cli snapshot instead, which refreshes the same local mirror.
- **Smart-home commands return an empty device list** — No smart-home actors are paired with this router; pair one in the FRITZ!Box user interface under Smart Home first.
- **Login is rejected after several attempts** — FRITZ!OS applies a rising login block time; wait for the period the error reports before retrying.

## Sources & Inspiration

This CLI was built by studying these projects and resources:

- [**fritzconnection**](https://github.com/kbr/fritzconnection) — Python
- [**FritzBoxShell**](https://github.com/jhubig/FritzBoxShell) — Shell
- [**fritzbox_exporter**](https://github.com/sberk42/fritzbox_exporter) — Go
- [**gofritz**](https://github.com/nitram509/gofritz) — Go
- [**fbtr64toolbox**](https://github.com/MarcusRoeckrath/fbtr64toolbox) — Shell
- [**fritz_TR-064**](https://github.com/sky321/fritz_TR-064) — Python
- [**mcp-fritzbox**](https://github.com/ghbalf/mcp-fritzbox) — TypeScript
- [**fritzbox-mcp-server**](https://github.com/kambriso/fritzbox-mcp-server) — Python

Generated by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)
