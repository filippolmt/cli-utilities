# GETs that change state skip the cache and are not read-only

**Files:**
- `internal/cli/watch_get.go`
- `internal/cli/tag_get.go`
- `internal/cli/watch_list-watches.go`
- `internal/mcp/tools.go`

## What the generator emits

Every GET is a read: `"mcp:read-only": "true"` in the command annotations,
`mcplib.WithReadOnlyHintAnnotation(true)` on the MCP tool, and the request
goes through the 5-minute response cache.

## What this CLI needs

For `watch get`, `tag get` and `watch list-watches`:

- no `"mcp:read-only"` annotation;
- right after `c, err := flags.newClient()` and its error check:

  ```go
  // --recheck/--paused/--muted change the watch: never answer from cache.
  if cmd.Flags().Changed("recheck") || cmd.Flags().Changed("paused") || cmd.Flags().Changed("muted") {
  	c.NoCache = true
  }
  ```

  (`muted`/`recheck` for `tag get`, `recheck-all` for `watch list-watches`);
- `mcplib.WithReadOnlyHintAnnotation(false)` on the `watch_get`, `tag_get`
  and `watch_list-watches` tools. The MCP client already sets `NoCache`.

## Why

changedetection changes state through GET query parameters: `?paused=`,
`?muted=`, `?recheck=` on a watch or tag, `?recheck_all=1` on the list.

- From cache, `watch get X --paused paused`, then `unpaused`, then `paused`
  within five minutes never sent the third request: the watch stayed
  unpaused while the CLI reported success.
- With `readOnlyHint: true`, an MCP client may auto-approve a call that
  pauses or mutes a watch.

## Reprint check

```bash
go test ./internal/cli -run 'TestStateChangingGets'
go test ./internal/mcp -run TestStateChangingToolsAreNotReadOnly
```

## Upstream

The generator infers read-only from the HTTP method. It needs a spec
annotation (or a heuristic on state-setting query parameters) to mark a GET
as mutating, and then drop the read-only hint and the response cache.
