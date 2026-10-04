// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

// pp:data-source local — compares two stored snapshots; no live call.

package cli

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"time"

	"fritzbox-pp-cli/internal/cliutil"

	"github.com/spf13/cobra"
)

// hostSnapshotRow is one stored observation of a device.
type hostSnapshotRow struct {
	Name   string
	IP     string
	Active bool
}

type hostsDiffView struct {
	Since    string           `json:"since"`
	Baseline string           `json:"baseline_taken_at"`
	Added    []map[string]any `json:"added"`
	Removed  []map[string]any `json:"removed"`
	Changed  []map[string]any `json:"changed"`
	Note     string           `json:"note,omitempty"`
}

func newNovelHostsDiffCmd(flags *rootFlags) *cobra.Command {
	var since string
	var dbPath string
	var newOnly bool

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "See which devices appeared, disappeared, or changed IP since a point in time",
		Long: `Compare the devices on the network now against the oldest snapshot recorded
within the given window.

Snapshots come from 'fritzbox-pp-cli snapshot'; at least two are needed before a
diff can report anything.

Use this command for what changed on the network. Do NOT use it to list current
devices; use 'hosts list' instead.`,
		Example:     "  fritzbox-pp-cli hosts diff --since 24h --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "compare the network against an earlier snapshot", map[string]any{"since": since})
			}
			window, err := cliutil.ParseDurationLoose(since)
			if err != nil {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--since %q is not a duration; use forms like 90m, 24h, 7d, or 2w", since))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()

			if dbPath == "" {
				dbPath = defaultDBPath("fritzbox-pp-cli")
			}
			if _, statErr := os.Stat(dbPath); os.IsNotExist(statErr) {
				fmt.Fprintf(cmd.ErrOrStderr(), "no local mirror at %s\nrun: fritzbox-pp-cli snapshot\n", dbPath)
				if !wantsHumanTable(cmd.OutOrStdout(), flags) {
					return printJSONFiltered(cmd.OutOrStdout(), hostsDiffView{
						Since: since, Added: []map[string]any{}, Removed: []map[string]any{}, Changed: []map[string]any{},
						Note: "no snapshots recorded yet; run 'fritzbox-pp-cli snapshot' twice with time in between",
					}, flags)
				}
				return nil
			}

			box, err := newBox(flags)
			if err != nil {
				return err
			}
			db, err := box.openStore(ctx, dbPath)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			cutoff := time.Now().Add(-window).Unix()
			// The baseline is the oldest snapshot inside the window: comparing
			// against the newest would diff two nearly identical observations
			// and report nothing regardless of what actually changed.
			var baselineAt sql.NullInt64
			if err := db.DB().QueryRowContext(ctx,
				`SELECT MIN(taken_at) FROM fb_host_snapshots WHERE taken_at >= ?`, cutoff).Scan(&baselineAt); err != nil {
				return fmt.Errorf("finding a baseline snapshot: %w", err)
			}
			var latestAt sql.NullInt64
			if err := db.DB().QueryRowContext(ctx, `SELECT MAX(taken_at) FROM fb_host_snapshots`).Scan(&latestAt); err != nil {
				return fmt.Errorf("finding the most recent snapshot: %w", err)
			}

			view := hostsDiffView{
				Since:   since,
				Added:   []map[string]any{},
				Removed: []map[string]any{},
				Changed: []map[string]any{},
			}
			if !baselineAt.Valid || !latestAt.Valid || baselineAt.Int64 == latestAt.Int64 {
				view.Note = "not enough snapshots inside the window to compare; run 'fritzbox-pp-cli snapshot' again after some time has passed"
				if !wantsHumanTable(cmd.OutOrStdout(), flags) {
					return printJSONFiltered(cmd.OutOrStdout(), view, flags)
				}
				fmt.Fprintln(cmd.OutOrStdout(), view.Note)
				return nil
			}
			view.Baseline = time.Unix(baselineAt.Int64, 0).Format(time.RFC3339)

			before, err := loadHostSnapshot(ctx, db.DB(), baselineAt.Int64)
			if err != nil {
				return err
			}
			after, err := loadHostSnapshot(ctx, db.DB(), latestAt.Int64)
			if err != nil {
				return err
			}

			for mac, now := range after {
				prev, existed := before[mac]
				if !existed {
					view.Added = append(view.Added, map[string]any{"mac": mac, "name": now.Name, "ip": now.IP, "online": now.Active})
					continue
				}
				if newOnly {
					continue
				}
				if prev.IP != now.IP || prev.Name != now.Name {
					view.Changed = append(view.Changed, map[string]any{
						"mac": mac, "name": now.Name,
						"previous_ip": prev.IP, "ip": now.IP,
						"previous_name": prev.Name,
					})
				}
			}
			if !newOnly {
				for mac, prev := range before {
					if _, still := after[mac]; !still {
						view.Removed = append(view.Removed, map[string]any{"mac": mac, "name": prev.Name, "ip": prev.IP})
					}
				}
			}
			sortRowsByKey(view.Added, "mac")
			sortRowsByKey(view.Removed, "mac")
			sortRowsByKey(view.Changed, "mac")

			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(), view, flags)
			}
			out := cmd.OutOrStdout()
			if len(view.Added)+len(view.Removed)+len(view.Changed) == 0 {
				fmt.Fprintf(out, "No device changes since %s.\n", view.Baseline)
				return nil
			}
			fmt.Fprintf(out, "Compared against the snapshot taken at %s\n\n", view.Baseline)
			printDiffSection(out, "Appeared", view.Added)
			printDiffSection(out, "Disappeared", view.Removed)
			printDiffSection(out, "Changed", view.Changed)
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "24h", "How far back to look for a baseline snapshot, for example 90m, 24h, 7d")
	cmd.Flags().StringVar(&dbPath, "db", "", "Database path")
	cmd.Flags().BoolVar(&newOnly, "new", false, "Only report devices that appeared")
	return cmd
}

func loadHostSnapshot(ctx context.Context, db *sql.DB, takenAt int64) (map[string]hostSnapshotRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT mac, name, ip, active FROM fb_host_snapshots WHERE taken_at = ?`, takenAt)
	if err != nil {
		return nil, fmt.Errorf("reading snapshot %d: %w", takenAt, err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]hostSnapshotRow{}
	for rows.Next() {
		// name and ip are declared NOT NULL DEFAULT '' but a database written
		// by an older build may still hold NULLs, so both are scanned safely.
		var mac string
		var name, ip sql.NullString
		var active sql.NullInt64
		if err := rows.Scan(&mac, &name, &ip, &active); err != nil {
			return nil, fmt.Errorf("reading a snapshot row: %w", err)
		}
		out[mac] = hostSnapshotRow{Name: name.String, IP: ip.String, Active: active.Int64 == 1}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating snapshot rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing snapshot rows: %w", err)
	}
	return out, nil
}

func sortRowsByKey(rows []map[string]any, key string) {
	sort.Slice(rows, func(i, j int) bool {
		return fmt.Sprint(rows[i][key]) < fmt.Sprint(rows[j][key])
	})
}

func printDiffSection(w interface{ Write([]byte) (int, error) }, title string, rows []map[string]any) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintf(w, "%s (%d)\n", title, len(rows))
	for _, r := range rows {
		fmt.Fprintf(w, "  %-24v %-16v %v\n", r["name"], r["ip"], r["mac"])
	}
	fmt.Fprintln(w)
}
