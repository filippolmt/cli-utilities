# Coding standards

Read at review. Each rule is a judgement call; what tooling enforces (gofmt,
vet, golangci-lint, `scripts/check_patches.py`, the sweep) is not repeated
here.

## Patches to generated files

- **A patch's reprint check is a test that goes red without the patch.** A
  record in `.printing-press-patches/` names a `go test -run` as its reprint
  check, and that test fails when the patched call site reverts to what the
  generator emits. A test that passes either way checks nothing: look for
  assertions on the wrong stream (the store warns on `os.Stderr`) or on output
  the bug leaves intact.
- **The patch is a call; the logic is hand-written.** An edit to a generated
  file is the smallest change that reaches hand-written code: a call, a
  changed argument, a `Long` string. Lists, loops and helpers live in
  `<cli>_*.go` beside it. When the generator keeps state in a package-level
  map, an `init()` override in a hand-written file (as
  `fritzbox-pp-cli/internal/store/fritzbox_id_fields.go` and
  `subito-pp-cli/internal/cli/subito_export.go` do) beats editing the map.
- **The record is a reprint guard.** It says what the generator emits, what
  the CLI needs, why, how to tell the patch is gone, and what the upstream fix
  would be; it is not a changelog of the PR.

## Agent output

- **`--agent` keeps what identifies a row.** A list command's compact output
  carries the row's id, its human name and the value the command exists for
  (a listing's price, a filter's query-string key). The sweep catches rows cut
  to one field; the reviewer judges the rows that keep two useless ones.
