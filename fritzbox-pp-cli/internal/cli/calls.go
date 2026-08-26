// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import "github.com/spf13/cobra"

// newNovelCallsCmd is the calls command group. Its subcommands are attached by the
// hand-authored registration hook, which runs after this parent is wired into
// the root command.
func newNovelCallsCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:         "calls",
		Short:       "Call journal, digests, deflections",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}
}
