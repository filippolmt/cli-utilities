# `export ads` reads `/search/items` with the sync defaults

**Files:** `internal/cli/resource_paths.go`, `internal/cli/export.go`

## What the generator emits

- `resourceReadPaths["ads"] = "/search/items/detail-recommended"`;
- `export` sends only paging params on a list read.

## What this CLI needs

- `"/search/items"`, the path `sync` already uses for `ads`;
- in the `export` page loop, after `resourcePageParams`, the params of
  `syncResourceParamDefaults(resource)` (`t=s`, `sort=datedesc`) for any key
  not already set.

## Why

`detail-recommended` needs the `urn` of one listing, and `/search/items`
needs at least `t`: without them Subito answers HTTP 400 `No query params
specified`, so `export ads` always failed. `sync` already seeds the same
defaults, so both commands now read the same slice (newest listings for sale).

## Reprint check

`subito-pp-cli export ads --format jsonl --limit 1` must exit 0.

## Upstream

The generator picked the list path of `ads` from a sibling endpoint that
requires a parameter, and `export` ignores the spec defaults that `sync`
already applies. Both could share the sync path map and defaults.
