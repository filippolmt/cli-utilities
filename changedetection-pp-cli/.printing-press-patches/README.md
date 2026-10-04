# Printing Press patches

This directory records deliberate edits to **generator-emitted** files in this
printed CLI, so a reprint carries the intent forward instead of silently
dropping it. See `../AGENTS.md`.

Only edits to generated files belong here. Hand-authored files added alongside
the generated tree — `internal/cli/changedetection_*.go`,
`internal/cli/<novel-feature>.go` — survive a reprint on their own and are
not patches.

Each entry states what changed, why the generator's own output was wrong for
this API, and how to tell whether the reprint still needs the patch.
