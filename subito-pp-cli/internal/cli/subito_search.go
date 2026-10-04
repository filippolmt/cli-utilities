// Hand-written: shared search plumbing for subito novel commands (market,
// watch, ads risk/get) and the name-resolution flags added to `ads search`.

package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"subito-pp-cli/internal/client"
	"subito-pp-cli/internal/cliutil"
	"subito-pp-cli/internal/store"
	"subito-pp-cli/internal/subito"

	"github.com/spf13/cobra"
)

// pageDelay spaces out consecutive search pages. Subito sits behind Akamai;
// a steady human-ish pace keeps the client off its radar.
const pageDelay = 700 * time.Millisecond

// listingsResource names the recorded-listings history in sync_state.
const listingsResource = "subito_listings"

// searchOpts holds the human-facing search flags shared by novel commands.
type searchOpts struct {
	query      string
	category   string
	region     string
	province   string
	town       string
	adType     string
	priceMin   int
	priceMax   int
	shippable  bool
	titleOnly  bool
	advertiser string
	params     []string
}

func addSearchFlags(cmd *cobra.Command, o *searchOpts) {
	cmd.Flags().StringVar(&o.query, "query", "", "Keywords to search for")
	cmd.Flags().StringVar(&o.category, "category", "", "Category name or id, e.g. telefonia, auto, 41")
	cmd.Flags().StringVar(&o.category, "category-id", "", "Category id; same as --category")
	cmd.Flags().StringVar(&o.region, "region", "", "Region name or id, e.g. lombardia, 4")
	cmd.Flags().StringVar(&o.region, "region-id", "", "Region id; same as --region")
	cmd.Flags().StringVar(&o.province, "province", "", "Province name or id within --region, e.g. milano")
	cmd.Flags().StringVar(&o.town, "town", "", "Town name or ISTAT code within --province, e.g. monza")
	cmd.Flags().StringVar(&o.adType, "ad-type", "s", "Ad type key: s vendita, k cerco, u affitto, h affitto vacanze, g regalo")
	cmd.Flags().IntVar(&o.priceMin, "price-min", 0, "Minimum price in euro")
	cmd.Flags().IntVar(&o.priceMax, "price-max", 0, "Maximum price in euro")
	cmd.Flags().BoolVar(&o.shippable, "shippable", false, "Only listings that can be shipped (TuttoSubito)")
	cmd.Flags().BoolVar(&o.titleOnly, "title-only", false, "Let Subito match keywords in the title only")
	cmd.Flags().StringVar(&o.advertiser, "advertiser", "", "private or shop")
	cmd.Flags().StringArrayVar(&o.params, "param", nil, "Extra hades filter as key=value, e.g. --param fl=2 (see 'categories filters')")
}

// categoryAliases maps English and colloquial names to Subito category ids.
var categoryAliases = map[string]string{
	"cars": "2", "car": "2", "auto": "2", "macchine": "2",
	"moto": "3", "motorbikes": "3", "motorcycles": "3", "scooter": "3",
	"flats": "7", "apartments": "7", "case": "7",
	"bici": "41", "bikes": "41", "bicycles": "41",
	"phones": "12", "smartphone": "12", "cellulari": "12",
	"computers": "10", "pc": "10", "laptop": "10",
	"cameras": "40", "fotocamere": "40",
	"console": "44", "videogames": "44",
	"furniture": "14", "mobili": "14",
	"clothes": "16", "vestiti": "16",
}

type placeValue struct {
	Key          string `json:"key"`
	Value        string `json:"value"`
	FriendlyName string `json:"friendly_name"`
	Istat        string `json:"istat"`
}

func fetchValues(ctx context.Context, c *client.Client, path string, params map[string]string) ([]placeValue, error) {
	data, err := c.Get(ctx, path, params)
	if err != nil {
		return nil, err
	}
	var env struct {
		Values []placeValue `json:"values"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return env.Values, nil
}

func slugify(s string) string {
	return strings.Join(subito.Tokens(strings.ReplaceAll(s, "'", " ")), "-")
}

// matchValue finds name among values by key, ISTAT code, display name or
// friendly slug. Ambiguous prefix matches are an error naming the options.
func matchValue(kind, name string, values []placeValue) (placeValue, error) {
	want := slugify(name)
	var prefix []placeValue
	for _, v := range values {
		if v.Key == name || (v.Istat != "" && v.Istat == name) || slugify(v.Value) == want || v.FriendlyName == want {
			return v, nil
		}
		if want != "" && strings.HasPrefix(slugify(v.Value), want) {
			prefix = append(prefix, v)
		}
	}
	if len(prefix) == 1 {
		return prefix[0], nil
	}
	names := make([]string, 0, len(values))
	for _, v := range values {
		names = append(names, v.Value)
	}
	if len(prefix) > 1 {
		names = names[:0]
		for _, v := range prefix {
			names = append(names, v.Value)
		}
		return placeValue{}, usageErr(fmt.Errorf("%s %q is ambiguous: %s", kind, name, strings.Join(names, ", ")))
	}
	if len(names) > 25 {
		names = append(names[:25], "...")
	}
	return placeValue{}, usageErr(fmt.Errorf("unknown %s %q; options: %s", kind, name, strings.Join(names, ", ")))
}

// resolvePlace turns region/province/town names into hades r, ci and to.
func resolvePlace(ctx context.Context, c *client.Client, region, province, town string) (map[string]string, error) {
	out := map[string]string{}
	if region == "" {
		if province != "" || town != "" {
			return nil, usageErr(fmt.Errorf("--province and --town need --region"))
		}
		return out, nil
	}
	regions, err := fetchValues(ctx, c, "/geo/regions", nil)
	if err != nil {
		return nil, fmt.Errorf("loading regions: %w", err)
	}
	r, err := matchValue("region", region, regions)
	if err != nil {
		return nil, err
	}
	out["r"] = r.Key
	if province == "" {
		if town != "" {
			return nil, usageErr(fmt.Errorf("--town needs --province"))
		}
		return out, nil
	}
	provinces, err := fetchValues(ctx, c, "/geo/regions/"+r.Key+"/cities", nil)
	if err != nil {
		return nil, fmt.Errorf("loading provinces of %s: %w", r.Value, err)
	}
	p, err := matchValue("province", province, provinces)
	if err != nil {
		return nil, err
	}
	out["ci"] = p.Key
	if town == "" {
		return out, nil
	}
	towns, err := fetchValues(ctx, c, "/geo/regions/"+r.Key+"/cities/"+p.Key+"/towns", nil)
	if err != nil {
		return nil, fmt.Errorf("loading towns of %s: %w", p.Value, err)
	}
	t, err := matchValue("town", town, towns)
	if err != nil {
		return nil, err
	}
	out["to"] = t.Istat
	if out["to"] == "" {
		out["to"] = t.Key
	}
	return out, nil
}

func resolveCategory(ctx context.Context, c *client.Client, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	if _, err := strconv.Atoi(name); err == nil {
		return name, nil
	}
	if id, ok := categoryAliases[slugify(name)]; ok {
		return id, nil
	}
	cats, err := fetchValues(ctx, c, "/values/categories", nil)
	if err != nil {
		return "", fmt.Errorf("loading categories: %w", err)
	}
	v, err := matchValue("category", name, cats)
	if err != nil {
		return "", err
	}
	return v.Key, nil
}

func parseParamFlags(raw []string) (map[string]string, error) {
	out := map[string]string{}
	for _, kv := range raw {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, usageErr(fmt.Errorf("--param %q must be key=value", kv))
		}
		out[strings.TrimSpace(k)] = v
	}
	return out, nil
}

// buildParams resolves searchOpts into the hades query map.
func (o *searchOpts) buildParams(ctx context.Context, c *client.Client) (map[string]string, error) {
	params, err := parseParamFlags(o.params)
	if err != nil {
		return nil, err
	}
	if o.query != "" {
		params["q"] = o.query
	}
	if o.adType != "" {
		params["t"] = o.adType
	}
	cat, err := resolveCategory(ctx, c, o.category)
	if err != nil {
		return nil, err
	}
	if cat != "" {
		params["c"] = cat
	}
	place, err := resolvePlace(ctx, c, o.region, o.province, o.town)
	if err != nil {
		return nil, err
	}
	for k, v := range place {
		params[k] = v
	}
	if o.priceMin > 0 {
		params["ps"] = strconv.Itoa(o.priceMin)
	}
	if o.priceMax > 0 {
		params["pe"] = strconv.Itoa(o.priceMax)
	}
	if o.shippable {
		params["shp"] = "true"
	}
	if o.titleOnly {
		params["qso"] = "true"
	}
	switch strings.ToLower(o.advertiser) {
	case "":
	case "private", "privato":
		params["advt"] = "0,2"
	case "shop", "pro", "dealer", "negozio":
		params["advt"] = "1"
	default:
		return nil, usageErr(fmt.Errorf("--advertiser must be private or shop, got %q", o.advertiser))
	}
	return params, nil
}

// searchResult is what fetchAds collected across pages.
type searchResult struct {
	Total        int
	Ads          []subito.Ad
	ScannedPages int
	CapHit       bool
}

// fetchAds pages /search/items newest first until maxPages or the end.
func fetchAds(ctx context.Context, c *client.Client, params map[string]string, maxPages, pageSize int) (searchResult, error) {
	if cliutil.IsDogfoodEnv() && maxPages > 1 {
		maxPages = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 100
	}
	q := make(map[string]string, len(params)+3)
	for k, v := range params {
		q[k] = v
	}
	if q["sort"] == "" {
		q["sort"] = "datedesc"
	}
	q["lim"] = strconv.Itoa(pageSize)
	res := searchResult{Ads: make([]subito.Ad, 0)}
	start := 0
	for page := 0; page < maxPages; page++ {
		if page > 0 {
			if err := sleepCtx(ctx, pageDelay); err != nil {
				return res, err
			}
		}
		q["start"] = strconv.Itoa(start)
		data, err := c.Get(ctx, "/search/items", q)
		if err != nil {
			return res, fmt.Errorf("searching (page %d): %w", page+1, err)
		}
		p, err := subito.ParseSearch(data)
		if err != nil {
			return res, err
		}
		res.ScannedPages++
		res.Total = p.Total
		res.Ads = append(res.Ads, p.Ads...)
		start += p.Returned
		if p.Returned == 0 || start >= p.Total {
			return res, nil
		}
	}
	res.CapHit = start < res.Total
	return res, nil
}

// sleepCtx waits d or until ctx is cancelled.
func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

func listingRecord(a subito.Ad) store.SubitoListing {
	l := store.SubitoListing{
		ListID: a.ListID, URN: a.URN, UserID: a.UserID, Company: a.Company,
		CategoryID: a.CategoryID, Subject: a.Subject, SubjectNorm: subito.NormalizeSubject(a.Subject),
		Published: a.Published, URL: a.URL, Raw: a.Raw,
	}
	if subito.Priced(a) {
		l.Price = sql.NullFloat64{Float64: a.Price, Valid: true}
	}
	return l
}

// openSubitoStore opens the local store; dbPath "" means the default.
func openSubitoStore(ctx context.Context, dbPath string) (*store.Store, error) {
	if dbPath == "" {
		dbPath = defaultDBPath("subito-pp-cli")
	}
	return store.OpenWithContext(ctx, dbPath)
}

// recordAds keeps every listing a command saw, so history, seller and
// repost queries compound. A store failure only warns: the live answer the
// user asked for is still valid.
func recordAds(cmd *cobra.Command, db *store.Store, ads []subito.Ad) {
	if db == nil || len(ads) == 0 {
		return
	}
	recs := make([]store.SubitoListing, 0, len(ads))
	for _, a := range ads {
		if a.ListID != "" {
			recs = append(recs, listingRecord(a))
		}
	}
	if _, err := db.RecordSubitoListings(cmd.Context(), recs, time.Now()); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not record listings locally: %v\n", err)
		return
	}
	// Lets the generated sync hints know listings have been recorded.
	_ = db.SaveSyncState(listingsResource, "", len(recs))
}

func dbFileExists(dbPath string) bool {
	if dbPath == "" {
		dbPath = defaultDBPath("subito-pp-cli")
	}
	_, err := os.Stat(dbPath)
	return err == nil
}

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		search, _, err := root.Find([]string{"ads", "search"})
		if err != nil || search == nil || search.Name() != "search" {
			return
		}
		attachNameFlags(search, flags)
	})
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		rec, _, err := root.Find([]string{"ads", "recommended"})
		if err != nil || rec == nil || rec.Name() != "recommended" {
			return
		}
		// The generator replaces UUIDs in spec examples and fixtures with a
		// placeholder, but hades needs the real listing urn (a public id).
		rec.Example = "  subito-pp-cli ads recommended --urn " + recommendedFixtureURN + " --limit 10"
		if rec.Annotations == nil {
			rec.Annotations = map[string]string{}
		}
		rec.Annotations["pp:happy-args"] = "--urn=" + recommendedFixtureURN
	})
}

// recommendedFixtureURN is a live listing used for the help example and the
// live test fixture; refresh it when the ad expires (hades then answers 404).
const recommendedFixtureURN = "id:ad:f2233452-c527-4139-b3c2-43b343147fa0:list:663258568"

// attachNameFlags lets `ads search` take --region lombardia instead of
// --region-id 4, and --param for filters the typed flags do not cover.
func attachNameFlags(search *cobra.Command, flags *rootFlags) {
	var region, province, town, category string
	var extra []string
	search.Flags().StringVar(&region, "region", "", "Region name (e.g. lombardia); resolved to --region-id")
	search.Flags().StringVar(&province, "province", "", "Province name within --region (e.g. milano); resolved to --province-id")
	search.Flags().StringVar(&town, "town", "", "Town name within --province (e.g. monza); resolved to --town-istat")
	search.Flags().StringVar(&category, "category", "", "Category name (e.g. telefonia, bici); resolved to --category-id")
	search.Flags().StringArrayVar(&extra, "param", nil, "Extra hades filter as key=value, e.g. --param fl=2 (see 'categories filters')")
	generated := search.RunE
	search.RunE = func(cmd *cobra.Command, args []string) error {
		if region == "" && province == "" && town == "" && category == "" && len(extra) == 0 {
			return generated(cmd, args)
		}
		// Name resolution calls the API, so a dry run must stop before it.
		if dryRunOK(flags) {
			return writeDryRun(cmd.OutOrStdout(), flags, fmt.Sprintf("ads search with region=%q province=%q town=%q category=%q params=%v (names resolved at run time)", region, province, town, category, extra))
		}
		if len(extra) > 0 {
			if err := rejectLocalSource(flags, "ads search --param"); err != nil {
				return err
			}
		}
		c, err := flags.newClient()
		if err != nil {
			return err
		}
		ctx, cancel := boundCtx(cmd.Context(), flags)
		defer cancel()
		if region != "" || province != "" || town != "" {
			place, err := resolvePlace(ctx, c, region, province, town)
			if err != nil {
				return err
			}
			for wire, flag := range map[string]string{"r": "region-id", "ci": "province-id", "to": "town-istat"} {
				if v, ok := place[wire]; ok {
					if err := cmd.Flags().Set(flag, v); err != nil {
						return err
					}
				}
			}
		}
		if category != "" {
			id, err := resolveCategory(ctx, c, category)
			if err != nil {
				return err
			}
			if err := cmd.Flags().Set("category-id", id); err != nil {
				return err
			}
		}
		if len(extra) == 0 {
			return generated(cmd, args)
		}
		return runSearchWithExtraParams(ctx, cmd, flags, c, extra)
	}
}

// runSearchWithExtraParams serves `ads search --param ...`: the generated
// command cannot carry arbitrary query keys, so this path rebuilds the query
// from the typed flags plus the extras and prints the listings it returns.
func runSearchWithExtraParams(ctx context.Context, cmd *cobra.Command, flags *rootFlags, c *client.Client, extra []string) error {
	params, err := parseParamFlags(extra)
	if err != nil {
		return err
	}
	// Mirrors the wire map of the generated ads search; a missing flag means
	// a reprint renamed it, which must fail loudly rather than drop a filter.
	for wire, flag := range map[string]string{
		"q": "query", "c": "category-id", "t": "ad-type", "r": "region-id", "ci": "province-id",
		"to": "town-istat", "ps": "price-min", "pe": "price-max", "shp": "shippable",
		"qso": "title-only", "advt": "advertiser-type", "sort": "sort", "lim": "page-size", "start": "start",
	} {
		f := cmd.Flags().Lookup(flag)
		if f == nil {
			return fmt.Errorf("ads search has no --%s flag; update runSearchWithExtraParams to the regenerated command", flag)
		}
		if f.Changed || (f.DefValue != "" && f.DefValue != "0" && f.DefValue != "false") {
			if _, set := params[wire]; !set {
				params[wire] = f.Value.String()
			}
		}
	}
	data, err := c.Get(ctx, "/search/items", params)
	if err != nil {
		return classifyAPIError(cmd.OutOrStdout(), err, flags)
	}
	var env struct {
		Ads []json.RawMessage `json:"ads"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("parsing search response: %w", err)
	}
	if env.Ads == nil {
		env.Ads = make([]json.RawMessage, 0)
	}
	if db := openStoreOrWarn(cmd); db != nil {
		ads := make([]subito.Ad, 0, len(env.Ads))
		for _, raw := range env.Ads {
			if a, err := subito.ParseAd(raw); err == nil {
				ads = append(ads, a)
			}
		}
		recordAds(cmd, db, ads)
		_ = db.Close() // best-effort local cache; recordAds already warns on write errors
	}
	return printJSONFiltered(cmd.OutOrStdout(), env.Ads, flags)
}
