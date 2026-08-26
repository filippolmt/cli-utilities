---
name: pp-fritzbox
description: "Every FRITZ!Box feature the shell scripts have, plus a local database that remembers what your router forgets. Trigger phrases: `who is on my network`, `check my internet connection`, `turn on the guest wifi`, `who called me`, `did my internet drop`, `use fritzbox`, `run fritzbox`."
author: "Filippo Merante Caparrotta"
license: "Apache-2.0"
argument-hint: "<command> [args] | install cli|mcp"
allowed-tools: "Read Bash"
metadata:
  openclaw:
    requires:
      bins:
        - fritzbox-pp-cli
    install:
      - kind: go
        bins: [fritzbox-pp-cli]
        module: github.com/mvanhorn/printing-press-library/library/devices/fritzbox/cmd/fritzbox-pp-cli
---

# FRITZ!Box — Printing Press CLI

## Prerequisites: Install the CLI

This skill drives the `fritzbox-pp-cli` binary. **You must verify the CLI is installed before invoking any command from this skill.** If it is missing, install it first:

1. Install via the Printing Press installer. It defaults binaries to `$HOME/.local/bin` on macOS/Linux and `%LOCALAPPDATA%\Programs\PrintingPress\bin` on Windows:
   ```bash
   npx -y @mvanhorn/printing-press-library install fritzbox --cli-only
   ```
2. Verify: `fritzbox-pp-cli --version`
3. Ensure the reported install directory is on `$PATH` for the agent/runtime that will invoke this skill.

If the `npx` install fails before this CLI has a public-library category, install Node or use the category-specific Go fallback after publish.

If `--version` reports "command not found" after install, the runtime cannot see the binary directory on `$PATH`. Do not proceed with skill commands until verification succeeds.

FRITZ!Box exposes a large, fully self-describing TR-064 action surface plus a smart-home HTTP interface, but every existing tool is a stateless proxy for one request. This CLI mirrors the router into SQLite, so it answers questions no single call can: what changed on the network since yesterday (hosts diff), how often the line dropped this week (wan history), what the log said before the router rotated it away (log search). It reads the action catalog from your own firmware, so vendor extensions and future releases work without an update.

## When to Use This CLI

Use this CLI for any question about an AVM FRITZ!Box router: what is on the network, whether the internet connection is healthy and how stable it has been, who called, what the system log said, and what the smart-home actors are doing. It is the right choice when the question spans time or entities rather than a single reading, because it keeps a local mirror. It is also the right choice for driving the router non-interactively, since every command supports structured output, field selection, dry runs, and typed exit codes.

## Anti-triggers

Do not use this CLI for:
- Do not use this CLI for routers from other vendors; it speaks TR-064 as implemented by AVM and the vendor-specific extensions will not exist elsewhere.
- Do not use this CLI to reach a FRITZ!Box over the internet; it targets a device on the local network and does not implement MyFRITZ remote access.
- Do not use this CLI to restore a configuration backup or perform a firmware upgrade; those are destructive operations that belong in the router UI.
- Do not use this CLI as a monitoring daemon; it samples on demand rather than running continuously, so use the metrics command with an external scheduler instead.

## Unique Capabilities

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

## Command Reference

**dect** — DECT cordless handsets

- `fritzbox-pp-cli dect` — List the registered DECT handsets

**hosts** — Devices known to the router

- `fritzbox-pp-cli hosts` — List every device the router knows, online and offline

**query** — Raw FRITZ!OS data-path queries

- `fritzbox-pp-cli query` — Run an arbitrary FRITZ!OS data-path expression and print the raw JSON result

**tam** — Answering machines

- `fritzbox-pp-cli tam` — List the configured answering machines and whether each is active

**wlan** — Wireless network settings

- `fritzbox-pp-cli wlan` — Read the primary wireless network name and whether the access point is enabled


## Freshness Contract

This printed CLI owns bounded freshness only for registered store-backed read command paths. In `--data-source auto` mode, those paths check `sync_state` and may run a bounded refresh before reading local data. `--data-source local` never refreshes. `--data-source live` reads the API and does not mutate the local store. Set `FRITZBOX_NO_AUTO_REFRESH=1` to skip the freshness hook without changing source selection.

Covered paths:

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

When JSON output uses the generated provenance envelope, freshness metadata appears at `meta.freshness`. Treat it as current-cache freshness for the covered command path, not a guarantee of complete historical backfill or API-specific enrichment.

### Finding the right command

When you know what you want to do but not which command does it, ask the CLI directly:

```bash
fritzbox-pp-cli which "<capability in your own words>"
```

`which` resolves a natural-language capability query to the best matching command from this CLI's curated feature index. Exit code `0` means at least one match; exit code `2` means no confident match — fall back to `--help` or use a narrower query.

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

## Auth Setup

FRITZ!Box uses one credential pair over two different mechanics. TR-064 on port 49000 authenticates with HTTP Digest. The smart-home and web-data surfaces need a session id minted through a challenge-response handshake, using PBKDF2 on modern FRITZ!OS and MD5 on older firmware. Set FRITZBOX_USERNAME and FRITZBOX_PASSWORD, then run auth login to mint and cache the session id; the CLI refreshes it automatically when it expires. TR-064 must be enabled on the router under Home Network, Network Settings, Allow access for applications.

Run `fritzbox-pp-cli doctor` to verify setup.

## Agent Mode

Add `--agent` to any command. Expands to: `--json --compact --no-input --no-color --yes`.

- **Pipeable** — JSON on stdout, errors on stderr
- **Filterable** — `--select` keeps a subset of fields. Dotted paths descend into nested structures; arrays traverse element-wise. Critical for keeping context small on verbose APIs:

  ```bash
  fritzbox-pp-cli dect --agent --select id,name,status
  ```
- **Previewable** — `--dry-run` shows the request without sending
- **Offline-friendly** — sync/search commands can use the local SQLite store when available
- **Non-interactive** — never prompts, every input is a flag
- **Read-only** — do not use this CLI for create, update, delete, publish, comment, upvote, invite, order, send, or other mutating requests

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

- Use `--home <dir>` for one invocation, or set `FRITZBOX_HOME=<dir>` to relocate all four path kinds under one root.
- Use per-kind env vars only when a specific kind must diverge: `FRITZBOX_CONFIG_DIR`, `FRITZBOX_DATA_DIR`, `FRITZBOX_STATE_DIR`, `FRITZBOX_CACHE_DIR`.
- Resolution order is per-kind env var, `--home`, `FRITZBOX_HOME`, XDG (`XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`), then platform defaults.
- `config` contains settings like `config.toml` and profiles. `data` contains `credentials.toml`, `data.db`, cookies, and auth sidecars. `state` contains persisted queries, jobs, and `teach.log`. `cache` contains regenerable HTTP/cache files.
- Stored secrets live in `credentials.toml` under the data dir. Existing legacy `config.toml` secrets are read for compatibility and leave `config.toml` on the first auth write.
- Run `fritzbox-pp-cli doctor --fail-on warn` to surface path and credential-location warnings. `agent-context` exposes a schema v4 `paths` block for agents that need the resolved dirs.
- For MCP, pass relocation through the MCP host config. The MCP binary does not inherit CLI flags:

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

Fleet precedence: an inherited per-kind env var overrides an explicit `--home` for that kind. Use `FRITZBOX_HOME` or per-kind vars as durable fleet levers, and use `--home` only for a single invocation. Relocation is not reversible by unsetting env vars; move files manually before clearing `FRITZBOX_HOME`, or `doctor` will not find credentials left under the former root.

## Automatic learning

This CLI ships a self-capturing learning loop. The CLI does its own bookkeeping: every invocation is journaled locally, a failed flag followed by a corrected retry auto-derives a `flag_alias` candidate, and a `teach` on a query family without a playbook auto-synthesizes a `playbook_candidate` from the session's journal. Your job is judgment only: `recall` first, act on surfaced candidates, `teach` the final answer, `playbook amend` when you observe a correction. You never record failures by hand.

### Step 1: `recall` before any discovery

Before list/search/drill commands on a new user question, run:

```bash
fritzbox-pp-cli recall "<user's question>" --agent
```

The response envelope:

```json
{
  "query": "...",
  "normalized": "<normalized form>",
  "query_entities": ["..."],
  "found": true | false,
  "match_score": 0.0,
  "results": [
    { "resource_id": "...", "resource_type": "...", "venue": "...",
      "confidence": 2, "entity_match": "exact|partial|unknown",
      "source": "taught|preseed|pattern", "warnings": ["..."] }
  ],
  "mismatches": [ /* only when --debug-mismatches */ ],
  "warnings": [ /* top-level */ ],
  "candidates": [
    { "id": 12, "class": "flag_alias | playbook_candidate",
      "summary": "...", "sightings": 3, "last_seen": "...",
      "rationale": "...",
      "next_action": ["<trial command>", "fritzbox-pp-cli learnings confirm 12"] }
  ],
  "playbook": {
    "query_family": "...",
    "playbook": {
      "steps": [ { "cmd": "<command with {slot} substitution>", "purpose": "..." } ],
      "entity_slots": ["$ENTITY"],
      "expected_tool_calls": 3
    },
    "slots_resolved": { "$ENTITY": { "token": "<live token>", "canonical": "<canonical>" } },
    "notes": "<workarounds + gotchas for this query family>"
  },
  "notes": "<duplicate surface for non-playbook callers>"
}
```

Empty-store short-circuit: if the store has no learnings, playbooks, or candidates yet (recall finds nothing and `learnings list` and `learnings candidates` are both empty), skip recall for the rest of this session instead of taxing every query; resume recall-first once something has been taught.

### Step 2: decision tree

Read `candidates`, `playbook`, `notes`, `results[0]`, and warnings in that order:

```
if Candidates present (warnings include "candidates_present"):
    -> candidates are try-then-confirm, never facts. Follow each candidate's
       two-step next_action verbatim: run the trial command first, then run
       `learnings confirm <id>` only after the trial verified the behavior.
       Reject a wrong candidate with `learnings reject <id>`.
    -> NEVER re-teach something recall surfaced as a candidate; confirm or
       reject that candidate instead of teaching a duplicate.
    -> candidates ride alongside playbooks and resource hits, not instead of
       them; continue with the branches below after acting on them.

if Playbook present:
    -> READ Playbook.notes verbatim FIRST (workarounds + gotchas the CLI surface doesn't expose)
    -> replay Playbook.steps in order, substituting Playbook.slots_resolved entries
       for the entity slot tokens. If a step's slot is unresolved, fall back to
       discovery for that step only.
    -> the Playbook's expected_tool_calls is a budget; if you find yourself running
       materially more, record the divergence via `fritzbox-pp-cli playbook amend`
       at end-of-session.

elif Notes present (no Playbook):
    -> read Notes verbatim before any discovery step; they carry known gotchas
       for this query family even when no structured choreography exists yet.

elif Found AND Results[0].EntityMatch == "exact" AND Results[0].Confidence >= 2:
    -> skip discovery; fetch live data for Results[*].ResourceID in parallel

elif Found AND Results[0].EntityMatch == "partial":
    -> candidate hint, NOT a hit; read the resource title to validate before trusting

elif (any row in Mismatches[] when --debug-mismatches was passed):
    -> treat as cold start; the stored learning is for a different entity
       (different canonical resolved from query_entities)

else:  // Found == false, no playbook, no notes
    -> cold start; run discovery normally; teach the answer afterward (Step 4).
       If the family has no playbook yet, that teach auto-synthesizes a
       playbook candidate from this session's journal - you do not need to
       record one by hand.
```

Playbook and Notes are orthogonal to the per-resource path. A recall response can carry both a Playbook AND a `Results[]` hit - use both: the Playbook tells you which choreography to run; the resource hits short-circuit specific steps. Default to skipping `mismatches`; pass `--debug-mismatches` only when investigating cold-start surprises.

Candidate judgment details: `learnings confirm <id>` prints the candidate's full payload before materializing it - check that the printed payload matches the behavior you verified. `learnings reject <id>` tombstones the derivation signature so the same candidate does not resurface. The envelope carries only the few candidates worth acting on now; `fritzbox-pp-cli learnings candidates` lists the full open set.

Graceful degradation: if `learnings confirm` is an unknown command, you are driving an older binary - ignore the candidates guidance and follow the rest of the protocol.

### Step 3: always read `warnings`

- `low_confidence`: row exists at `confidence<2`. Treat as a hint, not a skip-discovery hit.
- `resource_not_in_store`: the local store doesn't have the resource the learning points at. The match validator couldn't classify entities — direct-fetch and re-evaluate.
- `cross_alias_match` (per-result): the row was taught under a different alias and matched the live query's canonical via `entity_lookups` (e.g., a "USA" teach satisfying a "United States" recall). Trust the resource_id.
- `similar_shape_different_entity:<canonical>` (top-level): a structurally matching row exists but its canonical entity differs from the live query's. Treated as cold start; the warning carries the conflicting canonical as a hint, but the row is NOT promoted into Results.
- `ambiguous_alias` (top-level): a single query entity resolved to multiple canonicals (e.g., "Cards" → Arizona Cardinals + St. Louis Cardinals). Surface the ambiguity from context before committing to a resource.
- `candidates_present` (top-level): the envelope carries a `candidates` section. Handle it via the candidates branch in Step 2 before anything else.
- `lookup_refresh_available` (top-level): an entity in the query has no lookup row yet, but synced data could provide one. Run `fritzbox-pp-cli sync` to refresh entity lookups.
- Top-level `no_learnings_for_query_family`: the table had no rows above the Jaccard floor. Pure cold start.

### Step 4: `teach &` after finalizing your response - always

Teaching is unconditional. After resolving a query the store could not answer, background-teach the final resource mapping - no call-count threshold, no judging whether it was "worth" learning. The teach is the anchor of the loop: it triggers playbook synthesis for a family without a playbook, and same-referent phrasings fold into one family so near-duplicate teaches do not fragment the store. Fire it after assembling your user-facing response but BEFORE emitting it, with a shell `&` so the call returns immediately:

```bash
fritzbox-pp-cli teach --query "<user's question>" --resource-type <type> --resource <id1> --resource <id2>
# (append shell `&` to background it)
```

Silent on success. Errors only land in `teach.log` under the resolved state dir. Teach the **most specific** resource - if the user asked a broad question and you walked through parent records to find the specific answer, teach the leaf id, not the parent. The CLI uses seeded `entity_lookups` for cross-alias resolution at recall time, so a teach under one alias (e.g., "Niners") satisfies future queries under another alias (e.g., "49ers", "San Francisco") automatically.

PII rule: teach the structural question with identifiers stripped - never include names, emails, phone numbers, account ids, or other personal identifiers in taught queries or notes. The CLI scans teach queries for obvious email/phone shapes and warns, but does not block; strip before teaching rather than relying on the warning.

### Step 5: playbooks - optional flags, automatic synthesis

You do not need to decide whether a session "deserves" a playbook: a teach on a family without one auto-synthesizes a `playbook_candidate` from the session's journal, and the next session judges it via confirm/reject. Attach explicit playbook flags only when you already hold choreography worth recording verbatim - workarounds the CLI didn't surface (silently-dropped flags, undocumented params, pagination tricks, payload gotchas). Prefer the **integrated one-call form** - record the resource learning and the playbook in the same `teach` invocation:

```bash
# Common case: record both the resource learning AND the playbook in one call.
fritzbox-pp-cli teach \
  --query "<user's question>" \
  --resource <id> \
  --playbook-file ~/playbooks/<shape>.json \
  --playbook-notes-file ~/playbooks/<shape>-notes.md
# (append shell `&` to background it)

# Alternate: playbook-only (no resource to record alongside).
fritzbox-pp-cli teach-playbook \
  --query "<user's question>" \
  --playbook-file ~/playbooks/<shape>.json \
  --notes-file ~/playbooks/<shape>-notes.md
```

Playbook files are JSON with `steps`, `entity_slots`, `expected_tool_calls`. Notes files are markdown carrying the gotchas verbatim. File-free callers (MCP-only agents) pass the same content inline: `--playbook-json` and `--playbook-notes` on the integrated `teach` form, `--playbook-json` and `--notes` on `teach-playbook`. On the integrated `teach` form, the playbook flags are optional - omit them entirely for a resource-only teach. On the standalone `teach-playbook` form, at least one of the playbook and notes flags must be set; both empty is rejected. Playbooks are keyed on the structural query family (entities stripped) so a recipe taught from one entity-shaped query applies to every other query of the same shape, with `slots_resolved` binding the live query's canonical at recall time.

When you DO find a playbook on a future recall, treat it as ground truth: replay the steps with `slots_resolved` substitutions, skip the discovery that the choreography already documents, and read `notes` before any step.

### Step 6: `playbook amend &` when your debug response identifies a correction

If your debug-protocol response identifies a concrete correction the notes or playbook should know — a workaround, an undocumented endpoint shape, a stale field name, observed schema drift, an empty-payload fallback — fire `playbook amend` BEFORE emitting your user-facing response. Same fire-and-forget posture as `teach`.

```bash
fritzbox-pp-cli playbook amend \
  --query "<exact recall query string>" \
  --add-note "<your concrete correction>"
# (append shell `&` to background it)
```

What counts as worth amending: a behavior you OBSERVED this session that future-you would benefit from knowing. Examples worth amending:

- A workaround for a CLI surface that silently drops or misorders a flag.
- An undocumented endpoint shape (response wrapped in `{meta, results}`, payload nested two levels deeper than the docs claim).
- Observed schema drift (a field renamed, an index that shifted between seasons, a category label that the API now returns lower-cased).

What does NOT belong in notes:

- The year-specific or entity-specific answer to the user's question. That's the response, not a learning.
- Per-team / per-athlete / per-row data the playbook already retrieves at runtime.
- Statements that paraphrase what the existing notes already say.

The amend command appends to the family's existing notes with a timestamped marker (`[amend YYYY-MM-DDTHH:MMZ]: <text>`). Multiple amends accumulate; the audit trail is visible. If no playbook exists yet for the family, amend creates a notes-only one (so cold-start corrections still land).

#### PII discipline for amend notes

`playbook amend` notes are designed to potentially flow upstream as shared knowledge in future versions of the Printing Press. Keep them clean of user-identifying content so the upstream-contribution path stays open without retroactive scrubbing:

- **Do NOT embed** paths to user filesystems, personal API keys or tokens, user email addresses, user GitHub handles, or specific query histories tied to a single user.
- **Acceptable**: endpoint shapes, undocumented field names, API gotchas, observed schema drift, workarounds for CLI surfaces, generalizable pagination or retry tactics.

If a correction is only meaningful with user-specific context, it belongs in a personal note, not in the playbook amend.

### Measuring the loop

`fritzbox-pp-cli learnings stats` reports recall hit rate, teach-to-reuse, playbook resolution rate, and candidate confirm/reject counts from the local `learn_events` table. Rates are null until they have a denominator; everything stays on this machine. Use it to check whether the loop is earning its keep for this CLI.

### Disabling learning

- `--no-learn` on a single command short-circuits both `recall` and the `teach` write path. Use for deterministic agent flows or tests that must not be affected by accumulated learnings.
- `FRITZBOX_NO_LEARN=true` in the environment globally disables the pipeline.

## Agent Feedback

When you (or the agent) notice something off about this CLI, record it:

```
fritzbox-pp-cli feedback "the --since flag is inclusive but docs say exclusive"
fritzbox-pp-cli feedback --stdin < notes.txt
fritzbox-pp-cli feedback list --json --limit 10
```

Entries are stored locally as `feedback.jsonl` under the resolved data dir. They are never POSTed unless `FRITZBOX_FEEDBACK_ENDPOINT` is set AND either `--send` is passed or `FRITZBOX_FEEDBACK_AUTO_SEND=true`. Default behavior is local-only.

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

A profile is a saved set of flag values, reused across invocations. Use it when a scheduled or recurring agent reuses the same saved flags while providing different input each run.

```
fritzbox-pp-cli profile save briefing --json
fritzbox-pp-cli --profile briefing dect
fritzbox-pp-cli profile list --json
fritzbox-pp-cli profile show briefing
fritzbox-pp-cli profile delete briefing --yes
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

1. **Empty, `help`, or `--help`** → show `fritzbox-pp-cli --help` output
2. **Starts with `install`** → ends with `mcp` → MCP installation; otherwise → see Prerequisites above
3. **Anything else** → Direct Use (execute as CLI command with `--agent`)

## MCP Server Installation

1. Install the MCP server:
   ```bash
   go install github.com/mvanhorn/printing-press-library/library/devices/fritzbox/cmd/fritzbox-pp-mcp@latest
   ```
2. Register with Claude Code:
   ```bash
   claude mcp add fritzbox-pp-mcp -- fritzbox-pp-mcp
   ```
3. Verify: `claude mcp list`

## Direct Use

1. Check if installed: `which fritzbox-pp-cli`
   If not found, offer to install (see Prerequisites at the top of this skill).
2. Match the user query to the best command from the Unique Capabilities and Command Reference above.
3. Execute with the `--agent` flag:
   ```bash
   fritzbox-pp-cli <command> [subcommand] [args] --agent
   ```
4. If ambiguous, drill into subcommand help: `fritzbox-pp-cli <command> --help`.
