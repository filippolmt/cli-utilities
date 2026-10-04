// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source local

package cli

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"subito-pp-cli/internal/store"
	"subito-pp-cli/internal/subito"

	"github.com/spf13/cobra"
)

type historyEvent struct {
	SeenAt    string   `json:"seen_at"`
	Price     *float64 `json:"price"`
	Published string   `json:"published"`
	Change    string   `json:"change"`
}

type repost struct {
	ListID string   `json:"list_id"`
	Price  *float64 `json:"price"`
	URL    string   `json:"url"`
}

type historyView struct {
	ListID       string         `json:"list_id"`
	Found        bool           `json:"found"`
	Subject      string         `json:"subject,omitempty"`
	URL          string         `json:"url,omitempty"`
	FirstSeen    string         `json:"first_seen,omitempty"`
	DaysTracked  int            `json:"days_tracked"`
	FirstPrice   *float64       `json:"first_price,omitempty"`
	LastPrice    *float64       `json:"last_price,omitempty"`
	PriceChanges int            `json:"price_changes"`
	ChangePct    float64        `json:"price_change_pct"`
	Renewals     int            `json:"renewals"`
	Events       []historyEvent `json:"events"`
	Reposts      []repost       `json:"likely_reposts"`
	Note         string         `json:"note,omitempty"`
}

func newNovelAdsHistoryCmd(flags *rootFlags) *cobra.Command {
	var dbPath string
	cmd := &cobra.Command{
		Use:   "history [url-or-id]",
		Short: "See when an ad was first seen, every price change, renewals and likely reposts.",
		Long: strings.Trim(`
Use this command for one ad's price, renewal and repost history.
Do NOT use it for scam signals; use 'ads risk' instead.

History is built from what this CLI has seen: 'market deals|suggest|stats',
'ads risk', 'ads search --param' and 'watch run' record the listings they
return (a plain 'ads search' does not). A renewal is a
move of the publish date (Subito bumps it when the seller renews). A likely
repost is another listing by the same seller with the same normalized title.`, "\n"),
		Example: strings.Trim(`
  subito-pp-cli ads history 663258568
  subito-pp-cli ads history https://www.subito.it/biciclette/bici-da-corsa-milano-663258568.htm --agent`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "local", "pp:happy-args": "ref=663258568"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "ads history")
			}
			if err := rejectLiveSource(flags, "ads history"); err != nil {
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
			view := historyView{ListID: ref.ListID, Events: make([]historyEvent, 0), Reposts: make([]repost, 0)}
			notSeen := "not seen locally yet; run a search that returns it (market deals, watch run), then check again"
			if !dbFileExists(dbPath) {
				view.Note = notSeen
				return printHistory(cmd, flags, view)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			if dbPath == "" {
				dbPath = defaultDBPath("subito-pp-cli")
			}
			db, err := store.OpenReadOnlyContext(ctx, dbPath)
			if err != nil {
				return fmt.Errorf("opening local store: %w", err)
			}
			defer db.Close()
			// History is cumulative, so only the never-recorded case gets a
			// hint; a stale-age hint would point at a sync that cannot help.
			hintIfUnsynced(cmd, db, listingsResource)
			l, firstSeen, err := db.SubitoListing(ctx, ref.ListID)
			if errors.Is(err, sql.ErrNoRows) {
				view.Note = notSeen
				return printHistory(cmd, flags, view)
			}
			if err != nil {
				return err
			}
			obs, err := db.SubitoObservations(ctx, ref.ListID)
			if err != nil {
				return err
			}
			view.Found, view.Subject, view.URL, view.FirstSeen = true, l.Subject, l.URL, firstSeen
			if t, err := time.Parse(time.RFC3339, firstSeen); err == nil {
				view.DaysTracked = int(time.Since(t).Hours() / 24)
			}
			for i, o := range obs {
				ev := historyEvent{SeenAt: o.SeenAt, Price: nullPtr(o.Price), Published: o.Published, Change: "first_seen"}
				if i > 0 {
					prev := obs[i-1]
					var changes []string
					if prev.Price != o.Price {
						changes = append(changes, "price")
						view.PriceChanges++
					}
					if prev.Published != o.Published {
						changes = append(changes, "renewed")
						view.Renewals++
					}
					ev.Change = strings.Join(changes, "+")
				}
				view.Events = append(view.Events, ev)
			}
			if len(obs) > 0 {
				view.FirstPrice, view.LastPrice = nullPtr(obs[0].Price), nullPtr(obs[len(obs)-1].Price)
				if view.FirstPrice != nil && view.LastPrice != nil && *view.FirstPrice > 0 {
					view.ChangePct = subito.GapPct(*view.LastPrice, *view.FirstPrice)
				}
			}
			if l.UserID != "" {
				others, err := db.SubitoSellerListings(ctx, l.UserID, l.ListID)
				if err != nil {
					return err
				}
				for _, o := range others {
					if l.SubjectNorm != "" && o.SubjectNorm == l.SubjectNorm {
						view.Reposts = append(view.Reposts, repost{ListID: o.ListID, Price: nullPtr(o.Price), URL: o.URL})
					}
				}
			}
			if len(obs) == 1 {
				view.Note = "seen once so far; history grows each time a search returns this ad"
			}
			return printHistory(cmd, flags, view)
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", "", "Database path (default: the CLI's local store)")
	return cmd
}

func nullPtr(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}

func printHistory(cmd *cobra.Command, flags *rootFlags, v historyView) error {
	if !wantsHumanTable(cmd.OutOrStdout(), flags) {
		return printJSONFiltered(cmd.OutOrStdout(), v, flags)
	}
	out := cmd.OutOrStdout()
	if !v.Found {
		fmt.Fprintf(out, "%s: %s\n", v.ListID, v.Note)
		return nil
	}
	fmt.Fprintf(out, "%s\n%s\nfirst seen %s · %d price change(s) · %d renewal(s)\n\n", cell(v.Subject), v.URL, v.FirstSeen, v.PriceChanges, v.Renewals)
	tw := newTabWriter(out)
	fmt.Fprintln(tw, "SEEN\tPRICE\tPUBLISHED\tCHANGE")
	for _, e := range v.Events {
		price := "-"
		if e.Price != nil {
			price = fmt.Sprintf("%.0f", *e.Price)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", e.SeenAt, price, e.Published, e.Change)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	for _, r := range v.Reposts {
		fmt.Fprintf(out, "likely repost: %s %s\n", r.ListID, r.URL)
	}
	if v.Note != "" {
		fmt.Fprintln(out, "\n"+v.Note)
	}
	return nil
}
