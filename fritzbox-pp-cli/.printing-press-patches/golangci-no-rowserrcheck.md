# `.golangci.yml` must not enable `rowserrcheck`

**File:** `.golangci.yml`

## What the generator emits

`rowserrcheck` in `linters.enable`.

## What this CLI needs

The `rowserrcheck` entry removed.

## Why

On `internal/cli` the analyzer recurses without end (`fatal error: stack
overflow` in `rowserr` / `slices.ContainsFunc`), with golangci-lint v2.13.2 and
v2.14.0 alike, so every lint run dies before reporting anything. `go vet`
passes, and no other enabled linter triggers it.

## Reprint check

`golangci-lint run ./...` in this directory. If it ends in `stack overflow`
with `rowserr` frames, remove `rowserrcheck` again. Once an upstream
rowserrcheck release no longer overflows on this package, drop this patch.

## Upstream

Not yet reported to golangci/rowserrcheck; no matching issue found.
