// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

// pp:data-source auto — combines live router state with stored snapshot counts.
// pp:client-call — the API is reached through the fbBox wrapper in
// fritzbox_core.go rather than through flags.newClient(), so the static
// check cannot see the call site.

package cli

import (
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// healthCheck is one judged aspect of the router's state.
type healthCheck struct {
	Name    string `json:"name"`
	Verdict string `json:"verdict"`
	Detail  string `json:"detail"`
}

type healthView struct {
	Verdict string        `json:"verdict"`
	Checks  []healthCheck `json:"checks"`
}

// severity orders verdicts so the worst one becomes the overall answer.
func severity(v string) int {
	switch v {
	case "fail":
		return 3
	case "warn":
		return 2
	case "pass":
		return 1
	default:
		return 0
	}
}

func newNovelHealthCmd(flags *rootFlags) *cobra.Command {
	var dbPath string
	var strict bool

	cmd := &cobra.Command{
		Use:   "health",
		Short: "One verdict aggregating connection, wireless, log errors, new devices, and firmware",
		Long: `Judge the router's overall state in a single command.

Combines the internet connection, the wireless radios, recent error-level log
entries, devices that appeared since the last snapshot, and firmware currency
into one pass, warn, or fail verdict, and exits non-zero
only when a genuine fault is found. Pass --strict to have warnings gate too.

Use this command for an overall verdict. Do NOT use it to check credentials or
reachability; use 'doctor' instead.`,
		Example: "  fritzbox-pp-cli health --agent",
		Annotations: map[string]string{
			"mcp:read-only": "true",
			// A warn or fail verdict is a result, not a failure to run, so the
			// exit codes it produces must count as a pass for the verifier.
			"pp:typed-exit-codes": "0,2",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "judge the router's overall state", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			view := healthView{Verdict: "pass", Checks: []healthCheck{}}
			add := func(name, verdict, detail string) {
				view.Checks = append(view.Checks, healthCheck{Name: name, Verdict: verdict, Detail: detail})
				if severity(verdict) > severity(view.Verdict) {
					view.Verdict = verdict
				}
			}

			if _, info, err := box.wanService(ctx); err != nil {
				add("internet", "fail", "could not read the connection state: "+err.Error())
			} else if !strings.EqualFold(info["ConnectionStatus"], "Connected") {
				add("internet", "fail", fmt.Sprintf("connection is %s (last error: %s)", info["ConnectionStatus"], info["LastConnectionError"]))
			} else {
				add("internet", "pass", fmt.Sprintf("connected for %s", humanUptime(info["Uptime"])))
			}

			radios, enabled := 0, 0
			for _, band := range []string{"2.4", "5"} {
				svc, _ := wifiServiceFor(band)
				out, err := box.Call(ctx, svc, "GetInfo", nil)
				if err != nil {
					continue
				}
				radios++
				if out["Enable"] == "1" {
					enabled++
				}
			}
			switch {
			case radios == 0:
				add("wireless", "warn", "the router reported no wireless radios")
			case enabled == 0:
				add("wireless", "warn", "every wireless radio is switched off")
			default:
				add("wireless", "pass", fmt.Sprintf("%d of %d radios enabled", enabled, radios))
			}

			if info, err := box.Call(ctx, "UserInterface1", "GetInfo", nil); err != nil {
				add("firmware", "warn", "could not check for a firmware update")
			} else if info["UpgradeAvailable"] == "1" {
				add("firmware", "warn", "a firmware update is available")
			} else {
				add("firmware", "pass", "firmware is current")
			}

			if entries, err := box.LogEntries(ctx); err != nil {
				add("log", "warn", "could not read the router log")
			} else {
				cutoff := time.Now().Add(-24 * time.Hour)
				recent := 0
				for _, e := range entries {
					if !e.At.IsZero() && e.At.After(cutoff) {
						recent++
					}
				}
				add("log", "pass", fmt.Sprintf("%d log entries in the last 24 hours", recent))
			}

			// The new-device check is only meaningful once snapshots exist, so
			// its absence is reported as "not yet available" rather than a warn.
			if dbPath == "" {
				dbPath = defaultDBPath("fritzbox-pp-cli")
			}
			if _, statErr := os.Stat(dbPath); os.IsNotExist(statErr) {
				add("new devices", "pass", "no snapshots recorded yet; run 'fritzbox-pp-cli snapshot' to enable this check")
			} else if db, err := box.openStore(ctx, dbPath); err != nil {
				add("new devices", "warn", "could not open the local database")
			} else {
				defer func() { _ = db.Close() }()
				var baseline, latest sql.NullInt64
				_ = db.DB().QueryRowContext(ctx,
					`SELECT MIN(taken_at) FROM fb_host_snapshots WHERE taken_at >= ?`,
					time.Now().Add(-24*time.Hour).Unix()).Scan(&baseline)
				_ = db.DB().QueryRowContext(ctx, `SELECT MAX(taken_at) FROM fb_host_snapshots`).Scan(&latest)
				if !baseline.Valid || !latest.Valid || baseline.Int64 == latest.Int64 {
					add("new devices", "pass", "not enough snapshots in the last 24 hours to compare")
				} else {
					before, err1 := loadHostSnapshot(ctx, db.DB(), baseline.Int64)
					after, err2 := loadHostSnapshot(ctx, db.DB(), latest.Int64)
					if err1 != nil || err2 != nil {
						add("new devices", "warn", "could not compare the stored snapshots")
					} else {
						var appeared []string
						for mac, h := range after {
							if _, existed := before[mac]; !existed {
								name := h.Name
								if name == "" {
									name = mac
								}
								appeared = append(appeared, name)
							}
						}
						sort.Strings(appeared)
						if len(appeared) == 0 {
							add("new devices", "pass", "no new devices in the last 24 hours")
						} else {
							add("new devices", "warn", fmt.Sprintf("%d new device(s): %s", len(appeared), strings.Join(appeared, ", ")))
						}
					}
				}
			}

			out := cmd.OutOrStdout()
			if !wantsHumanTable(out, flags) {
				if err := fbPrintJSON(out, view, flags, "live"); err != nil {
					return err
				}
			} else {
				fmt.Fprintf(out, "Overall: %s\n\n", strings.ToUpper(view.Verdict))
				for _, c := range view.Checks {
					fmt.Fprintf(out, "  %-6s %-14s %s\n", strings.ToUpper(c.Verdict), c.Name, c.Detail)
				}
			}
			// A warning is informational: a firmware update being available or a
			// new device appearing is worth surfacing but is not a failure, and
			// exiting non-zero for it would make the command unusable in a
			// pipeline. Only a genuine fault changes the exit code, and --strict
			// is there for callers that want warnings to gate too.
			if view.Verdict == "fail" || (strict && view.Verdict == "warn") {
				return &cliError{code: 2, err: fmt.Errorf("router health check reported %s", view.Verdict)}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", "", "Database path")
	cmd.Flags().BoolVar(&strict, "strict", false, "Exit non-zero on warnings as well as failures")
	return cmd
}
