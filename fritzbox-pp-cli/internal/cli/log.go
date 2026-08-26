// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import "github.com/spf13/cobra"

// newNovelLogCmd is the log command group. Its subcommands are attached by the
// hand-authored registration hook, which runs after this parent is wired into
// the root command.
func newNovelLogCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:         "log",
		Short:       "Router system log, live and retained",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}
}
