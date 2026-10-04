// Hand-written: watch, saved searches that report only what is new.
// pp:data-source live

package cli

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"subito-pp-cli/internal/store"
	"subito-pp-cli/internal/subito"

	"github.com/spf13/cobra"
)

// exitNewListings is returned by `watch run --exit-code` when something new
// turned up, so a cron wrapper can branch on it.
const exitNewListings = 6

var watchNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,40}$`)

type watchChange struct {
	Watch     string   `json:"watch"`
	Change    string   `json:"change"`
	ListID    string   `json:"list_id"`
	Subject   string   `json:"subject"`
	Price     *float64 `json:"price"`
	OldPrice  *float64 `json:"old_price,omitempty"`
	Town      string   `json:"town"`
	Seller    string   `json:"seller_segment"`
	Published string   `json:"published"`
	URL       string   `json:"url"`
}

type watchRunSummary struct {
	Watch    string `json:"watch"`
	Scanned  int    `json:"scanned_ads"`
	New      int    `json:"new"`
	Changed  int    `json:"price_changes"`
	Baseline bool   `json:"baseline,omitempty"`
	Error    string `json:"error,omitempty"`
}

type watchRunView struct {
	Changes []watchChange     `json:"changes"`
	Watches []watchRunSummary `json:"watches"`
	Note    string            `json:"note,omitempty"`
}

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		addNovelCommandIfAbsent(root, newWatchCmd(flags))
	})
}

func newWatchCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Saved searches that report only new listings and price changes",
		Long: strings.Trim(`
Save a search once with 'watch add', then run 'watch run' from cron or a
loop: it prints only listings it has not reported before, plus price
changes on ones it has. The first run of a new watch records the current
listings as the baseline and reports nothing.`, "\n"),
		Example: strings.Trim(`
  subito-pp-cli watch add bici --query "bici da corsa" --category bici --region lombardia --price-max 800
  subito-pp-cli watch run --agent`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newWatchAddCmd(flags), newWatchListCmd(flags), newWatchRunCmd(flags), newWatchRmCmd(flags))
	return cmd
}

func newWatchAddCmd(flags *rootFlags) *cobra.Command {
	var o searchOpts
	cmd := &cobra.Command{
		Use:   "add [name]",
		Short: "Save a search under a name; place and category names are resolved once, now",
		Example: strings.Trim(`
  subito-pp-cli watch add tracer --query "tracer 9 gt" --category moto --price-max 11000
  subito-pp-cli watch add iphone --query "iphone 15 128" --category telefonia --shippable --advertiser private`, "\n"),
		Annotations: map[string]string{"mcp:local-write": "true", "pp:data-source": "live", "pp:happy-args": "name=pp-dogfood;--query=iphone 15;--category-id=12"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "watch add")
			}
			if len(args) != 1 || !watchNameRe.MatchString(args[0]) {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("pass one watch name: lowercase letters, digits, - or _ (e.g. bici-corsa)"))
			}
			if err := rejectLocalSource(flags, "watch add"); err != nil {
				return err
			}
			if o.query == "" && o.category == "" && len(o.params) == 0 {
				return usageErr(fmt.Errorf("give the watch something to search: --query, --category or --param"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			params, err := o.buildParams(ctx, c)
			if err != nil {
				return err
			}
			db, err := openSubitoStore(ctx, "")
			if err != nil {
				return fmt.Errorf("opening local store: %w", err)
			}
			defer func() { _ = db.Close() }()
			w := store.SubitoWatch{Name: args[0], Params: params, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
			if err := db.SaveSubitoWatch(ctx, w); err != nil {
				return err
			}
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(), w, flags)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "saved watch %q: %s\nrun it with: subito-pp-cli watch run %s\n", w.Name, formatParams(params), w.Name)
			return nil
		},
	}
	addSearchFlags(cmd, &o)
	return cmd
}

func formatParams(p map[string]string) string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+p[k])
	}
	return strings.Join(parts, " ")
}

func newWatchListCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "list",
		Short:       "List saved watches with their search parameters and last run",
		Example:     "  subito-pp-cli watch list --json",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "local"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "watch list")
			}
			if err := rejectLiveSource(flags, "watch list"); err != nil {
				return err
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			watches := make([]store.SubitoWatch, 0)
			db, err := openStoreForRead(ctx, "subito-pp-cli")
			if err != nil {
				return fmt.Errorf("opening local store: %w", err)
			}
			if db != nil {
				defer func() { _ = db.Close() }()
				if watches, err = db.SubitoWatches(ctx, ""); err != nil {
					return err
				}
			}
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(), watches, flags)
			}
			if len(watches) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No watches yet. Add one with: subito-pp-cli watch add <name> --query ...")
				return nil
			}
			tw := newTabWriter(cmd.OutOrStdout())
			fmt.Fprintln(tw, "NAME\tLAST RUN\tSEARCH")
			for _, w := range watches {
				last := w.LastRunAt
				if last == "" {
					last = "never"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\n", w.Name, last, formatParams(w.Params))
			}
			return tw.Flush()
		},
	}
	return cmd
}

func newWatchRmCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "rm [name]",
		Short:       "Delete a saved watch and what it has already reported",
		Example:     "  subito-pp-cli watch rm bici",
		Annotations: map[string]string{"mcp:local-write": "true", "pp:data-source": "local", "pp:happy-args": "name=pp-dogfood"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "watch rm")
			}
			if len(args) != 1 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("pass the watch name to delete"))
			}
			if err := rejectLiveSource(flags, "watch rm"); err != nil {
				return err
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			removed := false
			if dbFileExists("") {
				db, err := openSubitoStore(ctx, "")
				if err != nil {
					return fmt.Errorf("opening local store: %w", err)
				}
				defer func() { _ = db.Close() }()
				if removed, err = db.DeleteSubitoWatch(ctx, args[0]); err != nil {
					return err
				}
			}
			if !removed {
				return notFoundErr(fmt.Errorf("no watch named %q (see 'watch list')", args[0]))
			}
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(), map[string]any{"removed": args[0]}, flags)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed watch %q\n", args[0])
			return nil
		},
	}
	return cmd
}

func newWatchRunCmd(flags *rootFlags) *cobra.Command {
	var maxPages int
	var exitCode bool
	var includeExisting bool
	cmd := &cobra.Command{
		Use:   "run [name]",
		Short: "Run every watch (or one) and print only new listings and price changes",
		Long: strings.Trim(`
Runs each saved watch newest first and prints only listings the watch has
not reported yet, plus price changes on listings it has. The first run of
a watch is a baseline: it records what is already listed and prints
nothing, unless --include-existing. Exit codes: 5 when any watch failed
(its error is in the output; the others still report), else with
--exit-code 6 when anything new or changed turned up, else 0.`, "\n"),
		Example: strings.Trim(`
  subito-pp-cli watch run
  subito-pp-cli watch run bici --agent --exit-code`, "\n"),
		Annotations: map[string]string{"mcp:local-write": "true", "pp:data-source": "live", "pp:typed-exit-codes": "0,5,6"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "watch run")
			}
			if err := rejectLocalSource(flags, "watch run"); err != nil {
				return err
			}
			if maxPages < 1 {
				return usageErr(fmt.Errorf("--max-pages must be at least 1"))
			}
			if len(args) > 1 {
				return usageErr(fmt.Errorf("pass at most one watch name"))
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			view := watchRunView{Changes: make([]watchChange, 0), Watches: make([]watchRunSummary, 0)}
			if !dbFileExists("") {
				if name != "" {
					return notFoundErr(fmt.Errorf("no watch named %q (see 'watch list')", name))
				}
				view.Note = "no watches yet; add one with 'watch add'"
				return printWatchRun(cmd, flags, view)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			db, err := openSubitoStore(ctx, "")
			if err != nil {
				return fmt.Errorf("opening local store: %w", err)
			}
			defer func() { _ = db.Close() }()
			watches, err := db.SubitoWatches(ctx, name)
			if err != nil {
				return err
			}
			if name != "" && len(watches) == 0 {
				return notFoundErr(fmt.Errorf("no watch named %q (see 'watch list')", name))
			}
			if len(watches) == 0 {
				view.Note = "no watches yet; add one with 'watch add'"
				return printWatchRun(cmd, flags, view)
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			c.NoCache = true // a watch must see the listings of this minute, not a cached page
			type pendingMark struct {
				watch string
				seen  map[string]sql.NullFloat64
			}
			marks := make([]pendingMark, 0, len(watches))
			failed := 0
			fail := func(sum watchRunSummary, err error) {
				// One broken watch must not hide the others' results.
				sum.Error = err.Error()
				view.Watches = append(view.Watches, sum)
				failed++
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: watch %s failed: %v\n", sum.Watch, err)
			}
			for i, w := range watches {
				sum := watchRunSummary{Watch: w.Name, Baseline: w.LastRunAt == "" && !includeExisting}
				if i > 0 {
					if err := sleepCtx(ctx, pageDelay); err != nil {
						fail(sum, err)
						continue
					}
				}
				res, err := fetchAds(ctx, c, w.Params, maxPages, 100)
				if err != nil {
					fail(sum, err)
					continue
				}
				recordAds(cmd, db, res.Ads)
				seen, err := db.SubitoWatchSeenSet(ctx, w.Name)
				if err != nil {
					fail(sum, err)
					continue
				}
				sum.Scanned = len(res.Ads)
				mark := make(map[string]sql.NullFloat64, len(res.Ads))
				for _, a := range res.Ads {
					if _, dup := mark[a.ListID]; a.ListID == "" || dup {
						continue // a listing can shift onto the next page mid-run
					}
					price := sql.NullFloat64{}
					if subito.Priced(a) {
						price = sql.NullFloat64{Float64: a.Price, Valid: true}
					}
					mark[a.ListID] = price
					prev, known := seen[a.ListID]
					change := ""
					switch {
					case !known:
						change = "new"
					case price.Valid && prev.Valid && price.Float64 < prev.Float64:
						change = "price_drop"
					case prev != price:
						change = "price_change"
					}
					if change == "" || sum.Baseline {
						continue
					}
					if change == "new" {
						sum.New++
					} else {
						sum.Changed++
					}
					view.Changes = append(view.Changes, watchChange{Watch: w.Name, Change: change, ListID: a.ListID, Subject: a.Subject,
						Price: nullPtr(price), OldPrice: oldPrice(change, prev), Town: a.Town, Seller: subito.Segment(a), Published: a.Published, URL: a.URL})
				}
				marks = append(marks, pendingMark{watch: w.Name, seen: mark})
				view.Watches = append(view.Watches, sum)
			}
			if err := printWatchRun(cmd, flags, view); err != nil {
				return err
			}
			// Mark listings as reported only after they were printed: a crash
			// before this point reports them again next run instead of never.
			// Detached from --timeout: what was printed must be recorded even
			// when the fetches used up the deadline.
			markCtx := context.WithoutCancel(ctx)
			var markErr error
			for _, m := range marks {
				if err := db.MarkSubitoWatchSeen(markCtx, m.watch, m.seen, time.Now()); err != nil && markErr == nil {
					markErr = fmt.Errorf("recording what watch %s reported: %w", m.watch, err)
				}
			}
			if markErr != nil {
				return markErr
			}
			if failed > 0 {
				return apiErr(fmt.Errorf("%d of %d watch(es) failed; see the warnings above", failed, len(watches)))
			}
			if exitCode && len(view.Changes) > 0 {
				return &cliError{code: exitNewListings, err: fmt.Errorf("%d new or changed listing(s)", len(view.Changes))}
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&maxPages, "max-pages", 1, "Pages of 100 newest listings to check per watch")
	cmd.Flags().BoolVar(&exitCode, "exit-code", false, "Exit 6 when anything new or changed turned up")
	cmd.Flags().BoolVar(&includeExisting, "include-existing", false, "On a watch's first run, report current listings instead of recording them silently")
	return cmd
}

func oldPrice(change string, p sql.NullFloat64) *float64 {
	if change == "new" {
		return nil
	}
	return nullPtr(p)
}

func printWatchRun(cmd *cobra.Command, flags *rootFlags, v watchRunView) error {
	if !wantsHumanTable(cmd.OutOrStdout(), flags) {
		return printJSONFiltered(cmd.OutOrStdout(), v, flags)
	}
	out := cmd.OutOrStdout()
	for _, s := range v.Watches {
		switch {
		case s.Error != "":
			fmt.Fprintf(out, "%s: failed: %s\n", s.Watch, s.Error)
		case s.Baseline:
			fmt.Fprintf(out, "%s: baseline recorded (%d listings); next run reports only new ones\n", s.Watch, s.Scanned)
		default:
			fmt.Fprintf(out, "%s: %d new, %d price change(s) in %d listings checked\n", s.Watch, s.New, s.Changed, s.Scanned)
		}
	}
	if v.Note != "" {
		fmt.Fprintln(out, v.Note)
	}
	if len(v.Changes) == 0 {
		return nil
	}
	fmt.Fprintln(out)
	tw := newTabWriter(out)
	fmt.Fprintln(tw, "WATCH\tCHANGE\tPRICE\tWAS\tTOWN\tTITLE\tURL")
	for _, c := range v.Changes {
		price, was := "-", ""
		if c.Price != nil {
			price = fmt.Sprintf("%.0f", *c.Price)
		}
		if c.OldPrice != nil {
			was = fmt.Sprintf("%.0f", *c.OldPrice)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", c.Watch, c.Change, price, was, cell(c.Town), cell(truncate(c.Subject, 50)), c.URL)
	}
	return tw.Flush()
}
