// Hand-written: market stats, the price distribution of a search.
// pp:data-source live

package cli

import (
	"fmt"
	"sort"
	"strings"

	"subito-pp-cli/internal/subito"

	"github.com/spf13/cobra"
)

type regionStat struct {
	Region string `json:"region"`
	*distribution
}

type statsView struct {
	*compsRun
	Market   map[string]*distribution `json:"market"`
	ByRegion []regionStat             `json:"by_region"`
	Note     string                   `json:"note,omitempty"`
}

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		market, _, err := root.Find([]string{"market"})
		if err == nil && market != nil && market.Name() == "market" {
			addNovelCommandIfAbsent(market, newMarketStatsCmd(flags))
		}
	})
}

func newMarketStatsCmd(flags *rootFlags) *cobra.Command {
	var o compsOpts
	cmd := &cobra.Command{
		Use:   "stats [query]",
		Short: "Price distribution of a search: min, quartiles, median and max, by seller type and region",
		Long: strings.Trim(`
Use this command for the aggregate price picture of a query.
Do NOT use it to judge single ads; use 'market deals' instead.

The sample is cleaned like 'market deals': exact model, other variants
dropped, cross-posts merged. Add --per-m2 for real estate.`, "\n"),
		Example: strings.Trim(`
  subito-pp-cli market stats "bici da corsa carbonio" --category bici
  subito-pp-cli market stats bilocale --category appartamenti --ad-type u --region lazio --per-m2`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "live", "pp:happy-args": "query=iphone 15;--category-id=12;--max-scan-pages=1"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "market stats")
			}
			if err := rejectLocalSource(flags, "market stats"); err != nil {
				return err
			}
			if len(args) > 0 {
				o.search.query = strings.Join(args, " ")
			}
			if strings.TrimSpace(o.search.query) == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("a search query is required, e.g. market stats \"canon r6\""))
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
			seg := run.segmentValues("")
			all := allValues(seg)
			view := statsView{compsRun: run, ByRegion: make([]regionStat, 0), Note: sampleNote(run),
				Market: map[string]*distribution{"all": distributionOf(all), "private": distributionOf(seg["private"]), "pro": distributionOf(seg["pro"])}}
			byRegion := map[string][]float64{}
			for _, g := range run.groups {
				if v, ok := subito.Metric(g.Ad, o.perM2); ok {
					byRegion[g.Region] = append(byRegion[g.Region], v)
				}
			}
			for r, vals := range byRegion {
				view.ByRegion = append(view.ByRegion, regionStat{Region: r, distribution: distributionOf(vals)})
			}
			sort.Slice(view.ByRegion, func(i, j int) bool {
				if view.ByRegion[i].N != view.ByRegion[j].N {
					return view.ByRegion[i].N > view.ByRegion[j].N
				}
				return view.ByRegion[i].Region < view.ByRegion[j].Region
			})
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(), view, flags)
			}
			printMarketHeader(cmd, run, view.Market)
			if len(view.ByRegion) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No priced ads in the sample.")
			} else {
				tw := newTabWriter(cmd.OutOrStdout())
				fmt.Fprintln(tw, "REGION\tN\tMIN\tMEDIAN\tMAX")
				for _, r := range view.ByRegion {
					fmt.Fprintf(tw, "%s\t%d\t%.0f\t%.0f\t%.0f\n", cell(r.Region), r.N, r.Min, r.Median, r.Max)
				}
				if err := tw.Flush(); err != nil {
					return err
				}
			}
			if view.Note != "" {
				fmt.Fprintln(cmd.OutOrStdout(), "\n"+view.Note)
			}
			return nil
		},
	}
	addSearchFlags(cmd, &o.search)
	addCompsFlags(cmd, &o)
	return cmd
}
