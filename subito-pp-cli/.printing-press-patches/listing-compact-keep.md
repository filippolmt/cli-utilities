# `--agent` on listing and filter commands keeps the useful fields

**Files:** `internal/cli/ads_search.go`, `internal/cli/ads_recommended.go`,
`internal/cli/categories_filters.go` (the lists `adCompactKeep` and
`filterCompactKeep` live in the hand-written `internal/cli/subito_compact.go`)

## What the generator emits

`compactFields(filtered, map[string]bool{"type": true})`: schema-aware
compaction that keeps only gravity names (`id`, `name`, `price`, ...). A
Subito listing and a search filter both have `type` as their only gravity
name.

## What this CLI needs

`compactFieldsKeep(filtered, adCompactKeep, ...)` on the two listing commands,
so `urn`, `subject`, `features` (price, condition), `geo`, `dates` and `urls`
survive `--agent`/`--compact`; `compactFieldsKeep(filtered, filterCompactKeep,
...)` on `categories filters`, so `uri`, `label`, `query_string` and
`datasource` survive.

## Why

Without it these commands return every row as `{"type": ...}`: a listing
without title, price or link, a filter without its query-string key.

## Reprint check

`go test ./internal/cli -run TestAgentKeepsListingFields` fails for each
command whose call site is gone.

## Upstream

Schema-aware compaction could fall back to the frequency rule when the
documented item schema has no gravity field besides a categorization one
(`type`, `kind`), or the spec could name the fields to keep.
