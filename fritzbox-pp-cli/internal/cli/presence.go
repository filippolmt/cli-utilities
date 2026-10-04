// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

// pp:data-source auto — reads the live device list and stored last-seen times.
// pp:client-call — the API is reached through the fbBox wrapper in
// fritzbox_core.go rather than through flags.newClient(), so the static
// check cannot see the call site.

package cli

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type presenceRow struct {
	Person   string   `json:"person"`
	Home     bool     `json:"home"`
	Devices  []string `json:"devices"`
	LastSeen string   `json:"last_seen,omitempty"`
}

func newNovelPresenceCmd(flags *rootFlags) *cobra.Command {
	var addPerson, addMAC, forgetPerson string
	var dbPath string

	cmd := &cobra.Command{
		Use:   "presence",
		Short: "Report who is home by mapping people to their devices",
		Long: `Report who is home.

The router models MAC addresses, not people, so the mapping is kept locally.
Register a device with --person and --device, then run the command with no flags
to see who is in.

Use this command for who is home. Do NOT use it for a raw device inventory; use
'hosts list' instead.`,
		Example: "  fritzbox-pp-cli presence --agent",
		// Not read-only: --person/--device registers a mapping and --forget
		// deletes one, both against the local store. Marking the tool read-only
		// would let an MCP host run the forget path without prompting.
		Annotations: map[string]string{"mcp:local-write": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "report household presence", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			if dbPath == "" {
				dbPath = defaultDBPath("fritzbox-pp-cli")
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

			switch {
			case forgetPerson != "":
				res, err := db.DB().ExecContext(ctx, `DELETE FROM fb_people WHERE person = ?`, forgetPerson)
				if err != nil {
					return fmt.Errorf("forgetting %s: %w", forgetPerson, err)
				}
				n, _ := res.RowsAffected()
				return fbEmitObject(cmd, flags, map[string]any{"person": forgetPerson, "devices_removed": n})
			case addPerson != "" || addMAC != "":
				if addPerson == "" || addMAC == "" {
					_ = cmd.Usage()
					return usageErr(fmt.Errorf("--person and --device must be given together"))
				}
				// The device may be named rather than addressed, so it is
				// resolved against the router's list before being stored.
				hosts, err := box.Hosts(ctx)
				if err != nil {
					return err
				}
				host, err := findHost(hosts, addMAC)
				if err != nil {
					return err
				}
				if _, err := db.DB().ExecContext(ctx,
					`INSERT OR REPLACE INTO fb_people(person, mac) VALUES (?, ?)`,
					addPerson, strings.ToUpper(host.MAC)); err != nil {
					return fmt.Errorf("registering the device: %w", err)
				}
				return fbEmitObject(cmd, flags, map[string]any{
					"person": addPerson, "device": host.Name, "mac": host.MAC, "registered": true,
				})
			}

			people, err := loadPeople(ctx, db.DB())
			if err != nil {
				return err
			}
			if len(people) == 0 {
				out := cmd.OutOrStdout()
				if !wantsHumanTable(out, flags) {
					// An empty array on its own cannot be told apart from
					// "everyone is out", which is the same shape and a very
					// different answer. The note carries the setup step the
					// human branch below prints, so an agent or a piped caller
					// gets it too.
					return fbEmitObject(cmd, flags, map[string]any{
						"people": []presenceRow{},
						"note":   `no people are registered yet; register one with 'fritzbox-pp-cli presence --person Alice --device "Alice phone"'`,
					})
				}
				fmt.Fprintln(out, "No people are registered yet.")
				fmt.Fprintln(out, "Register one with: fritzbox-pp-cli presence --person Alice --device \"Alice's phone\"")
				return nil
			}

			hosts, err := box.Hosts(ctx)
			if err != nil {
				return err
			}
			online := map[string]fbHost{}
			for _, h := range hosts {
				online[strings.ToUpper(h.MAC)] = h
			}
			lastSeen := loadLastSeen(ctx, db.DB())

			rows := make([]presenceRow, 0, len(people))
			for person, macs := range people {
				row := presenceRow{Person: person, Devices: []string{}}
				var newest int64
				for _, mac := range macs {
					if h, ok := online[mac]; ok {
						row.Devices = append(row.Devices, h.Name)
						if h.Online() {
							row.Home = true
						}
					} else {
						row.Devices = append(row.Devices, mac)
					}
					if ts, ok := lastSeen[mac]; ok && ts > newest {
						newest = ts
					}
				}
				sort.Strings(row.Devices)
				if newest > 0 {
					row.LastSeen = time.Unix(newest, 0).Format(time.RFC3339)
				}
				rows = append(rows, row)
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].Person < rows[j].Person })

			out := cmd.OutOrStdout()
			if !wantsHumanTable(out, flags) {
				return fbPrintJSON(out, rows, flags, "live")
			}
			for _, r := range rows {
				state := "away"
				if r.Home {
					state = "home"
				}
				fmt.Fprintf(out, "%-20s %-6s %s\n", r.Person, state, strings.Join(r.Devices, ", "))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&addPerson, "person", "", "Person to register a device for")
	cmd.Flags().StringVar(&addMAC, "device", "", "Device name, IP address, or MAC address belonging to that person")
	cmd.Flags().StringVar(&forgetPerson, "forget", "", "Remove every device registered to this person")
	cmd.Flags().StringVar(&dbPath, "db", "", "Database path")
	return cmd
}

func loadPeople(ctx context.Context, db *sql.DB) (map[string][]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT person, mac FROM fb_people ORDER BY person, mac`)
	if err != nil {
		return nil, fmt.Errorf("reading the people table: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string][]string{}
	for rows.Next() {
		var person, mac string
		if err := rows.Scan(&person, &mac); err != nil {
			return nil, fmt.Errorf("reading a people row: %w", err)
		}
		out[person] = append(out[person], mac)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating people rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing people rows: %w", err)
	}
	return out, nil
}

// loadLastSeen returns the newest snapshot time each device was seen online.
// A missing history table is not an error: presence still works from the live
// device list, just without a last-seen column.
func loadLastSeen(ctx context.Context, db *sql.DB) map[string]int64 {
	rows, err := db.QueryContext(ctx,
		`SELECT UPPER(mac), MAX(taken_at) FROM fb_host_snapshots WHERE active = 1 GROUP BY UPPER(mac)`)
	if err != nil {
		return map[string]int64{}
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int64{}
	for rows.Next() {
		var mac string
		var at sql.NullInt64
		if err := rows.Scan(&mac, &at); err != nil {
			continue
		}
		out[mac] = at.Int64
	}
	_ = rows.Err()
	return out
}
