# The write-through cache skips non-entity responses

**File:** `internal/cli/data_source.go`

## What the generator emits

`writeThroughCache` tries to store every live read response.

## What this CLI needs

At the top of `writeThroughCache`:

```go
if nonEntityResources[resourceType] || isDryRunResponse(data) {
	return
}
```

`nonEntityResources` (`systeminfo`, `full-spec`, `history`, `favicon`) lives
in the hand-authored `internal/cli/changedetection_watches.go`.

## Why

These responses hold no entity rows: a status object, the OpenAPI document,
a `{timestamp: path}` history map, an image. Storing them always failed and
printed `warning: 1/1 … items returned but not cached locally (no
extractable ID field …)` on every call. The same happened with the
`{"dry_run": true}` sentinel that `--dry-run` returns. Caching history rows would need the
watch uuid, which `writeThroughCache` does not receive.

## Reprint check

```bash
go test ./internal/cli -run TestNonEntityReadsDoNotWarn
```

## Upstream

The generator could skip write-through for operations whose response schema
has no id field, instead of trying and warning.
