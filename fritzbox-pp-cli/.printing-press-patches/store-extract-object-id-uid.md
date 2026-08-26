# `extractObjectID` must recognise `UID`

**File:** `internal/store/store.go`

## What the generator emits

```go
for _, key := range []string{"id", "Id", "ID", "uuid", "slug", "name"} {
	if v, ok := obj[key]; ok {
		return ResourceIDString(v)
	}
}
```

## What this CLI needs

`UID` and `uid` inserted before `slug`/`name`, and a present-but-empty value
skipped rather than returned.

## Why

FRITZ!OS identifies a device by its `UID` and lets several devices share a
display name. With the emitted list the lookup falls through to `name`, so
every duplicate-named device collapses onto one row — and it does so **with no
error on any path**. On the machine this CLI was printed against, two devices
called "Mac", two called "Samsung" and three called "iPhone" turned 31 devices
into 27 rows in the local mirror.

The empty-value skip matters because `query.lua` answers a request for a field
it does not know with an empty string rather than omitting the key. Returning
the first *present* key would therefore yield an empty id and fail the upsert.

## Reprint check

Run a sync against a router with two devices sharing a name, then compare
`SELECT COUNT(*) FROM hosts` against `fritzbox-pp-cli hosts list --count`.
If the counts differ, this patch was dropped.

The sibling extractor used by the batch path, `ExtractResourceID`, needs no
patch: it consults `resourceIDFieldOverrides`, which this CLI populates from
the hand-authored `internal/store/fritzbox_id_fields.go`. Prefer that mechanism
if the generator ever routes both paths through the override map — the patch
here can then be dropped.

## Upstream

The generator's `genericIDFieldFallbacks` does list `uid`, but the lookup is
case-sensitive, so `UID` misses; `extractObjectID` has no `UID` entry at all.
Silent row collapse with no error is the wrong failure shape for a local
mirror. Worth fixing upstream in both extractors.
