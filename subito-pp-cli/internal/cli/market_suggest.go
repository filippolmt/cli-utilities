// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live

package cli

import (
	"fmt"
	"strings"

	"subito-pp-cli/internal/subito"

	"github.com/spf13/cobra"
)

// minSuggestComps is the subito-listing rule: never price from fewer than
// five comparable ads.
const minSuggestComps = 5

type suggestComp struct {
	ListID  string  `json:"list_id"`
	Subject string  `json:"subject"`
	Price   float64 `json:"price"`
	Town    string  `json:"town"`
	URL     string  `json:"url"`
}

type suggestView struct {
	*compsRun
	Segment     string        `json:"segment"`
	DroppedShop int           `json:"dropped_shop_ads"`
	Enough      bool          `json:"enough"`
	Listing     float64       `json:"listing_price,omitempty"`
	Minimum     float64       `json:"minimum_acceptable,omitempty"`
	QuickSale   float64       `json:"quick_sale,omitempty"`
	Market      *distribution `json:"market,omitempty"`
	Comparables []suggestComp `json:"comparables"`
	Caveat      string        `json:"caveat"`
	Note        string        `json:"note,omitempty"`
}

func newNovelMarketSuggestCmd(flags *rootFlags) *cobra.Command {
	var o compsOpts
	var includeShops bool
	cmd := &cobra.Command{
		Use:   "suggest [query]",
		Short: "Get a listing price, a minimum and a quick-sale price from at least five comparable ads.",
		Long: strings.Trim(`
Use this command to choose an asking price for an item you are selling.
Do NOT use it to rank existing ads as deals; use 'market deals' instead.

It collects comparable live ads the same way 'market deals' does (exact
model, no other variants, cross-posts merged) from private sellers only,
unless --include-shops. With at least five comparables it suggests:
  listing_price       about the 60th percentile, room to negotiate
  minimum_acceptable  about the 25th percentile, the floor to accept
  quick_sale          about the 35th percentile, to sell within days
Subito publishes asking prices only, never sold prices: these numbers say
what similar items are offered at, not what they sold for.`, "\n"),
		Example: strings.Trim(`
  subito-pp-cli market suggest "yamaha tracer 9 gt" --category-id 3
  subito-pp-cli market suggest "iphone 15 128" --category telefonia --shippable --agent`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "live", "pp:happy-args": "query=iphone 15;--category-id=12;--max-scan-pages=1"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "market suggest")
			}
			if err := rejectLocalSource(flags, "market suggest"); err != nil {
				return err
			}
			if o.perM2 {
				return usageErr(fmt.Errorf("market suggest prices a whole item; use 'market stats --per-m2' for price per square metre"))
			}
			if len(args) > 0 {
				o.search.query = strings.Join(args, " ")
			}
			if strings.TrimSpace(o.search.query) == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("describe the item to price, e.g. market suggest \"iphone 15 128\""))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			db := openStoreOrWarn(cmd)
			if db != nil {
				defer db.Close()
			}
			run, err := collectComps(ctx, cmd, c, db, &o)
			if err != nil {
				return classifyAPIError(cmd.OutOrStdout(), err, flags)
			}
			view := suggestView{compsRun: run, Segment: "private", Comparables: make([]suggestComp, 0),
				Caveat: "Based on asking prices of live Subito ads; Subito does not publish sold prices."}
			if includeShops {
				view.Segment = "all_sellers"
			}
			values := make([]float64, 0, len(run.groups))
			for _, g := range run.groups {
				if g.Company && !includeShops {
					view.DroppedShop++
					continue
				}
				v, ok := subito.Metric(g.Ad, o.perM2)
				if !ok {
					continue
				}
				values = append(values, v)
				view.Comparables = append(view.Comparables, suggestComp{ListID: g.ListID, Subject: g.Subject, Price: g.Price, Town: g.Town, URL: g.URL})
			}
			view.Market = distributionOf(values)
			view.Enough = len(values) >= minSuggestComps
			if view.Enough {
				view.Listing = subito.RoundPrice(subito.Percentile(values, 60))
				view.QuickSale = subito.RoundPrice(subito.Percentile(values, 35))
				view.Minimum = subito.RoundPrice(subito.Percentile(values, 25))
				view.Note = sampleNote(run)
			} else {
				view.Note = fmt.Sprintf("only %d comparable ads (need %d): no price suggested; try another spelling, --strict=false, a wider --region or --include-shops", len(values), minSuggestComps)
			}
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(), view, flags)
			}
			out := cmd.OutOrStdout()
			if !view.Enough {
				fmt.Fprintln(out, view.Note)
				return nil
			}
			fmt.Fprintf(out, "listing price       %.0f\nminimum acceptable  %.0f\nquick sale          %.0f\n\n", view.Listing, view.Minimum, view.QuickSale)
			fmt.Fprintf(out, "from %d %s comparables: median %.0f, p25 %.0f, p75 %.0f\n%s\n", view.Market.N, view.Segment, view.Market.Median, view.Market.P25, view.Market.P75, view.Caveat)
			if view.Note != "" {
				fmt.Fprintln(out, view.Note)
			}
			fmt.Fprintln(out)
			tw := newTabWriter(out)
			fmt.Fprintln(tw, "PRICE\tTOWN\tTITLE\tURL")
			for _, cp := range view.Comparables {
				fmt.Fprintf(tw, "%.0f\t%s\t%s\t%s\n", cp.Price, cell(cp.Town), cell(truncate(cp.Subject, 50)), cp.URL)
			}
			return tw.Flush()
		},
	}
	addSearchFlags(cmd, &o.search)
	addCompsFlags(cmd, &o)
	_ = cmd.Flags().MarkHidden("per-m2") // rejected in RunE: suggest prices whole items
	cmd.Flags().BoolVar(&includeShops, "include-shops", false, "Also use shop listings as comparables (shops ask more and include warranty)")
	return cmd
}
