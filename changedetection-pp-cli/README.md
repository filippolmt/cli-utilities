# changedetection.io CLI

**Every changedetection.io watch, tag, and notification from one CLI — plus cross-watch queries the web UI can't do.**

Drive any self-hosted changedetection.io instance from the terminal or an agent: manage watches, tags, and notifications through the official API, and answer questions the UI never surfaces in one call — `since` rolls up what changed recently, `stale` finds dead monitors, `errored`/`overdue` triage broken or behind-schedule ones, `diff` reads the latest text change directly, and `watch-search` filters locally.

Built for your own instance. Nothing is hardcoded to a specific host — you point it at your instance with a base URL and (optionally) an API key.

## Build

```bash
go build -o changedetection-pp-cli ./cmd/changedetection-pp-cli
```

That produces the `changedetection-pp-cli` binary in this folder. Run it as `./changedetection-pp-cli`.

## Credentials — kept inside this folder

The simplest setup: put a `config.toml` **in this CLI folder** and point the CLI at it. Nothing lands in your home directory.

1. Copy the example and fill it in:

   ```bash
   cp config.toml.example config.toml
   ```

   `config.toml`:

   ```toml
   base_url     = "https://your-host/api/v1"
   api_key      = "your-key-from-Settings-API"   # omit if your instance needs no auth
   insecure_tls = false                           # set true only for self-signed certs (like curl -k)
   ```

2. Tell the CLI to use it. Either per command:

   ```bash
   ./changedetection-pp-cli --config ./config.toml doctor
   ```

   or once per shell (recommended):

   ```bash
   export CHANGEDETECTION_CONFIG="$PWD/config.toml"
   ./changedetection-pp-cli doctor
   ```

**Config resolution order:** `--config <path>` → `CHANGEDETECTION_CONFIG` env → platform default (`~/.config/changedetection-pp-cli/config.toml`). The in-folder `config.toml` wins whenever you pass `--config` or set `CHANGEDETECTION_CONFIG`.

### Keep it out of git

`config.toml` holds a secret. It is already listed in `.gitignore`. Lock down permissions too:

```bash
chmod 600 config.toml
```

Commit `config.toml.example` (placeholders only), never `config.toml`.

### Alternative: environment variables

If you prefer not to store the key on disk, set it per session — no config file needed:

```bash
export CHANGEDETECTION_BASE_URL="https://your-host/api/v1"
export CHANGEDETECTION_API_KEY="your-key"    # optional
export CHANGEDETECTION_INSECURE=1            # optional, self-signed certs
./changedetection-pp-cli doctor
```

Env vars override the config file, so you can keep `base_url`/`insecure_tls` in `config.toml` and inject only the key at runtime.

### Keep *all* state in the folder (optional)

`config.toml` covers credentials. If you also want the local cache/DB/state to live here instead of your home directory, relocate the whole root:

```bash
export CHANGEDETECTION_HOME="$PWD/.cd-home"
```

## Authentication

changedetection.io uses an `x-api-key` header. Copy your key from the dashboard under **Settings > API**. Set it as `api_key` in `config.toml` or as `CHANGEDETECTION_API_KEY`. If your instance allows anonymous API access, you can skip the key entirely.

Self-signed TLS: if `curl` needs `-k` to reach your instance, set `insecure_tls = true` (or `CHANGEDETECTION_INSECURE=1`).

## Quick Start

```bash
export CHANGEDETECTION_CONFIG="$PWD/config.toml"

./changedetection-pp-cli doctor              # verify config, auth, connectivity
./changedetection-pp-cli watch list --agent  # all watches as JSON
./changedetection-pp-cli since 24h --agent   # what changed in the last day
./changedetection-pp-cli overdue --agent     # watches behind their recheck schedule
```

## Unique commands

Not available in the web UI or raw API — each answers a cross-watch question in one call.

| Command | What it does |
|---------|--------------|
| `since <duration>` | Watches changed within a window (`24h`, `7d`, `2w`). |
| `stale [--days N]` | Watches not changed in N days (default 30); never-changed count as stale. |
| `errored` | Watches currently in an error/fetch-failed state. |
| `overdue` | Watches past their scheduled recheck time (from `/systeminfo`). |
| `diff <uuid>` | Unified text diff of a watch's two most recent snapshots. |
| `watch-search <query> [--regex]` | Filter watches by text across URL, title, and error message. |

## Core commands (from the official API)

- `watch list` · `watch get <uuid>` · `watch create` · `watch update <uuid>` · `watch delete <uuid>`
- `watch history <uuid>` · `watch difference <uuid> <from> <to>` · `watch favicon <uuid>`
- `tags` · `tag create` · `tag get <uuid>` · `tag update <uuid>` · `tag delete <uuid>`
- `notifications get` · `notifications add` · `notifications replace` · `notifications delete`
- `find <query>` (server-side watch search) · `bulk-import` · `systeminfo` · `full-spec`

Run `./changedetection-pp-cli --help` for the complete tree, plus framework commands (`sync`, `search`, `analytics`, `export`, `doctor`, `agent-context`, `which`).

## Output formats

```bash
./changedetection-pp-cli watch list                       # table (terminal) / JSON (piped)
./changedetection-pp-cli watch list --json                # JSON
./changedetection-pp-cli watch list --json --select url,last_changed,last_error
./changedetection-pp-cli watch create --dry-run           # show request without sending
./changedetection-pp-cli watch list --agent               # JSON + compact + non-interactive
```

## Agent usage

Non-interactive, pipeable, filterable. Add `--agent` for JSON + compact + no prompts.

Exit codes: `0` success · `2` usage error · `3` not found · `4` auth error · `5` API error · `7` rate limited · `10` config error.

## Troubleshooting

- **`api: unreachable` / connection timeout** — check `base_url` points at a host you can actually reach; the API root is `<host>/api/v1` (changedetection default port is `5000`).
- **`tls: failed to verify certificate`** — self-signed cert; set `insecure_tls = true` or `CHANGEDETECTION_INSECURE=1`.
- **401 / 403 on every command** — set `api_key` (Settings > API) in `config.toml` or `CHANGEDETECTION_API_KEY`.
- **empty results on `since`/`stale`/`errored`/`overdue`/`watch-search`** — these read the live watch list; confirm `doctor` reports `api: reachable` and auth is configured.

Learn more about the server at [changedetection.io](https://github.com/dgtlmoon/changedetection.io).
