// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.
// Novel command. Hand-authored body; regen preserves implemented scaffolds.

package cli

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

func newNovelWatchSearchCmd(flags *rootFlags) *cobra.Command {
	var useRegex bool

	cmd := &cobra.Command{
		Use:   "watch-search <query>",
		Short: "Filter watches by text across URL, title, and error message.",
		Long: "Filter the full watch list locally by substring (default) or regular expression (--regex), " +
			"matching against URL, title, link, and last_error. Richer than the server-side 'find' command, " +
			"which only matches URL and title.",
		Example:     "  changedetection-pp-cli watch-search pricing --agent",
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
				return usageErr(fmt.Errorf("a search query is required"))
			}
			query := args[0]

			var re *regexp.Regexp
			if useRegex {
				var err error
				re, err = regexp.Compile("(?i)" + query)
				if err != nil {
					return usageErr(fmt.Errorf("invalid --regex pattern %q: %w", query, err))
				}
			}
			needle := strings.ToLower(query)
			match := func(fields ...string) bool {
				for _, f := range fields {
					if re != nil {
						if re.MatchString(f) {
							return true
						}
					} else if strings.Contains(strings.ToLower(f), needle) {
						return true
					}
				}
				return false
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
				if match(w.URL, w.Link, w.Title, w.errorText()) {
					rows = append(rows, map[string]any{
						"uuid":         w.UUID,
						"url":          w.URL,
						"title":        w.Title,
						"last_changed": isoOrNever(w.LastChanged),
						"last_error":   w.errorText(),
					})
				}
			}
			if wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printAutoTable(cmd.OutOrStdout(), rows)
			}
			return printJSONFiltered(cmd.OutOrStdout(), rows, flags)
		},
	}
	cmd.Flags().BoolVar(&useRegex, "regex", false, "Treat the query as a case-insensitive regular expression")
	return cmd
}
