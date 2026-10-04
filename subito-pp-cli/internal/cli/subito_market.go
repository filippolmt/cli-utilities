// Hand-written: the comparables pipeline shared by market deals, market
// suggest, market stats and ads risk.

package cli

import (
	"context"
	"fmt"
	"math"
	"strings"

	"subito-pp-cli/internal/client"
	"subito-pp-cli/internal/cliutil"
	"subito-pp-cli/internal/store"
	"subito-pp-cli/internal/subito"

	"github.com/spf13/cobra"
)

// compsOpts selects and cleans the ads a market judgment is made on.
type compsOpts struct {
	search       searchOpts
	strict       bool
	keepVariants bool
	keepDamaged  bool
	keepAccess   bool
	exclude      []string
	maxScanPages int
	perM2        bool
}

// addCompsFlags adds the sample-cleaning flags; callers add the search
// flags themselves with addSearchFlags.
func addCompsFlags(cmd *cobra.Command, o *compsOpts) {
	cmd.Flags().BoolVar(&o.strict, "strict", true, "Keep only ads whose title contains every keyword (Subito mixes in nearby models); --strict=false keeps them")
	cmd.Flags().BoolVar(&o.keepVariants, "keep-variants", false, "Keep titles with model qualifiers missing from the query (pro, plus, max, mini, ultra, lite, fe)")
	cmd.Flags().BoolVar(&o.keepAccess, "include-accessories", false, "Keep accessory listings for the model (\"cover per ...\", \"staffa per ...\")")
	cmd.Flags().BoolVar(&o.keepDamaged, "include-damaged", false, "Keep titles of broken, crashed or parts-only items (da riparare, rotto, incidentata, ricambi...)")
	cmd.Flags().StringSliceVar(&o.exclude, "exclude", nil, "Drop ads whose title contains any of these words, e.g. --exclude ricambi,rotto")
	cmd.Flags().IntVar(&o.maxScanPages, "max-scan-pages", 3, "Search pages of 100 ads to scan")
	cmd.Flags().BoolVar(&o.perM2, "per-m2", false, "Compare price per square metre (real estate); ads without a size are skipped")
}

// compsRun is the cleaned sample plus the bookkeeping agents need to judge it.
type compsRun struct {
	Query           string         `json:"query"`
	Params          map[string]any `json:"params"`
	TotalOnSubito   int            `json:"total_on_subito"`
	ScannedAds      int            `json:"scanned_ads"`
	ScannedPages    int            `json:"scanned_pages"`
	DroppedOffModel int            `json:"dropped_off_model"`
	DroppedVariants int            `json:"dropped_variants"`
	DroppedDamaged  int            `json:"dropped_damaged"`
	DroppedAccess   int            `json:"dropped_accessories"`
	DroppedExcluded int            `json:"dropped_excluded"`
	DroppedNoPrice  int            `json:"dropped_no_price"`
	MergedCrossPost int            `json:"merged_cross_posts"`
	Kept            int            `json:"kept"`
	ScanCapHit      bool           `json:"scan_cap_hit"`
	Metric          string         `json:"metric"`
	groups          []subito.Group
	perM2           bool
}

// collectComps searches, records what it saw, then filters, dedupes and
// keeps only ads with a usable metric.
func collectComps(ctx context.Context, cmd *cobra.Command, c *client.Client, db *store.Store, o *compsOpts) (*compsRun, error) {
	if o.maxScanPages < 1 {
		return nil, usageErr(fmt.Errorf("--max-scan-pages must be at least 1"))
	}
	params, err := o.search.buildParams(ctx, c)
	if err != nil {
		return nil, err
	}
	res, err := fetchAds(ctx, c, params, o.maxScanPages, 100)
	if err != nil {
		return nil, err
	}
	recordAds(cmd, db, res.Ads)
	run := &compsRun{Query: o.search.query, Params: map[string]any{}, TotalOnSubito: res.Total, ScannedAds: len(res.Ads), ScannedPages: res.ScannedPages, ScanCapHit: res.CapHit, Metric: "price", perM2: o.perM2}
	for k, v := range params {
		run.Params[k] = v
	}
	if o.perM2 {
		run.Metric = "price_per_m2"
	}
	tokens := subito.Tokens(o.search.query)
	excl := make([]string, 0, len(o.exclude))
	for _, e := range o.exclude {
		excl = append(excl, subito.Tokens(e)...)
	}
	variants := []string{}
	dropPlus := false
	if o.strict && !o.keepVariants && len(tokens) > 0 {
		variants = subito.VariantsToDrop(tokens)
		dropPlus = !strings.Contains(o.search.query, "+")
	}
	kept := make([]subito.Ad, 0, len(res.Ads))
	for _, a := range res.Ads {
		switch {
		case o.strict && len(tokens) > 0 && !subito.MatchesAll(a.Subject, tokens):
			run.DroppedOffModel++
		case subito.ContainsAny(a.Subject, variants) || (dropPlus && subito.PlusVariant(a.Subject)):
			run.DroppedVariants++
		case o.strict && !o.keepAccess && (subito.Accessory(a.Subject, tokens) || (subito.AccessoryCategory(a.CategoryID) && params["c"] != a.CategoryID)):
			run.DroppedAccess++
		case !o.keepDamaged && subito.Damaged(a.Subject):
			run.DroppedDamaged++
		case subito.ContainsAny(a.Subject, excl):
			run.DroppedExcluded++
		default:
			if _, ok := subito.Metric(a, o.perM2); !ok {
				run.DroppedNoPrice++
				continue
			}
			kept = append(kept, a)
		}
	}
	groups := subito.Dedupe(kept)
	run.MergedCrossPost = len(kept) - len(groups)
	run.Kept = len(groups)
	run.groups = groups
	return run, nil
}

// distribution summarises one sample of metric values.
type distribution struct {
	N      int     `json:"n"`
	Min    float64 `json:"min"`
	P25    float64 `json:"p25"`
	Median float64 `json:"median"`
	P75    float64 `json:"p75"`
	Max    float64 `json:"max"`
}

func distributionOf(values []float64) *distribution {
	if len(values) == 0 {
		return nil
	}
	return &distribution{
		N: len(values), Min: round2(subito.Percentile(values, 0)), P25: round2(subito.Percentile(values, 25)),
		Median: round2(subito.Median(values)), P75: round2(subito.Percentile(values, 75)), Max: round2(subito.Percentile(values, 100)),
	}
}

func round2(f float64) float64 {
	if math.IsNaN(f) {
		return 0
	}
	return math.Round(f*100) / 100
}

// segmentValues splits the sample into private and pro metric values,
// leaving out exceptListID (the ad being judged, if it is in the sample).
func (r *compsRun) segmentValues(exceptListID string) map[string][]float64 {
	out := map[string][]float64{"private": {}, "pro": {}}
	for _, g := range r.groups {
		if exceptListID != "" && g.ListID == exceptListID {
			continue
		}
		if v, ok := subito.Metric(g.Ad, r.perM2); ok {
			out[subito.Segment(g.Ad)] = append(out[subito.Segment(g.Ad)], v)
		}
	}
	return out
}

// benchmark returns the median an ad is judged against: its own segment
// when that has enough ads, otherwise the whole sample (flagged).
func benchmark(seg map[string][]float64, a subito.Ad) (median float64, n int, basis string) {
	own := seg[subito.Segment(a)]
	if len(own) >= subito.MinComparables {
		return subito.Median(own), len(own), subito.Segment(a)
	}
	all := allValues(seg)
	if len(all) == 0 {
		return 0, 0, "none"
	}
	return subito.Median(all), len(all), "all_sellers"
}

func allValues(seg map[string][]float64) []float64 {
	return append(append([]float64(nil), seg["private"]...), seg["pro"]...)
}

// rejectLocalSource refuses --data-source local on commands that only make
// sense against live search results.
func rejectLocalSource(flags *rootFlags, name string) error {
	if flags.dataSource == "local" {
		return usageErr(fmt.Errorf("%s reads live search results; --data-source local has no equivalent", name))
	}
	return nil
}

// rejectLiveSource refuses --data-source live on local-only commands.
func rejectLiveSource(flags *rootFlags, name string) error {
	if flags.dataSource == "live" {
		return usageErr(fmt.Errorf("%s reads the local history only; --data-source live has no equivalent", name))
	}
	return nil
}

// openStoreOrWarn opens the default store; on failure it warns and returns
// nil so live commands still answer without recording.
func openStoreOrWarn(cmd *cobra.Command) *store.Store {
	db, err := openSubitoStore(cmd.Context(), "")
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: local store unavailable, listings will not be recorded: %v\n", err)
		return nil
	}
	return db
}

func sampleNote(run *compsRun) string {
	switch {
	case run.Kept == 0 && run.ScanCapHit:
		return fmt.Sprintf("no ad kept after scanning %d ads; raise --max-scan-pages or relax --strict", run.ScannedAds)
	case run.Kept == 0:
		return "no ad matched; check the spelling, try another spelling of the model, or --strict=false"
	case run.Kept < subito.MinComparables:
		return fmt.Sprintf("only %d comparable ads: treat the numbers as weak", run.Kept)
	case run.ScanCapHit:
		return fmt.Sprintf("sample is the newest %d of %d ads; raise --max-scan-pages for more", run.ScannedAds, run.TotalOnSubito)
	}
	return ""
}

func printMarketHeader(cmd *cobra.Command, run *compsRun, market map[string]*distribution) {
	out := cmd.OutOrStdout()
	for _, name := range []string{"private", "pro"} {
		if d := market[name]; d != nil {
			fmt.Fprintf(out, "%-8s n=%d  median %.0f  (p25 %.0f, p75 %.0f)\n", name, d.N, d.Median, d.P25, d.P75)
		}
	}
	fmt.Fprintf(out, "scanned %d of %d ads, kept %d (off-model %d, other variants %d, accessories %d, damaged %d, excluded %d, no price %d, cross-posts merged %d)\n\n",
		run.ScannedAds, run.TotalOnSubito, run.Kept, run.DroppedOffModel, run.DroppedVariants, run.DroppedAccess, run.DroppedDamaged, run.DroppedExcluded, run.DroppedNoPrice, run.MergedCrossPost)
}

// cell makes a remote string safe for one table cell: no tabs, newlines or
// terminal control sequences from listing text.
func cell(s string) string { return cliutil.ScrubTerminal(s) }
