// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command. Hand-authored body; regen preserves implemented scaffolds.

package cli

import (
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

func newNovelStaleCmd(flags *rootFlags) *cobra.Command {
	var flagDays string

	cmd := &cobra.Command{
		Use:         "stale",
		Short:       "List watches that have not changed (or not been checked) in N days.",
		Long:        "List watches whose last change is older than --days days (never-changed watches count as stale). Reads the live /watch list once and filters by last_changed.",
		Example:     "  changedetection-pp-cli stale --days 30 --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			daysStr := flagDays
			if daysStr == "" {
				daysStr = "30"
			}
			days, err := strconv.Atoi(daysStr)
			if err != nil || days < 0 {
				return usageErr(fmt.Errorf("--days must be a non-negative integer, got %q", flagDays))
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
			cutoff := time.Now().AddDate(0, 0, -days).Unix()
			rows := make([]map[string]any, 0)
			for _, w := range watches {
				if w.LastChanged < cutoff {
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
	cmd.Flags().StringVar(&flagDays, "days", "", "Age threshold in days; watches not changed in this many days are stale (default 30)")
	return cmd
}
