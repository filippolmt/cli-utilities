# `watch update` / `tag update` send only the flags the user set

**Files:**
- `internal/cli/watch_update.go`
- `internal/cli/tag_update.go`

## What the generator emits

```go
if bodyConditionsMatchLogic != "" {
	body["conditions_match_logic"] = bodyConditionsMatchLogic
}
...
if bodyFetchBackend != "" {
...
if bodyProcessor != "" {
```

with flag defaults `"ALL"`, `"system"` and `"text_json_diff"` from the spec.

## What this CLI needs

The same three checks as `if cmd.Flags().Changed("conditions-match-logic")`,
`Changed("fetch-backend")` and `Changed("processor")`, as the generator
already does for bool flags.

## Why

A non-empty default makes the `!= ""` check always true, so every update
PUT carried the three defaults. `watch update <uuid> --title x` reset a
watch's fetch backend (e.g. `html_requests` → `system`) and turned a
`restock_diff` watch into `text_json_diff`. On create the defaults match the
server's, so `watch create` / `tag create` are left as emitted.

## Reprint check

```bash
go test ./internal/cli -run TestUpdateSendsOnlyChangedFields
```

## Upstream

For update operations, the template should gate every body field on
`cmd.Flags().Changed(...)`, whatever its type or default.
