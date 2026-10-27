# `export ads` pages `/search/items` with the sync defaults

**File:** `internal/cli/export.go` (path, paging and helpers live in the
hand-written `internal/cli/subito_export.go`)

## What the generator emits

- `resourceReadPaths["ads"] = "/search/items/detail-recommended"` and an
  `ads` read config without pagination;
- an `export` page loop that sends only paging params.

## What this CLI needs

- `subito_export.go` overrides, in `init()`, the `ads` path to
  `/search/items` and its config to offset paging over `start`/`lim`, 100
  per page. The generated maps stay untouched.
- In `export.go`, two calls: `limit = exportLimit(resource, limit,
  cmd.ErrOrStderr())` before the page loop, and `params =
  exportListParams(resource, params)` after `resourcePageParams`.
  `exportListParams` adds `syncResourceParamDefaults` (`t=s`,
  `sort=datedesc`); `exportLimit` caps `export ads` without `--limit` at 100
  listings and prints a `hint:` saying so.

## Why

`detail-recommended` needs one listing's `urn`, and `/search/items` needs
at least `t`: without them Subito answers HTTP 400 `No query params
specified`, so `export ads` always failed. Without paging it stopped at the
first page whatever `--limit` said. Without a cap, an export with no
`--limit` would page through every listing on Subito.

## Reprint check

`go test ./internal/cli -run TestExportAds`.

## Upstream

The generator took the list path of `ads` from a sibling endpoint that
requires a parameter, and `export` ignores the spec defaults and paging
that `sync` already applies. Both could share the sync path map, defaults
and pagination.
