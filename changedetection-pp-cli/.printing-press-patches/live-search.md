# Live `search` matches substrings and prints one envelope

**File:** `internal/cli/search.go`

## What the generator emits

```go
data, getErr := c.Get(cmd.Context(), "/search", map[string]string{
	"q": query,
})
if getErr == nil {
	results := extractSearchResults(data, searchResponsePaths...)
```

and, at the end of `outputSearchResults`:

```go
wrapped, err := wrapWithProvenance(data, prov)
...
return printOutputWithFlags(cmd.OutOrStdout(), wrapped, &outputFlags)
```

## What this CLI needs

```go
data, getErr := c.Get(cmd.Context(), "/search", map[string]string{
	"q":       query,
	"partial": "true",
})
if getErr == nil {
	results := extractSearchResults(withPageTitles(cmd.Context(), c, flattenUUIDMap(data)), searchResponsePaths...)
```

and `return printOutput(cmd.OutOrStdout(), wrapped, true)`, the call
`watch list-watches` already uses after `wrapWithProvenance` (the
`outputFlags` copy goes with it). `flattenUUIDMap` and `withPageTitles` live
in the hand-authored `internal/cli/changedetection_watches.go`.

## Why

- `GET /search` matches `q` exactly unless `partial=true`: `search bob`
  returned nothing while the title "Bob Alchimia …" exists.
- The response is an object keyed by watch UUID, not an array, so
  `extractSearchResults` returned the whole map as a single row.
- It carries no `page_title`, so untitled watches came back with
  `title: null`; `withPageTitles` fills them from `/watch`, as the novel
  commands do.
- `wrapWithProvenance` already builds the `{meta, results}` envelope;
  `printOutputWithFlags` wraps it again under `--agent`, giving
  `{"meta":{"source":"local"},"results":{"meta":{"source":"live"},...}}`.
  This also hit local search.

## Reprint check

```bash
go test ./internal/cli -run TestLiveSearchFindsWatchesBySubstring
```

The test is hand-authored (`changedetection_watches_test.go`) and fails if
any of these changes is dropped.

## Upstream

Two generator issues, not specific to this API: `outputSearchResults` must
not pass an already-wrapped envelope to `printOutputWithFlags`, and
`extractSearchResults` should flatten an object of objects. The
`partial=true` default is this API's.
