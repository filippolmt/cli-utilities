# Issue tracker

GitHub Issues on `filippolmt/cli-utilities`, read with `gh` from the repo root.

- One issue: `gh issue view <n> --comments`.
- A reference in a commit or PR (`#123`, `Closes #45`) is an issue or a PR on
  the same repo; `gh pr view <n>` when `gh issue view` says it is a PR.
- Issues are titled `<cli>: <what>` (`subito: sharper market judgments`);
  `gh issue list --search "<cli> in:title"` finds a CLI's open ones.

Many changes have no issue: the spec is then the PR description, or the
conversation that asked for the change.
