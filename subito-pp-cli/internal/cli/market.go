// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command scaffold. Implement the RunE body before shipping.
// generate --force preserves implemented bodies; untouched TODO scaffolds may refresh.
// pp:data-source auto
// Supported strategies: auto, local, live, or computed. Change this default deliberately.

package cli

import (
	"github.com/spf13/cobra"
)

func newNovelMarketCmd(flags *rootFlags) *cobra.Command {

	cmd := &cobra.Command{
		Use:         "market",
		Short:       "Work with market",
		Example:     "  subito-pp-cli market deals 'iphone 15 128' --category-id 12 --shippable --agent",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "auto", "pp:typed-exit-codes": "0,2"},
		RunE:        parentNoSubcommandRunE(flags),
	}
	addNovelCommandIfAbsent(cmd, newNovelMarketDealsCmd(flags))
	addNovelCommandIfAbsent(cmd, newNovelMarketSuggestCmd(flags))
	return cmd
}
