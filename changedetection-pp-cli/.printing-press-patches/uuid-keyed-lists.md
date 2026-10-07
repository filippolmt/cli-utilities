# `sync` and the write-through cache read uuid-keyed lists

**Files:**
- `internal/cli/sync.go`
- `internal/cli/data_source.go`

## What the generator emits

```go
items, nextCursor, hasMore := extractPageItems(data, pageSize.cursorParam, responsePathForResource(resource, path)...)
```

in `sync.go`, and `writeThroughCache` in `data_source.go` decoding `data` as
it comes.

## What this CLI needs

```go
items, nextCursor, hasMore := extractPageItems(flattenUUIDMap(data), pageSize.cursorParam, responsePathForResource(resource, path)...)
```

and, in `writeThroughCache` right after `defer db.Close()`:

```go
// changedetection lists are objects keyed by uuid; cache one row each.
data = flattenUUIDMap(data)
```

`flattenUUIDMap` lives in the hand-authored
`internal/cli/changedetection_watches.go`; it flattens only objects whose
keys are all UUIDs and whose values are all objects, so detail responses
pass through unchanged.

## Why

`GET /watch` and `GET /tags` return `{"<uuid>": {...}, ...}`, not an array.
Without flattening:

- `sync` failed on watches with `missing id for watch` and stored all tags
  as one row, so the local store and `analytics` stayed empty;
- every live list read (`watch list-watches`, `tags`, `find`) printed
  `warning: 1/1 … items returned but not cached locally (no extractable ID
  field …)` and cached nothing.

## Reprint check

```bash
go test ./internal/cli -run 'TestSyncStoresEveryWatchAndTag|TestLiveListsFeedTheLocalStore'
```

Both tests are hand-authored (`changedetection_watches_test.go`) and fail if
either change is dropped.

## Upstream

`extractPageItems` and `writeThroughCache` should treat an object whose
values are all objects keyed by an id-shaped string as a list, injecting
the key as the id.
