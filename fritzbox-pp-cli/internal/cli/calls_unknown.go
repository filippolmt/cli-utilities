// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

// pp:data-source live — anti-joins the live call journal against the live phonebook.
// pp:client-call — the API is reached through the fbBox wrapper in
// fritzbox_core.go rather than through flags.newClient(), so the static
// check cannot see the call site.

package cli

import (
	"sort"

	"github.com/spf13/cobra"
)

func newNovelCallsUnknownCmd(flags *rootFlags) *cobra.Command {
	var days, minCalls int

	cmd := &cobra.Command{
		Use:   "unknown",
		Short: "List inbound numbers that are not in the phonebook, ranked by frequency",
		Long: `Find the numbers that keep calling and are not saved anywhere.

This is an anti-join between the call journal and the phonebook, both of which
the router only exposes as separate downloads, so it is computed locally.

Use this command for repeat callers you have not saved. Do NOT use it for a
general call rollup; use 'calls digest' instead.`,
		Example:     "  fritzbox-pp-cli calls unknown --days 30 --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "find callers that are not in the phonebook", map[string]any{"days": days})
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			calls, err := box.Calls(ctx, days)
			if err != nil {
				return err
			}
			contacts, err := box.Phonebook(ctx)
			if err != nil {
				// Without the phonebook every caller would look unknown, which
				// would be a confidently wrong answer rather than a partial one.
				return err
			}
			known := map[string]bool{}
			for _, c := range contacts {
				if d := onlyDigits(c.Number); d != "" {
					known[d] = true
				}
			}

			type entry struct {
				Number string
				Count  int
				Missed int
				Last   string
			}
			grouped := map[string]*entry{}
			for _, c := range calls {
				if c.Kind == "outbound" || c.Kind == "active-outbound" {
					continue
				}
				digits := onlyDigits(c.Number)
				if digits == "" || known[digits] {
					continue
				}
				e, ok := grouped[digits]
				if !ok {
					e = &entry{Number: c.Number}
					grouped[digits] = e
				}
				e.Count++
				if c.Kind == "missed" {
					e.Missed++
				}
				if !c.At.IsZero() {
					if stamp := c.At.Format("2006-01-02 15:04"); stamp > e.Last {
						e.Last = stamp
					}
				}
			}

			entries := make([]*entry, 0, len(grouped))
			for _, e := range grouped {
				if e.Count >= minCalls {
					entries = append(entries, e)
				}
			}
			sort.Slice(entries, func(i, j int) bool {
				if entries[i].Count != entries[j].Count {
					return entries[i].Count > entries[j].Count
				}
				return entries[i].Last > entries[j].Last
			})
			rows := make([]map[string]any, 0, len(entries))
			for _, e := range entries {
				rows = append(rows, map[string]any{
					"number": e.Number, "calls": e.Count, "missed": e.Missed, "last_call": e.Last,
				})
			}
			return fbEmit(cmd, flags, rows, "Every recent caller is already in the phonebook.")
		},
	}
	cmd.Flags().IntVar(&days, "days", 30, "How many days back to examine")
	cmd.Flags().IntVar(&minCalls, "min-calls", 1, "Only report numbers that called at least this many times")
	return cmd
}
