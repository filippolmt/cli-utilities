# `go.mod` directive must not pin the generator's build toolchain

**File:** `go.mod`

## What the generator emits

```
go 1.26.5
```

…which is the Go version that built the `cli-printing-press` binary, not a
requirement of this CLI.

## What this CLI needs

```
go 1.27.0
```

## Why

The generator runs `govulncheck` as a post-generation quality gate, and
`govulncheck` resolves the standard-library version from this directive. Go
1.26.5 carries five standard-library advisories fixed in 1.26.6, so generation
exits 3 against its own gate despite having produced a complete, buildable
tree.

## Reprint check

`grep '^go ' go.mod`. If it names a version with open standard-library
advisories, raise it to the toolchain actually installed and re-run
`govulncheck ./...`.

## Upstream

The emitted directive should be the minimum version the generated code needs,
or the toolchain present at generation time — not whatever built the generator.
