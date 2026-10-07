# Text-returning endpoints must skip the live JSON guard

**Files:**
- `internal/cli/watch_history_get-watch-snapshot.go`
- `internal/cli/watch_difference_get-watch-history-diff.go`

## What the generator emits

```go
data, prov, err := resolveReadWithStrategyAndResponsePath(cmd.Context(), c, flags, "auto", "history", false, path, params, nil, "", cmd.ErrOrStderr())
if err != nil {
	return classifyAPIError(err, flags)
}
```

`resolveReadWithStrategyAndResponsePath` always enables `assertLiveJSONBody`.

## What this CLI needs

```go
data, prov, err := resolveReadWithStrategyResponsePathAndJSONGuard(cmd.Context(), c, flags, "auto", "history", false, path, params, nil, "", false, cmd.ErrOrStderr())
if err != nil {
	return classifyAPIError(err, flags)
}
if handled, err := printTextBody(cmd, flags, data, prov); handled {
	return err
}
```

(`"difference"` instead of `"history"` in the second file.) `printTextBody`
lives in the hand-authored `internal/cli/changedetection_text.go`: raw text on
a terminal, the text as a JSON string in the provenance envelope otherwise.

## Why

The spec declares `text/plain` for `GET /watch/{uuid}/history/{timestamp}`
and text/HTML for `GET /watch/{uuid}/difference/{from}/{to}`. With the guard
on, both commands fail every time with
`API returned a non-JSON response; expected JSON` — they cannot return a
snapshot or a diff at all.

## Reprint check

```bash
go test ./internal/cli -run TestTextEndpointsReturnTheBody
```

The test is hand-authored (`changedetection_watches_test.go`) and fails with
the non-JSON error if this patch was dropped.

## Upstream

The generator should disable the live JSON guard (and route output through a
text path) for any operation whose 2xx response has no `application/json`
content. `resolveReadWithStrategyResponsePathAndJSONGuard` already exists for
this; only the template's choice of caller is wrong.
