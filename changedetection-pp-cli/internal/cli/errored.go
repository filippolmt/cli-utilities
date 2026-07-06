// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command. Hand-authored body; regen preserves implemented scaffolds.

package cli

import (
	"github.com/spf13/cobra"
)

func newNovelErroredCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "errored",
		Short:       "List watches currently in an error/fetch-failed state.",
		Long:        "List every watch whose last_error is set (fetch failed, filter missing, etc.). Reads the live /watch list once and filters by last_error.",
		Example:     "  changedetection-pp-cli errored --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
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
			rows := make([]map[string]any, 0)
			for _, w := range watches {
				if msg := w.errorText(); msg != "" {
					rows = append(rows, map[string]any{
						"uuid":         w.UUID,
						"url":          w.URL,
						"title":        w.Title,
						"error":        msg,
						"last_checked": isoOrNever(w.LastChecked),
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
