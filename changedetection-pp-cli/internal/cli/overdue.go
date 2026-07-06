// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command. Hand-authored body; regen preserves implemented scaffolds.

package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func newNovelOverdueCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "overdue",
		Short:       "List watches past their scheduled recheck time.",
		Long:        "List watches the server reports as overdue for a recheck (from /systeminfo overdue_watches), enriched with URL and title from the watch list.",
		Example:     "  changedetection-pp-cli overdue --agent",
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
			sysData, err := c.Get(ctx, "/systeminfo", nil)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			var sys struct {
				Overdue []string `json:"overdue_watches"`
			}
			if err := json.Unmarshal(sysData, &sys); err != nil {
				return fmt.Errorf("parsing /systeminfo response: %w", err)
			}
			// Enrich uuids with url/title from the watch list; a uuid missing
			// from the list still appears, just without metadata.
			byUUID := map[string]watchRow{}
			if watches, werr := fetchWatches(ctx, c); werr == nil {
				for _, w := range watches {
					byUUID[w.UUID] = w
				}
			}
			rows := make([]map[string]any, 0, len(sys.Overdue))
			for _, id := range sys.Overdue {
				row := map[string]any{"uuid": id}
				if w, ok := byUUID[id]; ok {
					row["url"] = w.URL
					row["title"] = w.Title
					row["last_checked"] = isoOrNever(w.LastChecked)
				}
				rows = append(rows, row)
			}
			if wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printAutoTable(cmd.OutOrStdout(), rows)
			}
			return printJSONFiltered(cmd.OutOrStdout(), rows, flags)
		},
	}
	return cmd
}
