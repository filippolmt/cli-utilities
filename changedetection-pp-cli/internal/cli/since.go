// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command. Hand-authored body; regen preserves implemented scaffolds.

package cli

import (
	"fmt"
	"time"

	"changedetection-pp-cli/internal/cliutil"
	"github.com/spf13/cobra"
)

func newNovelSinceCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "since <duration>",
		Short:       "Show every watch that changed in the last N hours or days in one call.",
		Long:        "Show every watch whose content changed within the given window (e.g. 24h, 7d, 2w). Reads the live /watch list once and filters by last_changed.",
		Example:     "  changedetection-pp-cli since 24h --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			if len(args) == 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("a duration is required, e.g. 'since 24h'"))
			}
			dur, err := cliutil.ParseDurationLoose(args[0])
			if err != nil {
				return usageErr(fmt.Errorf("invalid duration %q: %w", args[0], err))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			watches, err := fetchWatches(ctx, c)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			cutoff := time.Now().Add(-dur).Unix()
			rows := make([]map[string]any, 0)
			for _, w := range watches {
				if w.LastChanged > 0 && w.LastChanged >= cutoff {
					rows = append(rows, map[string]any{
						"uuid":         w.UUID,
						"url":          w.URL,
						"title":        w.Title,
						"last_changed": isoOrNever(w.LastChanged),
					})
				}
			}
			if wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printAutoTable(cmd.OutOrStdout(), rows)
			}
			return printJSONFiltered(cmd.OutOrStdout(), rows, flags)
		},
	}
	return cmd
}
