# `find --partial` is a switch

**File:** `internal/cli/promoted_find.go`

## What the generator emits

```go
var flagPartial string
...
if flagPartial != "" {
	params["partial"] = formatCLIParamValue(flagPartial)
}
...
cmd.Flags().StringVar(&flagPartial, "partial", "", "Allow partial matching of URL query")
```

## What this CLI needs

```go
var flagPartial bool
...
if flagPartial {
	params["partial"] = "true"
}
...
cmd.Flags().BoolVar(&flagPartial, "partial", false, "Match --q as a substring of URLs and titles instead of exactly")
```

plus `Args: cobra.NoArgs` on the command.

## Why

The spec types `partial` as a string (see `tools-manifest.json`), but the
server treats it as a boolean. As a string flag, `find --q bob --partial --agent` took `--agent`
as the value and the server answered HTTP 500. `NoArgs` turns a detached
`--partial false` into an error instead of a silently ignored positional;
`--partial=false` still works. The MCP `find` tool calls the API directly
(`internal/mcp/tools.go`) and is unaffected.

## Reprint check

```bash
go test ./internal/cli -run TestFindPartialIsASwitch
```

## Upstream

The fix belongs in the spec: `partial` should be `type: boolean`, which the
generator already emits as a bool flag.
