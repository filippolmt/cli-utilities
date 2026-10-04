# Subito CLI

**Search Subito.it without a browser, rank ads against the real market, and watch for new listings from cron.**

subito-pp-cli talks to the same search backend the site uses, so it needs no Chrome, extension or login. 'market deals' ranks ads by % gap from a like-for-like median, 'ads risk' checks scam signals, 'market suggest' prices what you sell, and 'watch run' prints only what is new since the last run.

Learn more at [Subito](https://www.subito.it).

Created by [@filippolmt](https://github.com/filippolmt) (Filippo Merante Caparrotta).

## Install

The recommended path installs both the `subito-pp-cli` binary and the `pp-subito` agent skill (Claude Code, Codex, Cursor, Gemini CLI, GitHub Copilot, and other agents supported by the upstream [`skills`](https://github.com/vercel-labs/skills) CLI) in one shot:

```bash
npx -y @mvanhorn/printing-press-library install subito
```

For CLI only (no skill):

```bash
npx -y @mvanhorn/printing-press-library install subito --cli-only
```

For skill only — installs the skill into the same agents as the default command above, but skips the CLI binary (use this to update or reinstall just the skill):

```bash
npx -y @mvanhorn/printing-press-library install subito --skill-only
```

To constrain the skill install to one or more specific agents (repeatable — agent names match the [`skills`](https://github.com/vercel-labs/skills) CLI):

```bash
npx -y @mvanhorn/printing-press-library install subito --agent claude-code
npx -y @mvanhorn/printing-press-library install subito --agent claude-code --agent codex
```

### Without Node (Go fallback)

If `npx` isn't available (no Node, offline), install the CLI directly via Go (requires Go 1.26.6 or newer):

```bash
go install github.com/mvanhorn/printing-press-library/library/commerce/subito/cmd/subito-pp-cli@latest
```

This installs the CLI only — no skill.

### Pre-built binary

Download a pre-built binary for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/subito-current). On macOS, clear the Gatekeeper quarantine: `xattr -d com.apple.quarantine <binary>`. On Unix, mark it executable: `chmod +x <binary>`.

<!-- pp-hermes-install-anchor -->
## Install for Hermes

Install the CLI binary first. The installer writes binaries to a per-user managed bin directory by default: `$HOME/.local/bin` on macOS/Linux and `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows.

```bash
npx -y @mvanhorn/printing-press-library install subito --cli-only
```

Then install the focused Hermes skill.

From the Hermes CLI:

```bash
hermes skills install mvanhorn/printing-press-library/cli-skills/pp-subito --force
```

Inside a Hermes chat session:

```bash
/skills install mvanhorn/printing-press-library/cli-skills/pp-subito --force
```

Restart the Hermes session or gateway if the newly installed skill is not visible immediately.

## Install for OpenClaw
Install both the CLI binary and the focused OpenClaw skill. The installer defaults binaries to a per-user bin directory (`$HOME/.local/bin` on macOS/Linux, `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows):

```bash
npx -y @mvanhorn/printing-press-library install subito --agent openclaw
```

Restart the OpenClaw session or gateway if the newly installed skill is not visible immediately.

## Use with Claude Desktop

This CLI ships an [MCPB](https://github.com/modelcontextprotocol/mcpb) bundle — Claude Desktop's standard format for one-click MCP extension installs (no JSON config required).

To install:

1. Download the `.mcpb` for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/subito-current).
2. Double-click the `.mcpb` file. Claude Desktop opens and walks you through the install.

Requires Claude Desktop 1.0.0 or later. Pre-built bundles ship for macOS Apple Silicon (`darwin-arm64`) and Windows (`amd64`, `arm64`); for other platforms, use the manual config below.

<details>
<summary>Manual JSON config (advanced)</summary>

If you can't use the MCPB bundle (older Claude Desktop, unsupported platform), install the MCP binary and configure it manually.


```bash
go install github.com/mvanhorn/printing-press-library/library/commerce/subito/cmd/subito-pp-mcp@latest
```

Add to your Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "subito": {
      "command": "subito-pp-mcp"
    }
  }
}
```

</details>

## Authentication

No account or key. Subito's public search is anonymous. Plain curl is blocked by the Akamai edge, but this CLI's Go HTTP client is not.

## Quick Start

```bash
# Check the install without touching the network
subito-pp-cli doctor --dry-run

# Region ids for --region-id
subito-pp-cli geo regions

# A live search
subito-pp-cli ads search --query "bici da corsa" --category-id 41 --region-id 4 --price-max 1500

# Rank those ads against the market
subito-pp-cli market deals "bici da corsa" --category-id 41 --region-id 4

# Save the search; 'watch run' from cron then prints only new ads
subito-pp-cli watch add bici --query "bici da corsa" --category-id 41 --region-id 4

```

## Unique Features

These capabilities aren't available in any other tool for this API.

### Market intelligence
- **`market deals`** — Rank every ad for a search by how far it sits below or above the like-for-like market price.

  _Reach for this when asked whether a listing is a good price or which listings are the best deals._

  ```bash
  subito-pp-cli market deals "iphone 15 128" --category-id 12 --shippable --agent
  ```
- **`market suggest`** — Get a listing price, a minimum and a quick-sale price from at least five comparable ads (asking prices of private sellers; no price below five comparables).

  _Use when the user is about to sell something on Subito and needs a price._

  ```bash
  subito-pp-cli market suggest "yamaha tracer 9 gt" --category-id 3 --agent
  ```

### Buyer safety
- **`ads risk`** — Check one ad for mechanical scam signals with the evidence for each. Photos are not checked and the seller check covers only ads this CLI has already seen, so "ok" is not a clearance.

  _Use before contacting a seller or paying for an item._

  ```bash
  subito-pp-cli ads risk https://www.subito.it/biciclette/bici-da-corsa-milano-663258568.htm --agent
  ```
- **`ads history`** — See when an ad was first seen, every price change, renewals and likely reposts, from what earlier searches and watches recorded locally.

  _Use to judge negotiating leverage on an ad that has been around or keeps dropping._

  ```bash
  subito-pp-cli ads history 663258568 --agent
  ```

## Recipes

### Cheapest private iPhones that ship

```bash
subito-pp-cli ads search --query "iphone 15" --category-id 12 --shippable --advertiser-type 0,2 --sort priceasc --agent --select subject,urls.default,geo.town.value
```

Narrows the large search payload to title, link and town.

### Deals below market

```bash
subito-pp-cli market deals "canon r6" --verdict below --agent
```

Ads whose title has every keyword, ranked by % gap from the median of the same seller type (private vs shop).

### Price before selling

```bash
subito-pp-cli market suggest "yamaha tracer 9 gt" --category-id 3
```

Listing, minimum and quick-sale price from at least five live private comparables.

### Cron alert for new listings

```bash
subito-pp-cli watch add bici --query "bici da corsa" --category bici --region lombardia --price-max 800
```

Save the search once; then run 'subito-pp-cli watch run --agent --exit-code' from cron. The first run records a baseline and prints nothing; later runs print new ads and price changes and exit 6 when there are any.

### Read one ad in full

```bash
subito-pp-cli ads get 663258568 --agent --select detail.name,detail.price,detail.description,detail.images
```

Title, price, description and photo URLs from the ad page; works with the URL, urn or list id.

## Usage

Run `subito-pp-cli --help` for the full command reference and flag list.

## Paths & environment variables

This CLI separates local files into four path kinds:

| Kind | Contents |
|------|----------|
| `config` | User-editable settings such as `config.toml` and saved profiles |
| `data` | Durable local data such as `data.db` |
| `state` | Runtime state such as persisted queries, jobs, and `teach.log` |
| `cache` | Regenerable HTTP/cache files |

Each kind resolves independently. The ladder is:

1. Per-kind env var: `SUBITO_CONFIG_DIR`, `SUBITO_DATA_DIR`, `SUBITO_STATE_DIR`, or `SUBITO_CACHE_DIR`
2. `--home <dir>` for this invocation
3. `SUBITO_HOME` for a flat relocated root
4. XDG env vars: `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`
5. Platform defaults matching existing installs

For containers and agent sandboxes, prefer a single relocated root:

```bash
export SUBITO_HOME=/srv/subito
subito-pp-cli doctor
```

Under `SUBITO_HOME=/srv/subito`, the four dirs resolve to `/srv/subito/config`, `/srv/subito/data`, `/srv/subito/state`, and `/srv/subito/cache`.

MCP servers do not receive CLI flags from the host. Put relocation in the host `env` block:

```json
{
  "mcpServers": {
    "subito": {
      "command": "subito-pp-mcp",
      "env": {
        "SUBITO_HOME": "/srv/subito"
      }
    }
  }
}
```

Precedence matters in fleets: an ambient per-kind variable such as `SUBITO_DATA_DIR` overrides an explicit `--home` for that kind. Use `SUBITO_HOME` or the per-kind variables for durable fleet relocation; treat `--home` as the weaker per-invocation lever.

Relocation is one-way. Unsetting `SUBITO_HOME` does not move files back to platform defaults, and `doctor` cannot find files left under a former root. Move the files manually before unsetting relocation variables.

Existing installs keep working because the platform-default rung matches the legacy layout. Run `subito-pp-cli doctor --fail-on warn` to check path warnings in automation.

## Commands

### ads

Search Subito listings (annunci) through the site's own search backend

- **`subito-pp-cli ads check`** - Ask whether a search has new listings since a given listing urn (cheap, no listing data)
- **`subito-pp-cli ads recommended`** - Listings Subito recommends next to one listing (similar items); pass the urn field of a listing from ads search or market deals output
- **`subito-pp-cli ads search`** - Search listings by keyword, category, place, price and more. Returns newest first by default.

### categories

Subito categories and the filters each one accepts

- **`subito-pp-cli categories filters`** - Show the search filters a category accepts: query-string key, type and value source
- **`subito-pp-cli categories list`** - List every category with its id and macro-category

### geo

Italian regions, provinces and towns with the ids Subito search expects

- **`subito-pp-cli geo provinces`** - List the provinces of a region (Subito calls them cities)
- **`subito-pp-cli geo regions`** - List the 20 regions with their ids
- **`subito-pp-cli geo towns`** - List the towns of a province with ISTAT codes and coordinates

### values

Look up the allowed values of a filter (brands, models, fuels, conditions...)

- **`subito-pp-cli values <value_path>`** - Values of one filter list; the path comes from 'categories filters' datasource, without /v1/values/


### Self-learning loop

This CLI caches per-question discovery so repeat queries skip the walk and structurally similar queries get answered via entity substitution. The loop also self-captures: every invocation is journaled locally, and failed-flag corrections plus fresh teaches surface as candidates on the next `recall` for confirm/reject judgment. Agents call `recall` before discovery and fire `teach &` after answering. See the `## Automatic learning` section in `SKILL.md` for the full protocol.

- **`subito-pp-cli recall <query>`** - Look up cached resources for a query before running discovery
- **`subito-pp-cli teach`** - Record a query -> resource mapping (silent on success, safe to background with `&`)
- **`subito-pp-cli learnings list`** - Inspect taught rows
- **`subito-pp-cli learnings forget <query>`** - Undo a teach
- **`subito-pp-cli learnings candidates`** - List auto-captured candidates awaiting confirm/reject
- **`subito-pp-cli learnings stats`** - Local loop metrics: recall hit rate, teach-to-reuse, playbook resolution, candidate counts
- **`subito-pp-cli teach-pattern`** - Install a query/resource template up front
- **`subito-pp-cli teach-lookup`** - Add an entity mapping (e.g. country code, team alias) for pattern substitution

Pass `--no-learn` or set `SUBITO_NO_LEARN=true` to disable the loop for deterministic flows.

The local store's schema version stamp is one-way: once this version of `subito-pp-cli` opens the database, older binaries refuse it with a version error — upgrade the binary rather than downgrading.

## Output Formats

```bash
# Human-readable table (default in terminal, JSON when piped)
subito-pp-cli ads search

# JSON for scripting and agents
subito-pp-cli ads search --json
# Filter to specific fields
subito-pp-cli ads search --json --select urn,subject,body

# Dry run — show the request without sending
subito-pp-cli ads search --dry-run

# Agent mode — JSON + compact + no prompts in one flag
subito-pp-cli ads search --agent
```

## Agent Usage

This CLI is designed for AI agent consumption:

- **Non-interactive** - never prompts, every input is a flag
- **Pipeable** - `--json` output to stdout, errors to stderr
- **Filterable** - `--select <field>[,<field>...]` returns only fields you need
- **Previewable** - `--dry-run` shows the request without sending
- **Read-only by default** - this CLI does not create, update, delete, publish, send, or mutate remote resources
- **Offline-friendly** - sync/search commands can use the local SQLite store when available
- **Agent-safe by default** - no colors or formatting unless `--human-friendly` is set

Exit codes: `0` success, `2` usage error, `3` not found, `5` API error, `7` rate limited, `10` config error.

## Health Check

```bash
subito-pp-cli doctor
```

Verifies configuration and connectivity to the API.

## Configuration

Run `subito-pp-cli doctor` to see the resolved config, data, state, and cache directories. The platform-default config path is `~/.config/subito-pp-cli/config.toml`; `--home`, `SUBITO_HOME`, and per-kind env vars can relocate it.

Static request headers can be configured under `headers`; per-command header overrides take precedence.

## Troubleshooting
**Not found errors (exit code 3)**
- Check the resource ID is correct
- Run the `list` command to see available items

### API-specific
- **HTTP 403 Access Denied** — Subito's Akamai edge blocked the request; wait a few minutes and slow down with --rate-limit 0.5
- **Search returns models you did not ask for** — Use market deals/suggest/stats, which keep only titles with every keyword and drop other variants (pro, plus, max); or add --title-only to ads search
- **values returns 400 brand-invalid** — Use the zero-padded key from 'subito-pp-cli values cars/brands --category 2', e.g. 000083
- **ads history says not seen locally** — History only covers ads earlier searches recorded; run 'subito-pp-cli market deals <query>' or a watch over time first

## Sources & Inspiration

This CLI was built by studying these projects and resources:

- [**subito-it-searcher**](https://github.com/morrolinux/subito-it-searcher) — Python (158 stars)
- [**SubitoScanner**](https://github.com/drego85/SubitoScanner) — Python (10 stars)
- [**subitoo**](https://github.com/Kianda/subitoo) — Python (4 stars)
- [**subito-cli**](https://github.com/remorses/subito-cli) — TypeScript (2 stars)
- [**Subito-Advanced-Filters**](https://github.com/sawdsawd/Subito-Advanced-Filters) — Python (2 stars)
- [**Casa Radar (subito-it-property-scraper)**](https://github.com/Wiwo99/subito-it-property-scraper) — Python
- [**subito_scraper**](https://github.com/mattia93/subito_scraper) — Python

Generated by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)
