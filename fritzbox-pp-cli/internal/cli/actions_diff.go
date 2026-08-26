// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

// pp:data-source local — compares two stored catalog snapshots.

package cli

import (
	"fmt"
	"sort"
	"time"

	"github.com/spf13/cobra"
)

type actionsDiffView struct {
	From    string   `json:"from"`
	To      string   `json:"to"`
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
	Changed []string `json:"changed"`
	Note    string   `json:"note,omitempty"`
}

func newNovelActionsDiffCmd(flags *rootFlags) *cobra.Command {
	var refresh bool
	var dbPath string

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Show which TR-064 actions a firmware update added or removed",
		Long: `Compare the two most recent snapshots of the router's action catalog.

AVM publishes no changelog at action granularity, so the only way to find out
what a firmware update actually changed is to have recorded the catalog before
it. Take a snapshot with 'actions search --refresh' or 'actions diff --refresh'
before and after an update.`,
		Example:     "  fritzbox-pp-cli actions diff --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "compare two action-catalog snapshots", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			db, err := box.openStore(ctx, dbPath)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			if refresh {
				if _, _, err := box.syncActionCatalog(ctx, db); err != nil {
					return err
				}
			}
			snapshots, err := catalogSnapshots(ctx, db)
			if err != nil {
				return err
			}
			view := actionsDiffView{Added: []string{}, Removed: []string{}, Changed: []string{}}
			if len(snapshots) < 2 {
				view.Note = "fewer than two catalog snapshots recorded; run 'fritzbox-pp-cli actions diff --refresh' now and again after the next firmware update"
				if !wantsHumanTable(cmd.OutOrStdout(), flags) {
					return printJSONFiltered(cmd.OutOrStdout(), view, flags)
				}
				fmt.Fprintln(cmd.OutOrStdout(), view.Note)
				return nil
			}

			newer, older := snapshots[0], snapshots[1]
			view.To = time.Unix(newer, 0).Format(time.RFC3339)
			view.From = time.Unix(older, 0).Format(time.RFC3339)

			before, err := loadCatalog(ctx, db, older)
			if err != nil {
				return err
			}
			after, err := loadCatalog(ctx, db, newer)
			if err != nil {
				return err
			}
			for key, row := range after {
				prev, existed := before[key]
				if !existed {
					view.Added = append(view.Added, key)
					continue
				}
				// An action can keep its name while gaining or losing
				// arguments, which is exactly the kind of change a caller
				// needs to know about.
				if prev.In != row.In || prev.Out != row.Out {
					view.Changed = append(view.Changed, key)
				}
			}
			for key := range before {
				if _, still := after[key]; !still {
					view.Removed = append(view.Removed, key)
				}
			}
			sort.Strings(view.Added)
			sort.Strings(view.Removed)
			sort.Strings(view.Changed)

			out := cmd.OutOrStdout()
			if !wantsHumanTable(out, flags) {
				return printJSONFiltered(out, view, flags)
			}
			if len(view.Added)+len(view.Removed)+len(view.Changed) == 0 {
				fmt.Fprintf(out, "No catalog changes between %s and %s.\n", view.From, view.To)
				return nil
			}
			fmt.Fprintf(out, "Comparing %s -> %s\n\n", view.From, view.To)
			for title, list := range map[string][]string{"Added": view.Added, "Removed": view.Removed, "Changed": view.Changed} {
				if len(list) == 0 {
					continue
				}
				fmt.Fprintf(out, "%s (%d)\n", title, len(list))
				for _, k := range list {
					fmt.Fprintf(out, "  %s\n", k)
				}
				fmt.Fprintln(out)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "Record a fresh catalog snapshot before comparing")
	cmd.Flags().StringVar(&dbPath, "db", "", "Database path")
	return cmd
}
