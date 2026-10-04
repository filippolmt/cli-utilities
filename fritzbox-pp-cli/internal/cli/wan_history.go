// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

// pp:data-source local — reconstructed entirely from stored uptime samples.

package cli

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

type wanDrop struct {
	At             string `json:"at"`
	PreviousUptime int64  `json:"previous_uptime_seconds"`
	PreviousIP     string `json:"previous_ip"`
	NewIP          string `json:"new_ip"`
	LastError      string `json:"last_error,omitempty"`
}

type wanHistoryView struct {
	Days            int       `json:"days"`
	SamplesExamined int       `json:"samples_examined"`
	Drops           []wanDrop `json:"drops"`
	IPChanges       []wanDrop `json:"ip_changes"`
	CurrentUptime   int64     `json:"current_uptime_seconds"`
	Note            string    `json:"note,omitempty"`
}

func newNovelWanHistoryCmd(flags *rootFlags) *cobra.Command {
	var days int
	var dbPath string
	var ipOnly bool

	cmd := &cobra.Command{
		Use:   "history",
		Short: "Show how often the internet connection dropped and when",
		Long: `Reconstruct the internet connection's drop history from stored uptime samples.

The router reports how long the current connection has been up. When that
counter goes down between two samples, the connection was re-established in
between, which is what marks a drop. Samples come from
'fritzbox-pp-cli snapshot'.

Use this command for line stability over time. Do NOT use it for the current
connection state; use 'wan status' instead.`,
		Example:     "  fritzbox-pp-cli wan history --days 7 --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "reconstruct the connection drop history", map[string]any{"days": days})
			}
			if days <= 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--days must be greater than zero"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			if dbPath == "" {
				dbPath = defaultDBPath("fritzbox-pp-cli")
			}
			view := wanHistoryView{Days: days, Drops: []wanDrop{}, IPChanges: []wanDrop{}}
			if _, statErr := os.Stat(dbPath); os.IsNotExist(statErr) {
				fmt.Fprintf(cmd.ErrOrStderr(), "no local mirror at %s\nrun: fritzbox-pp-cli snapshot\n", dbPath)
				view.Note = "no samples recorded yet; run 'fritzbox-pp-cli snapshot' on a schedule to build history"
				if !wantsHumanTable(cmd.OutOrStdout(), flags) {
					return printJSONFiltered(cmd.OutOrStdout(), view, flags)
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

			cutoff := time.Now().AddDate(0, 0, -days).Unix()
			rows, err := db.DB().QueryContext(ctx,
				`SELECT taken_at, uptime, external_ip, last_error FROM fb_wan_samples WHERE taken_at >= ? ORDER BY taken_at ASC`, cutoff)
			if err != nil {
				return fmt.Errorf("reading the connection samples: %w", err)
			}
			defer func() { _ = rows.Close() }()
			type sample struct {
				at        int64
				uptime    int64
				ip        string
				lastError string
			}
			var samples []sample
			for rows.Next() {
				var s sample
				var ip, lastErr sql.NullString
				var uptime sql.NullInt64
				if err := rows.Scan(&s.at, &uptime, &ip, &lastErr); err != nil {
					return fmt.Errorf("reading a connection sample: %w", err)
				}
				s.uptime, s.ip, s.lastError = uptime.Int64, ip.String, lastErr.String
				samples = append(samples, s)
			}
			if err := rows.Err(); err != nil {
				return fmt.Errorf("iterating connection samples: %w", err)
			}
			if err := rows.Close(); err != nil {
				return fmt.Errorf("closing connection samples: %w", err)
			}

			view.SamplesExamined = len(samples)
			if len(samples) > 0 {
				view.CurrentUptime = samples[len(samples)-1].uptime
			}
			for i := 1; i < len(samples); i++ {
				prev, cur := samples[i-1], samples[i]
				// A drop is an uptime counter that went backwards. Comparing
				// against elapsed wall-clock time instead would misreport every
				// snapshot gap as a drop.
				if cur.uptime < prev.uptime {
					view.Drops = append(view.Drops, wanDrop{
						At:             time.Unix(cur.at, 0).Format(time.RFC3339),
						PreviousUptime: prev.uptime,
						PreviousIP:     prev.ip,
						NewIP:          cur.ip,
						LastError:      cur.lastError,
					})
				}
				if prev.ip != cur.ip && prev.ip != "" && cur.ip != "" {
					view.IPChanges = append(view.IPChanges, wanDrop{
						At: time.Unix(cur.at, 0).Format(time.RFC3339), PreviousIP: prev.ip, NewIP: cur.ip,
					})
				}
			}
			if len(samples) < 2 {
				view.Note = "fewer than two samples in the window; run 'fritzbox-pp-cli snapshot' again after some time has passed"
			}

			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				if ipOnly {
					return printJSONFiltered(cmd.OutOrStdout(), view.IPChanges, flags)
				}
				return printJSONFiltered(cmd.OutOrStdout(), view, flags)
			}
			out := cmd.OutOrStdout()
			if view.Note != "" {
				fmt.Fprintln(out, view.Note)
				return nil
			}
			fmt.Fprintf(out, "Examined %d samples over the last %d day(s).\n", view.SamplesExamined, days)
			fmt.Fprintf(out, "Current connection uptime: %s\n\n", humanUptime(fmt.Sprint(view.CurrentUptime)))
			if !ipOnly {
				if len(view.Drops) == 0 {
					fmt.Fprintln(out, "No connection drops recorded.")
				} else {
					fmt.Fprintf(out, "Drops (%d)\n", len(view.Drops))
					for _, d := range view.Drops {
						fmt.Fprintf(out, "  %s  after %s up\n", d.At, humanUptime(fmt.Sprint(d.PreviousUptime)))
					}
				}
				fmt.Fprintln(out)
			}
			if len(view.IPChanges) == 0 {
				fmt.Fprintln(out, "No external address changes recorded.")
				return nil
			}
			fmt.Fprintf(out, "External address changes (%d)\n", len(view.IPChanges))
			for _, c := range view.IPChanges {
				fmt.Fprintf(out, "  %s  %s -> %s\n", c.At, c.PreviousIP, c.NewIP)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&days, "days", 7, "How many days of history to examine")
	cmd.Flags().StringVar(&dbPath, "db", "", "Database path")
	cmd.Flags().BoolVar(&ipOnly, "ip", false, "Only report external address changes")
	return cmd
}
