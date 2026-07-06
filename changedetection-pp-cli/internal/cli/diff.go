// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command. Hand-authored body; regen preserves implemented scaffolds.

package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/spf13/cobra"
)

func newNovelDiffCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "diff <uuid>",
		Short:       "Print the unified text difference between a watch's two most recent snapshots.",
		Long:        "Resolve a watch's two most recent history snapshots and print the text difference between them, so you can read what actually changed without picking timestamps by hand.",
		Example:     "  changedetection-pp-cli diff 095be615-a8ad-4c33-8e9c-c7612fbf6c9f",
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
				return usageErr(fmt.Errorf("a watch uuid is required"))
			}
			uuid := args[0]
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			histData, err := c.Get(ctx, "/watch/"+uuid+"/history", nil)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			var hist map[string]json.RawMessage
			if err := json.Unmarshal(histData, &hist); err != nil {
				return fmt.Errorf("parsing history for %s: %w", uuid, err)
			}
			stamps := make([]int64, 0, len(hist))
			for k := range hist {
				if ts, convErr := strconv.ParseInt(k, 10, 64); convErr == nil {
					stamps = append(stamps, ts)
				}
			}
			if len(stamps) < 2 {
				return fmt.Errorf("watch %s has %d snapshot(s); need at least 2 to diff", uuid, len(stamps))
			}
			sort.Slice(stamps, func(i, j int) bool { return stamps[i] > stamps[j] })
			to := strconv.FormatInt(stamps[0], 10)
			from := strconv.FormatInt(stamps[1], 10)

			diffData, err := c.Get(ctx, "/watch/"+uuid+"/difference/"+from+"/"+to, map[string]string{"format": "text"})
			if err != nil {
				return classifyAPIError(err, flags)
			}
			// The difference endpoint returns text; the client wraps it as a JSON
			// value. Unquote when it is a JSON string, otherwise use the raw bytes.
			diffText := string(diffData)
			var s string
			if json.Unmarshal(diffData, &s) == nil {
				diffText = s
			}

			if flags.asJSON || (!isTerminal(cmd.OutOrStdout()) && !flags.csv && !flags.quiet && !flags.plain) {
				return printJSONFiltered(cmd.OutOrStdout(), map[string]any{
					"uuid": uuid,
					"from": isoOrNever(stamps[1]),
					"to":   isoOrNever(stamps[0]),
					"diff": diffText,
				}, flags)
			}
			fmt.Fprintln(cmd.OutOrStdout(), diffText)
			return nil
		},
	}
	return cmd
}
