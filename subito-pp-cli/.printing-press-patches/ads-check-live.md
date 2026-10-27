# `ads check` reads live only

**File:** `internal/cli/ads_check.go`

## What the generator emits

`resolveReadWithStrategyAndResponsePath(..., "auto", "ads", ...)`: a live
read that writes its response through to the `ads` table and falls back to
the local store on a network error.

## What this CLI needs

The strategy `"live"`.

## Why

`/search/items/check` answers `{"newads": bool, "count": n}`, not listings.
The write-through tried to store it as an `ads` row and printed
`warning: 1/1 ads items returned but not cached locally (no extractable ID
field …)` on every call. A local fallback has nothing to answer with either:
"anything new since this listing" is only true live.

## Reprint check

`subito-pp-cli ads check --query soffione --last <urn> --agent` must print
nothing on stderr; `grep -n '"live", "ads"' internal/cli/ads_check.go`.

## Upstream

The generator could skip write-through for operations whose response schema
has no id field, as it already does for list envelopes with no items.
