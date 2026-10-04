# Printing Press patches

This directory records deliberate edits to **generator-emitted** files in this
printed CLI, so a reprint carries the intent forward instead of silently
dropping it. See `../AGENTS.md`.

Only edits to generated files belong here. Hand-authored files added alongside
the generated tree — `internal/subito/*`, `internal/cli/subito_*.go`,
`internal/store/subito_store.go`, `internal/cli/watch.go`,
`internal/cli/ads_get.go`, `internal/cli/market_stats.go` and the filled-in
novel scaffolds (`market_deals.go`, `market_suggest.go`, `ads_risk.go`,
`ads_history.go`, `market.go`) — survive a reprint on their own and are not
patches. The subito tables in `internal/store/extras.go` live in the file the
generator reserves for novel-feature migrations.

Each entry states what changed, why the generator's own output was wrong for
this API, and how to tell whether the reprint still needs the patch.
