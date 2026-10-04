# `.golangci.yml` carries the repo's lint policy

**File:** `.golangci.yml`

## What the generator emits

`linters.enable` only: no `exclusions`, no `settings`.

## What this CLI needs

Under `linters`:

- `exclusions.generated: lax`, so files with a `DO NOT EDIT` header are
  skipped (without the explicit key golangci-lint v2.14 linted them);
- `exclusions.paths`: `internal/cliutil/`, `internal/platform/`, the
  generator-reserved packages whose files carry no header;
- `settings.errcheck.exclude-functions`: `fmt.Fprint`, `fmt.Fprintf`,
  `fmt.Fprintln`; command output to a terminal or pipe has no recovery
  worth an error branch.

## Why

CI (`.github/workflows/ci.yml` at the repo root) lints every module. Without
this policy each generated tree reports 100+ findings in code a reprint
overwrites, which buries the findings in hand-written code.

## Reprint check

`grep -n 'generated: lax' .golangci.yml`. If it is gone, restore the block
above and re-run `golangci-lint run ./...`.

## Upstream

The emitted config could exclude its own generated files and reserved
packages.
