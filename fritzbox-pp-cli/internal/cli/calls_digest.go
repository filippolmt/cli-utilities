// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

// pp:data-source live — fetches the call journal and phonebook, then joins them locally.
// pp:client-call — the API is reached through the fbBox wrapper in
// fritzbox_core.go rather than through flags.newClient(), so the static
// check cannot see the call site.

package cli

import (
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// callerKey groups a caller so that formatting differences in the same number
// do not split one person across several rows.
func callerKey(number, name string) string {
	if digits := onlyDigits(number); digits != "" {
		return digits
	}
	if name != "" {
		return "name:" + strings.ToLower(name)
	}
	return "unknown"
}

type callerSummary struct {
	Name     string   `json:"name"`
	Number   string   `json:"number"`
	Total    int      `json:"total"`
	Missed   int      `json:"missed"`
	Inbound  int      `json:"inbound"`
	Outbound int      `json:"outbound"`
	Last     string   `json:"last_call"`
	InBook   bool     `json:"in_phonebook"`
	Kinds    []string `json:"-"`
}

func newNovelCallsDigestCmd(flags *rootFlags) *cobra.Command {
	var days int
	var missedOnly bool

	cmd := &cobra.Command{
		Use:   "digest",
		Short: "Roll up recent calls by caller with names resolved from the phonebook",
		Long: `Group the recent call journal by caller and resolve each number against the
phonebook stored on the router.

The router returns calls and contacts as two separate downloads with no shared
key, so the correlation happens locally.

Use this command for a rollup of who called. Do NOT use it for individual call
records; use 'calls list' instead.`,
		Example:     "  fritzbox-pp-cli calls digest --days 7 --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "summarise the call journal by caller", map[string]any{"days": days})
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
			// A phonebook that cannot be read degrades the digest to numbers
			// without names, which is still useful, so it is not fatal.
			byNumber := map[string]string{}
			if contacts, err := box.Phonebook(ctx); err == nil {
				for _, c := range contacts {
					if d := onlyDigits(c.Number); d != "" {
						byNumber[d] = c.Name
					}
				}
			}

			grouped := map[string]*callerSummary{}
			for _, c := range calls {
				if missedOnly && c.Kind != "missed" {
					continue
				}
				key := callerKey(c.Number, c.Name)
				s, ok := grouped[key]
				if !ok {
					s = &callerSummary{Number: c.Number, Name: c.Name}
					if resolved, found := byNumber[onlyDigits(c.Number)]; found {
						s.Name, s.InBook = resolved, true
					}
					grouped[key] = s
				}
				s.Total++
				switch c.Kind {
				case "missed":
					s.Missed++
				case "outbound", "active-outbound":
					s.Outbound++
				default:
					s.Inbound++
				}
				if !c.At.IsZero() {
					stamp := c.At.Format("2006-01-02 15:04")
					if stamp > s.Last {
						s.Last = stamp
					}
				}
			}

			rows := make([]map[string]any, 0, len(grouped))
			summaries := make([]*callerSummary, 0, len(grouped))
			for _, s := range grouped {
				summaries = append(summaries, s)
			}
			sort.Slice(summaries, func(i, j int) bool {
				if summaries[i].Total != summaries[j].Total {
					return summaries[i].Total > summaries[j].Total
				}
				return summaries[i].Last > summaries[j].Last
			})
			for _, s := range summaries {
				rows = append(rows, map[string]any{
					"name": s.Name, "number": s.Number, "total": s.Total,
					"missed": s.Missed, "inbound": s.Inbound, "outbound": s.Outbound,
					"last_call": s.Last, "in_phonebook": s.InBook,
				})
			}
			return fbEmit(cmd, flags, rows, "No calls in the router's journal for that period.")
		},
	}
	cmd.Flags().IntVar(&days, "days", 7, "How many days back to summarise")
	cmd.Flags().BoolVar(&missedOnly, "missed", false, "Only count missed calls")
	return cmd
}
