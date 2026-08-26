// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"github.com/spf13/cobra"
)

// newNovelActionsCmd is the TR-064 action-catalog command group. Both
// subcommands read the catalog this firmware actually publishes rather than a
// vendor list, which is the only way to tell what a given box can do.
func newNovelActionsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "actions",
		Short:       "TR-064 action catalog: search this firmware's actions and diff them across updates",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newNovelActionsDiffCmd(flags))
	cmd.AddCommand(newNovelActionsSearchCmd(flags))
	return cmd
}
