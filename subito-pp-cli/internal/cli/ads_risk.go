// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live

package cli

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"subito-pp-cli/internal/client"
	"subito-pp-cli/internal/store"
	"subito-pp-cli/internal/subito"

	"github.com/spf13/cobra"
)

type riskView struct {
	ListID       string          `json:"list_id"`
	Subject      string          `json:"subject"`
	Price        float64         `json:"price"`
	URL          string          `json:"url"`
	Seller       string          `json:"seller_segment"`
	Verdict      string          `json:"verdict"`
	FiredCount   int             `json:"fired_count"`
	Signals      []subito.Signal `json:"signals"`
	MarketMedian float64         `json:"market_median"`
	Comparables  int             `json:"comparables"`
	CompsQuery   string          `json:"comparables_query"`
	Source       string          `json:"ad_source"`
}

func newNovelAdsRiskCmd(flags *rootFlags) *cobra.Command {
	var compsQuery string
	var maxScanPages int
	cmd := &cobra.Command{
		Use:   "risk [url-or-id]",
		Short: "Check one ad for mechanical scam signals with the evidence for each.",
		Long: strings.Trim(`
Use this command to check one ad for scam signals.
Do NOT use it for the ad's full text and photos; use 'ads get' instead.
Do NOT use it for price or renewal history; use 'ads history' instead.

Signals, each reported with its evidence:
  price_far_below_market          25%+ under the like-for-like median, no reason given
  seller_many_similar_ads         a private seller with 2+ other ads of similar value seen locally
  contact_off_platform            WhatsApp, Telegram, e-mail or a phone number in the text
  deposit_or_untraceable_payment  deposit, prepaid top-up, Western Union, "amici e parenti"
  shipping_only_for_pickup_item   "solo spedizione" on vehicles, property or furniture
  catalog_photos                  not checked: needs a human look at the photos
Verdict: exclude with 2+ signals, caution with 1, ok with none. The median
comes from a live search on the first words of the title (--comps-query to
change it); the seller check only knows ads this CLI has already seen.`, "\n"),
		Example: strings.Trim(`
  subito-pp-cli ads risk https://www.subito.it/biciclette/bici-da-corsa-milano-663258568.htm
  subito-pp-cli ads risk 663258568 --comps-query "bici corsa" --agent`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "live", "pp:happy-args": "ref=663258568;--max-scan-pages=1"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "ads risk")
			}
			if err := rejectLocalSource(flags, "ads risk"); err != nil {
				return err
			}
			if len(args) != 1 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("pass one ad URL, urn or list id"))
			}
			ref, err := subito.ParseRef(args[0])
			if err != nil {
				return usageErr(err)
			}
			if maxScanPages < 1 {
				return usageErr(fmt.Errorf("--max-scan-pages must be at least 1"))
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
			ad, source, err := loadAd(ctx, cmd, flags, c, db, ref)
			if err != nil {
				return classifyAPIError(cmd.OutOrStdout(), err, flags)
			}
			if compsQuery == "" {
				compsQuery = defaultCompsQuery(ad.Subject)
			}
			o := compsOpts{strict: true, maxScanPages: maxScanPages}
			o.search.query = compsQuery
			o.search.category = ad.CategoryID
			o.search.adType = ad.AdType
			if o.search.adType == "" {
				o.search.adType = "s"
			}
			run, err := collectComps(ctx, cmd, c, db, &o)
			if err != nil {
				return classifyAPIError(cmd.OutOrStdout(), err, flags)
			}
			median, n, _ := benchmark(run.segmentValues(ad.ListID), ad)
			in := subito.RiskInput{Ad: ad, MarketMedian: median, Comparables: n, SellerSimilar: -1}
			if db != nil && ad.UserID != "" && subito.Priced(ad) {
				in.SellerSimilar, err = similarSellerAds(ctx, db, ad)
				if err != nil {
					return err
				}
			}
			signals := subito.Risk(in)
			verdict, fired := subito.RiskVerdict(signals)
			view := riskView{ListID: ad.ListID, Subject: ad.Subject, Price: ad.Price, URL: ad.URL, Seller: subito.Segment(ad),
				Verdict: verdict, FiredCount: fired, Signals: signals, MarketMedian: round2(median), Comparables: n,
				CompsQuery: compsQuery, Source: source}
			if ad.UserID == "" {
				view.Seller = "unknown"
			}
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(), view, flags)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s · %.0f € · %s\nverdict: %s (%d signal(s)), median %.0f over %d ads for %q\n\n", cell(view.Subject), view.Price, view.URL, view.Verdict, view.FiredCount, view.MarketMedian, view.Comparables, view.CompsQuery)
			tw := newTabWriter(out)
			fmt.Fprintln(tw, "SIGNAL\tSTATE\tDETAIL")
			for _, s := range signals {
				state, detail := "clear", s.Note
				switch {
				case s.Fired:
					state, detail = "FIRED", s.Evidence
				case !s.Checked:
					state = "unchecked"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Name, state, cell(detail))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&compsQuery, "comps-query", "", "Search used to measure the market (default: first words of the title)")
	cmd.Flags().IntVar(&maxScanPages, "max-scan-pages", 2, "Search pages of 100 ads to scan for comparables")
	return cmd
}

// defaultCompsQuery keeps the first four words of a title: enough to name
// the model, few enough that --strict still finds comparables.
func defaultCompsQuery(subject string) string {
	t := subito.Tokens(subject)
	if len(t) > 4 {
		t = t[:4]
	}
	return strings.Join(t, " ")
}

// loadAd finds the full search record of a listing: from the local store,
// or by searching its page title live. When the search does not return it,
// the ad is rebuilt from the page alone, without seller data.
func loadAd(ctx context.Context, cmd *cobra.Command, flags *rootFlags, c *client.Client, db *store.Store, ref subito.Ref) (subito.Ad, string, error) {
	if db != nil {
		if l, _, err := db.SubitoListing(ctx, ref.ListID); err == nil && len(l.Raw) > 0 {
			if a, err := subito.ParseAd(l.Raw); err == nil {
				return a, "local_store", nil
			}
		}
	}
	d, err := fetchAdPage(ctx, flags, adPageURL(ref))
	if err != nil {
		return subito.Ad{}, "", err
	}
	category := categoryFromURL(d.URL)
	catID, _ := resolveCategory(ctx, c, category)
	// The ad page does not say sale vs rent; a title search with the
	// category finds the ad and its real type in most cases.
	params := map[string]string{"q": d.Name}
	if catID != "" {
		params["c"] = catID
	}
	if res, err := fetchAds(ctx, c, params, 1, 100); err == nil {
		recordAds(cmd, db, res.Ads)
		for _, a := range res.Ads {
			if a.ListID == ref.ListID {
				return a, "live_search", nil
			}
		}
	}
	return subito.Ad{ListID: ref.ListID, Subject: d.Name, Body: d.Description, Price: d.Price, HasPrice: d.Price > 0,
		CategoryID: catID, Category: category, URL: d.URL, ImageCount: len(d.Images)}, "ad_page", nil
}

// categoryFromURL reads the category slug from /<category>/<slug>-<id>.htm.
func categoryFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[0]
}

// similarSellerAds counts the seller's other locally seen items in the same
// category priced at least half this one. Cross-posts of this same item
// (same normalized title) are not other items.
func similarSellerAds(ctx context.Context, db *store.Store, ad subito.Ad) (int, error) {
	others, err := db.SubitoSellerListings(ctx, ad.UserID, ad.ListID)
	if err != nil {
		return 0, err
	}
	self := subito.NormalizeSubject(ad.Subject)
	items := map[string]bool{}
	for _, o := range others {
		if o.CategoryID == ad.CategoryID && o.Price.Valid && o.Price.Float64 >= ad.Price/2 && o.SubjectNorm != self {
			items[o.SubjectNorm] = true
		}
	}
	return len(items), nil
}
