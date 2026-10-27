# `--agent` on listing commands keeps the listing fields

**Files:** `internal/cli/ads_search.go`, `internal/cli/ads_recommended.go`
(the list `adCompactKeep` lives in the hand-written
`internal/cli/subito_compact.go`)

## What the generator emits

`compactFields(filtered, map[string]bool{"type": true})`: schema-aware
compaction that keeps only gravity names (`id`, `name`, `price`, ...). A
Subito listing's only gravity name is `type`.

## What this CLI needs

`compactFieldsKeep(filtered, adCompactKeep, map[string]bool{"type": true})`,
so `urn`, `subject`, `features` (price, condition), `geo`, `dates` and `urls`
survive `--agent`/`--compact`.

## Why

Without it `ads search --agent` returns every listing as `{"type": ...}`:
no title, no price, no link.

## Reprint check

`grep -n adCompactKeep internal/cli/ads_search.go internal/cli/ads_recommended.go`
must show both call sites; `go test ./internal/cli -run TestAdsSearchAgentKeepsListingFields`
fails when one is gone.

## Upstream

Schema-aware compaction could fall back to the frequency rule when the
documented item schema has no gravity field besides a categorization one
(`type`, `kind`), or the spec could name the fields to keep.
