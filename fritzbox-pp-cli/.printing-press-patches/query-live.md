# `query` reads live only

**File:** `internal/cli/promoted_query.go`

## What the generator emits

`resolveReadWithStrategyAndResponsePath(..., "auto", "query", ...)`: a live
read that writes its response through to a `query` resource and falls back to
the local store on a network error. `Long` repeats `Short`.

## What this CLI needs

The strategy `"live"`, and a `Long` help saying the command always reads
live, has no offline fallback and refuses `--data-source local`.

## Why

`query` runs an arbitrary data-path expression. Its answer has no row
identity, so the write-through printed `warning: 1/1 query items returned but
not cached locally (no extractable ID field …)` on most calls, and a cached
answer to one expression could never answer another offline.

## Reprint check

`go test ./internal/cli -run TestQueryAndWlanDoNotWarn`.

## Upstream

The generator could skip write-through for operations whose response schema
has no id field (subito's `ads-check-live.md` asks for the same).
