// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live

package cli

import (
	"fmt"
	"sort"
	"strings"

	"subito-pp-cli/internal/subito"

	"github.com/spf13/cobra"
)

type dealRow struct {
	ListID     string   `json:"list_id"`
	Subject    string   `json:"subject"`
	Price      float64  `json:"price"`
	Value      float64  `json:"value"`
	Median     float64  `json:"market_median"`
	GapPct     float64  `json:"gap_pct"`
	Verdict    string   `json:"verdict"`
	Segment    string   `json:"segment"`
	Basis      string   `json:"compared_with"`
	Comps      int      `json:"comparables"`
	Town       string   `json:"town"`
	Province   string   `json:"province"`
	Shippable  bool     `json:"shippable"`
	Published  string   `json:"published"`
	URL        string   `json:"url"`
	Duplicates []string `json:"duplicate_list_ids,omitempty"`
}

type dealsView struct {
	*compsRun
	Market map[string]*distribution `json:"market"`
	Deals  []dealRow                `json:"deals"`
	Note   string                   `json:"note,omitempty"`
}

func newNovelMarketDealsCmd(flags *rootFlags) *cobra.Command {
	var o compsOpts
	var limit int
	var verdict string
	cmd := &cobra.Command{
		Use:   "deals [query]",
		Short: "Rank every ad for a search by how far it sits below or above the like-for-like market price.",
		Long: strings.Trim(`
Use this command to judge individual ads against the market for a query and rank them by % gap.
Do NOT use this command for aggregate price distribution only; use 'market stats' instead.
Do NOT use it to choose your own asking price; use 'market suggest' instead.

It searches Subito newest first, keeps only titles containing every keyword
(--strict), drops other variants of the model (a "pro", "plus" or "max"
title, or a "GT+" style trim, when the query has none; --keep-variants keeps
them), drops accessories for the model ("cover per ..."; --include-accessories
keeps them), drops broken or parts-only items (--include-damaged keeps them), merges the same item cross-posted in several towns, and compares
private sellers with private sellers and shops with shops. Each ad gets its
gap from that median and a verdict: below (<= -10%), in_line, above (>= +10%).
Every listing seen is recorded locally for 'ads history' and 'ads risk'.`, "\n"),
		Example: strings.Trim(`
  subito-pp-cli market deals "iphone 15 128" --category-id 12 --shippable --agent
  subito-pp-cli market deals "canon r6" --exclude ricambi,rotta --verdict below
  subito-pp-cli market deals trilocale --category appartamenti --region lombardia --province milano --per-m2`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "live", "pp:happy-args": "query=iphone 15;--category-id=12;--max-scan-pages=1"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "market deals")
			}
			if err := rejectLocalSource(flags, "market deals"); err != nil {
				return err
			}
			if len(args) > 0 {
				o.search.query = strings.Join(args, " ")
			}
			if strings.TrimSpace(o.search.query) == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("a search query is required, e.g. market deals \"iphone 15\""))
			}
			if limit < 1 {
				return usageErr(fmt.Errorf("--limit must be at least 1"))
			}
			switch verdict {
			case "", "below", "in_line", "above":
			default:
				return usageErr(fmt.Errorf("--verdict must be below, in_line or above"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			db := openStoreOrWarn(cmd)
			if db != nil {
				defer func() { _ = db.Close() }()
			}
			run, err := collectComps(ctx, cmd, c, db, &o)
			if err != nil {
				return classifyAPIError(cmd.OutOrStdout(), err, flags)
			}
			seg := run.segmentValues("")
			view := dealsView{compsRun: run, Market: map[string]*distribution{
				"private": distributionOf(seg["private"]), "pro": distributionOf(seg["pro"]),
			}, Deals: make([]dealRow, 0, len(run.groups))}
			for _, g := range run.groups {
				value, _ := subito.Metric(g.Ad, o.perM2)
				median, n, basis := benchmark(seg, g.Ad)
				gap := subito.GapPct(value, median)
				row := dealRow{
					ListID: g.ListID, Subject: g.Subject, Price: g.Price, Value: value, Median: round2(median),
					GapPct: gap, Verdict: subito.Verdict(gap), Segment: subito.Segment(g.Ad), Basis: basis, Comps: n,
					Town: g.Town, Province: g.Province, Shippable: g.Shippable, Published: g.Published, URL: g.URL,
					Duplicates: g.Duplicates,
				}
				if verdict != "" && row.Verdict != verdict {
					continue
				}
				view.Deals = append(view.Deals, row)
			}
			sort.SliceStable(view.Deals, func(i, j int) bool { return view.Deals[i].GapPct < view.Deals[j].GapPct })
			if limit > 0 && len(view.Deals) > limit {
				view.Deals = view.Deals[:limit]
			}
			view.Note = sampleNote(run)
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(), view, flags)
			}
			return printDealsTable(cmd, view)
		},
	}
	addSearchFlags(cmd, &o.search)
	addCompsFlags(cmd, &o)
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum ads to list, best deals first")
	cmd.Flags().StringVar(&verdict, "verdict", "", "Only list ads with this verdict: below, in_line or above")
	return cmd
}

func printDealsTable(cmd *cobra.Command, v dealsView) error {
	out := cmd.OutOrStdout()
	printMarketHeader(cmd, v.compsRun, v.Market)
	if len(v.Deals) == 0 {
		fmt.Fprintln(out, "No ads to rank.")
		if v.Note != "" {
			fmt.Fprintln(out, v.Note)
		}
		return nil
	}
	tw := newTabWriter(out)
	fmt.Fprintln(tw, "GAP\tVERDICT\tPRICE\tSELLER\tTOWN\tTITLE\tURL")
	for _, d := range v.Deals {
		fmt.Fprintf(tw, "%+.0f%%\t%s\t%.0f\t%s\t%s\t%s\t%s\n", d.GapPct, d.Verdict, d.Price, d.Segment, cell(d.Town), cell(truncate(d.Subject, 50)), d.URL)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if v.Note != "" {
		fmt.Fprintln(out, "\n"+v.Note)
	}
	return nil
}
