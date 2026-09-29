// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"fritzbox-pp-cli/internal/store"

	"github.com/spf13/cobra"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		if parent := fbFind(root, "portmap"); parent != nil {
			fbAttach(parent, newPortmapListCmd(flags))
			fbAttach(parent, newPortmapAddCmd(flags))
			fbAttach(parent, newPortmapDeleteCmd(flags))
		}
		if parent := fbFind(root, "log"); parent != nil {
			fbAttach(parent, newLogTailCmd(flags))
		}
		fbAttach(root, newVPNCmd(flags))
		fbAttach(root, newSnapshotCmd(flags))
		fbAttach(root, newMetricsCmd(flags))
	})
}

// --------------------------------------------------------------- portmap ----

// fbPortMapping is one forwarding rule.
type fbPortMapping struct {
	Index        int    `json:"index"`
	Description  string `json:"description"`
	Protocol     string `json:"protocol"`
	ExternalPort string `json:"external_port"`
	InternalHost string `json:"internal_host"`
	InternalPort string `json:"internal_port"`
	Enabled      bool   `json:"enabled"`
	// Source is "tr064" for a rule in the TR-064 table (deletable by index),
	// "upnp" for one a device opened itself, "exposed-host" for a DMZ host.
	Source string `json:"source"`
}

func (p fbPortMapping) row() map[string]any {
	return map[string]any{
		"index": p.Index, "description": p.Description, "protocol": p.Protocol,
		"external_port": p.ExternalPort, "internal_host": p.InternalHost,
		"internal_port": p.InternalPort, "enabled": p.Enabled, "source": p.Source,
	}
}

// PortMappings lists every open port: the TR-064 forwarding table plus the
// per-device sharing and UPnP/PCP ports from the portoverview page.
//
// There is no list action: entries are read one index at a time until the
// router reports the index is invalid, which is the documented way to
// enumerate them.
func (b *fbBox) PortMappings(ctx context.Context) ([]fbPortMapping, error) {
	svc, _, err := b.wanService(ctx)
	if err != nil {
		return nil, err
	}
	countOut, err := b.Call(ctx, svc, "GetPortMappingNumberOfEntries", nil)
	if err != nil {
		return nil, err
	}
	count, _ := strconv.Atoi(countOut["PortMappingNumberOfEntries"])
	mappings := make([]fbPortMapping, 0, count)
	for i := 0; i < count; i++ {
		out, err := b.Call(ctx, svc, "GetGenericPortMappingEntry", map[string]string{
			"PortMappingIndex": strconv.Itoa(i),
		})
		if err != nil {
			// The table can shrink while it is being walked; stopping at the
			// first invalid index is correct rather than failing the command.
			break
		}
		mappings = append(mappings, fbPortMapping{
			Index:        i,
			Description:  out["PortMappingDescription"],
			Protocol:     out["PortMappingProtocol"],
			ExternalPort: out["ExternalPort"],
			InternalHost: out["InternalClient"],
			InternalPort: out["InternalPort"],
			Enabled:      out["PortMappingEnabled"] == "1",
			Source:       "tr064",
		})
	}
	// FRITZ!OS 7+ keeps per-device sharing, including the ports devices open
	// themselves over UPnP/PCP, outside the TR-064 table; only the web UI's
	// portoverview page lists them. Firmware without the page answers with
	// no device list, which yields just the TR-064 rules.
	raw, err := b.web.Data(ctx, "portoverview", nil)
	if err != nil {
		return nil, fbErr(err)
	}
	mappings = append(mappings, parsePortOverview(raw)...)
	return mappings, nil
}

// igdRuleKeys lists portoverview's per-device igdrules fields in output order.
var igdRuleKeys = []struct{ key, protocol string }{
	{"TCP_ipv4", "TCP"}, {"TCP_ipv6", "TCP (IPv6)"},
	{"UDP_ipv4", "UDP"}, {"UDP_ipv6", "UDP (IPv6)"},
	// GRP/GSE are further protocol groups the page does not document; they
	// keep the router's own name rather than a guessed protocol.
	{"GRP_ipv4", "GRP"}, {"GRP_ipv6", "GRP (IPv6)"},
	{"GSE_ipv4", "GSE"}, {"GSE_ipv6", "GSE (IPv6)"},
}

// parsePortOverview turns the portoverview page into mappings: one per port a
// device opened itself, and one for a device exposed as the DMZ host. Index is
// -1 because none of them live in the TR-064 table 'portmap delete' walks.
func parsePortOverview(raw json.RawMessage) []fbPortMapping {
	list, ok := jsonPath(raw, "data", "devices")
	if !ok {
		return nil
	}
	var devices []struct {
		Name     string            `json:"devicename"`
		IP       string            `json:"localIpv4"`
		Exposed  bool              `json:"exposed_ipv4"`
		IGDRules map[string]string `json:"igdrules"`
	}
	if err := json.Unmarshal(list, &devices); err != nil {
		return nil
	}
	var out []fbPortMapping
	for _, d := range devices {
		for _, k := range igdRuleKeys {
			for _, port := range strings.Split(d.IGDRules[k.key], ",") {
				if port = strings.TrimSpace(port); port == "" {
					continue
				}
				out = append(out, fbPortMapping{
					Index: -1, Description: d.Name + " (UPnP/PCP)", Protocol: k.protocol,
					ExternalPort: port, InternalHost: d.IP, Enabled: true, Source: "upnp",
				})
			}
		}
		if d.Exposed {
			out = append(out, fbPortMapping{
				Index: -1, Description: d.Name + " (exposed host)", Protocol: "ALL",
				ExternalPort: "*", InternalHost: d.IP, Enabled: true, Source: "exposed-host",
			})
		}
	}
	return out
}

func newPortmapListCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "list",
		Short:       "List every port forwarding rule",
		Example:     "  fritzbox-pp-cli portmap list --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "list the port forwarding rules", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			mappings, err := box.PortMappings(ctx)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(mappings))
			for _, m := range mappings {
				rows = append(rows, m.row())
			}
			return fbEmit(cmd, flags, rows, "No port forwarding rules are configured.")
		},
	}
	return cmd
}

func newPortmapAddCmd(flags *rootFlags) *cobra.Command {
	var protocol, internalHost, description string
	var externalPort, internalPort int
	var confirm bool
	cmd := &cobra.Command{
		Use:         "add",
		Short:       "Add a port forwarding rule",
		Example:     "  fritzbox-pp-cli portmap add --external 8443 --host 192.168.178.30 --internal 443 --confirm",
		Annotations: map[string]string{"pp:happy-args": "--external=8443;--host=192.168.178.30;--internal=443"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "add a port forwarding rule", nil)
			}
			if externalPort <= 0 || internalPort <= 0 || internalHost == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--external, --internal, and --host are all required"))
			}
			proto := strings.ToUpper(protocol)
			if proto != "TCP" && proto != "UDP" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--protocol must be TCP or UDP"))
			}
			action := fmt.Sprintf("forward external %s port %d to %s:%d", proto, externalPort, internalHost, internalPort)
			if !confirm {
				return fbDryRun(cmd, flags, action+" (pass --confirm to actually do it)", map[string]any{
					"protocol": proto, "external_port": externalPort, "internal_host": internalHost, "internal_port": internalPort,
				})
			}
			if refused, err := fbRefuseUnderHarness(cmd, flags, action); refused {
				return err
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			svc, _, err := box.wanService(ctx)
			if err != nil {
				return err
			}
			if description == "" {
				description = fmt.Sprintf("fritzbox-pp-cli %s %d", proto, externalPort)
			}
			if _, err := box.Call(ctx, svc, "AddPortMapping", map[string]string{
				"RemoteHost": "", "ExternalPort": strconv.Itoa(externalPort),
				"PortMappingProtocol": proto, "InternalPort": strconv.Itoa(internalPort),
				"InternalClient": internalHost, "PortMappingEnabled": "1",
				"PortMappingDescription": description, "PortMappingLeaseDuration": "0",
			}); err != nil {
				return err
			}
			return fbEmitObject(cmd, flags, map[string]any{"created": true, "protocol": proto,
				"external_port": externalPort, "internal_host": internalHost, "internal_port": internalPort})
		},
	}
	cmd.Flags().StringVar(&protocol, "protocol", "TCP", "Protocol to forward: TCP or UDP")
	cmd.Flags().IntVar(&externalPort, "external", 0, "External port to listen on")
	cmd.Flags().IntVar(&internalPort, "internal", 0, "Internal port to forward to")
	cmd.Flags().StringVar(&internalHost, "host", "", "Internal IP address to forward to")
	cmd.Flags().StringVar(&description, "description", "", "Label shown in the router user interface")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Actually create the rule instead of printing what would happen")
	return cmd
}

func newPortmapDeleteCmd(flags *rootFlags) *cobra.Command {
	var protocol string
	var externalPort int
	var confirm bool
	cmd := &cobra.Command{
		Use:         "delete",
		Short:       "Delete a port forwarding rule",
		Example:     "  fritzbox-pp-cli portmap delete --external 8443 --confirm",
		Annotations: map[string]string{"pp:happy-args": "--external=8443"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "delete a port forwarding rule", nil)
			}
			if externalPort <= 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--external is required"))
			}
			proto := strings.ToUpper(protocol)
			action := fmt.Sprintf("delete the %s forwarding rule for external port %d", proto, externalPort)
			if !confirm {
				return fbDryRun(cmd, flags, action+" (pass --confirm to actually do it)", map[string]any{"protocol": proto, "external_port": externalPort})
			}
			if refused, err := fbRefuseUnderHarness(cmd, flags, action); refused {
				return err
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			svc, _, err := box.wanService(ctx)
			if err != nil {
				return err
			}
			if _, err := box.Call(ctx, svc, "DeletePortMapping", map[string]string{
				"RemoteHost": "", "ExternalPort": strconv.Itoa(externalPort), "PortMappingProtocol": proto,
			}); err != nil {
				return err
			}
			return fbEmitObject(cmd, flags, map[string]any{"deleted": true, "protocol": proto, "external_port": externalPort})
		},
	}
	cmd.Flags().StringVar(&protocol, "protocol", "TCP", "Protocol of the rule to remove: TCP or UDP")
	cmd.Flags().IntVar(&externalPort, "external", 0, "External port of the rule to remove")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Actually delete the rule instead of printing what would happen")
	return cmd
}

// ------------------------------------------------------------------- log ----

// fbLogEntry is one router log line.
type fbLogEntry struct {
	At       time.Time `json:"at"`
	Message  string    `json:"message"`
	Category string    `json:"category"`
	ID       string    `json:"id"`
}

// LogEntries fetches the router's current log buffer.
func (b *fbBox) LogEntries(ctx context.Context) ([]fbLogEntry, error) {
	raw, err := b.web.Data(ctx, "log", nil)
	if err != nil {
		return nil, fbErr(err)
	}
	list, ok := jsonPath(raw, "data", "log")
	if !ok {
		return nil, apiErr(fmt.Errorf("router returned no log data"))
	}
	var rows []struct {
		Time  string `json:"time"`
		Date  string `json:"date"`
		Group string `json:"group"`
		Msg   string `json:"msg"`
	}
	if err := json.Unmarshal(list, &rows); err != nil {
		return nil, apiErr(fmt.Errorf("parsing the router log: %w", err))
	}
	entries := make([]fbLogEntry, 0, len(rows))
	for _, r := range rows {
		at := parseFritzDateTime(r.Date, r.Time)
		msg := strings.TrimSpace(r.Msg)
		entries = append(entries, fbLogEntry{
			At: at, Message: msg, Category: r.Group,
			// The router assigns no stable identifier, so one is derived from
			// the timestamp and text. That is what makes repeated polling
			// idempotent instead of duplicating every line each time.
			ID: logEntryID(r.Date, r.Time, msg),
		})
	}
	return entries, nil
}

func parseFritzDateTime(date, timeStr string) time.Time {
	combined := strings.TrimSpace(date) + " " + strings.TrimSpace(timeStr)
	for _, layout := range []string{"02.01.06 15:04:05", "02.01.2006 15:04:05", "02.01.06 15:04"} {
		if t, err := time.ParseInLocation(layout, combined, time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}

func logEntryID(date, timeStr, msg string) string {
	sum := sha256.Sum256([]byte(date + "|" + timeStr + "|" + msg))
	return hex.EncodeToString(sum[:12])
}

func newLogTailCmd(flags *rootFlags) *cobra.Command {
	var limit int
	var category string
	var noStore bool
	cmd := &cobra.Command{
		Use:   "tail",
		Short: "Show the most recent router log entries",
		Long: `Show the newest entries from the router's log buffer.

Every run also appends what it sees into the local database, so 'log search'
can later reach entries the router has already rotated away. Pass --no-store to
read without recording.`,
		Example:     "  fritzbox-pp-cli log tail --limit 20 --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read the router log", map[string]any{"limit": limit})
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			entries, err := box.LogEntries(ctx)
			if err != nil {
				return err
			}
			if !noStore {
				if err := box.recordLog(ctx, entries); err != nil {
					// Retention is a bonus on top of the read; failing to
					// persist must not hide the log the user asked for.
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not retain log entries: %v\n", err)
				}
			}
			rows := make([]map[string]any, 0, len(entries))
			for _, e := range entries {
				if category != "" && !strings.EqualFold(e.Category, category) {
					continue
				}
				at := ""
				if !e.At.IsZero() {
					at = e.At.Format(time.RFC3339)
				}
				rows = append(rows, map[string]any{"at": at, "category": e.Category, "message": e.Message})
				if limit > 0 && len(rows) >= limit {
					break
				}
			}
			return fbEmit(cmd, flags, rows, "The router log is empty.")
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of entries to show; 0 means all")
	cmd.Flags().StringVar(&category, "category", "", "Only show entries in this category, for example sys, wlan, or fon")
	cmd.Flags().BoolVar(&noStore, "no-store", false, "Do not append the entries to the local database")
	return cmd
}

// recordLog appends log entries into the local database.
func (b *fbBox) recordLog(ctx context.Context, entries []fbLogEntry) error {
	if len(entries) == 0 {
		return nil
	}
	db, err := b.openStore(ctx, "")
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	rows := make([]store.LogEntry, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, store.LogEntry{
			ID: e.ID, LoggedAt: e.At.Unix(), Message: e.Message, Category: e.Category,
		})
	}
	_, err = db.InsertLogEntries(ctx, rows)
	return err
}

// ------------------------------------------------------------------- vpn ----

func newVPNCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "vpn", Short: "VPN connection state"}
	cmd.AddCommand(&cobra.Command{
		Use:         "status",
		Short:       "Show the router's VPN configuration and connection state",
		Example:     "  fritzbox-pp-cli vpn status --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read the VPN state", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			raw, err := box.web.Data(ctx, "sysStatus", nil)
			if err != nil {
				return err
			}
			vpn, ok := jsonPath(raw, "data", "vpn")
			if !ok {
				return apiErr(fmt.Errorf("router returned no VPN information"))
			}
			var payload any
			if err := json.Unmarshal(vpn, &payload); err != nil {
				return apiErr(fmt.Errorf("parsing the VPN state: %w", err))
			}
			return fbEmitObject(cmd, flags, map[string]any{"vpn": payload})
		},
	})
	return cmd
}

// -------------------------------------------------------------- snapshot ----

func newSnapshotCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Record the current network state into the local database",
		Long: `Record a point-in-time snapshot of the devices on the network, the internet
connection, the log buffer, and the router's power draw.

This is what gives 'hosts diff', 'wan history', and 'log search' something to
compare against. Run it on a schedule to build history; a single snapshot is
enough for the commands to run, but at least two are needed before a diff can
report anything.`,
		Example: "  fritzbox-pp-cli snapshot --agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "record a network snapshot", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			db, err := box.openStore(ctx, "")
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			now := time.Now().Unix()
			summary := map[string]any{"taken_at": time.Unix(now, 0).Format(time.RFC3339)}
			// Each capture is independent: a router without a DSL line or
			// without smart-home support must still record the parts it has.
			var problems []string

			if hosts, err := box.Hosts(ctx); err != nil {
				problems = append(problems, "hosts: "+err.Error())
			} else {
				rows := make([][]any, 0, len(hosts))
				for _, h := range hosts {
					active := 0
					if h.Online() {
						active = 1
					}
					rows = append(rows, []any{now, h.MAC, h.Name, h.IP, active})
				}
				if err := insertRows(ctx, db, "fb_host_snapshots",
					[]string{"taken_at", "mac", "name", "ip", "active"}, rows); err != nil {
					problems = append(problems, "hosts: "+err.Error())
				} else {
					summary["hosts_recorded"] = len(rows)
				}
				// Also refresh the generic resource mirror the framework's
				// `search` command reads. The generated `sync` cannot populate
				// it for this resource because it does not send the endpoint's
				// default data-path expression, so snapshot is the one command
				// that leaves every local table current.
				items := make([]json.RawMessage, 0, len(hosts))
				for _, h := range hosts {
					encoded, err := json.Marshal(h)
					if err != nil {
						continue
					}
					items = append(items, encoded)
				}
				if stored, _, err := db.UpsertBatch("hosts", items); err != nil {
					problems = append(problems, "host mirror: "+err.Error())
				} else {
					summary["hosts_mirrored"] = stored
				}
			}

			if svc, info, err := box.wanService(ctx); err != nil {
				problems = append(problems, "wan: "+err.Error())
			} else {
				uptime, _ := strconv.ParseInt(info["Uptime"], 10, 64)
				if err := insertRows(ctx, db, "fb_wan_samples",
					[]string{"taken_at", "uptime", "external_ip", "status", "last_error"},
					[][]any{{now, uptime, info["ExternalIPAddress"], info["ConnectionStatus"], info["LastConnectionError"]}}); err != nil {
					problems = append(problems, "wan: "+err.Error())
				} else {
					summary["wan_service"] = svc
					summary["wan_uptime_seconds"] = uptime
				}
			}

			if entries, err := box.LogEntries(ctx); err != nil {
				problems = append(problems, "log: "+err.Error())
			} else {
				rows := make([]store.LogEntry, 0, len(entries))
				for _, e := range entries {
					rows = append(rows, store.LogEntry{ID: e.ID, LoggedAt: e.At.Unix(), Message: e.Message, Category: e.Category})
				}
				if n, err := db.InsertLogEntries(ctx, rows); err != nil {
					problems = append(problems, "log: "+err.Error())
				} else {
					summary["log_entries_added"] = n
				}
			}

			if drains, err := box.EnergyDrain(ctx); err != nil {
				problems = append(problems, "energy: "+err.Error())
			} else {
				rows := make([][]any, 0, len(drains))
				for _, d := range drains {
					rows = append(rows, []any{now, d.Name, d.ActPercent})
				}
				if err := insertRows(ctx, db, "fb_energy_samples",
					[]string{"taken_at", "subsystem", "percent"}, rows); err != nil {
					problems = append(problems, "energy: "+err.Error())
				} else {
					summary["energy_samples"] = len(rows)
				}
			}

			if len(problems) > 0 {
				sort.Strings(problems)
				summary["partial_failures"] = problems
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %d part(s) of the snapshot could not be recorded\n", len(problems))
			}
			return fbEmitObject(cmd, flags, summary)
		},
	}
	return cmd
}

// insertRows writes rows into one FRITZ!Box history table in a single
// transaction. Unlike ReplaceRows it appends, because history must accumulate.
func insertRows(ctx context.Context, db *store.Store, table string, columns []string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}
	// Identifiers cannot be bound as parameters, so they are validated instead.
	if err := store.ValidateIdentifiers(table, columns); err != nil {
		return err
	}
	tx, err := db.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("opening the %s write transaction: %w", table, err)
	}
	defer func() { _ = tx.Rollback() }()
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(columns)), ", ")
	// #nosec G202 -- table and columns validated by store.ValidateIdentifiers
	// above; every value is a placeholder.
	stmt := "INSERT OR REPLACE INTO " + table + " (" + strings.Join(columns, ", ") + ") VALUES (" + placeholders + ")"
	for _, row := range rows {
		if _, err := tx.ExecContext(ctx, stmt, row...); err != nil {
			return fmt.Errorf("inserting into %s: %w", table, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing %s: %w", table, err)
	}
	return nil
}

// --------------------------------------------------------------- metrics ----

func newMetricsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Print router metrics in Prometheus text exposition format",
		Long: `Print the router's current counters in Prometheus text exposition format.

This is a one-shot read, not a daemon: point a scrape job or a cron entry at it
rather than leaving it running.`,
		Example:     "  fritzbox-pp-cli metrics",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read router metrics", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			type metric struct {
				name, help, kind string
				value            float64
			}
			var metrics []metric

			if info, err := box.Call(ctx, "DeviceInfo1", "GetInfo", nil); err == nil {
				if v, err := strconv.ParseFloat(info["UpTime"], 64); err == nil {
					metrics = append(metrics, metric{"fritzbox_uptime_seconds", "Router uptime in seconds", "counter", v})
				}
			}
			if _, info, err := box.wanService(ctx); err == nil {
				if v, err := strconv.ParseFloat(info["Uptime"], 64); err == nil {
					metrics = append(metrics, metric{"fritzbox_wan_uptime_seconds", "Current internet connection uptime in seconds", "counter", v})
				}
				connected := 0.0
				if strings.EqualFold(info["ConnectionStatus"], "Connected") {
					connected = 1
				}
				metrics = append(metrics, metric{"fritzbox_wan_connected", "Whether the internet connection is up", "gauge", connected})
			}
			for action, name := range map[string]string{
				"GetTotalBytesSent":     "fritzbox_wan_bytes_sent_total",
				"GetTotalBytesReceived": "fritzbox_wan_bytes_received_total",
			} {
				if out, err := box.Call(ctx, "WANCommonInterfaceConfig1", action, nil); err == nil {
					for _, raw := range out {
						if v, err := strconv.ParseFloat(raw, 64); err == nil {
							metrics = append(metrics, metric{name, "WAN traffic counter in bytes", "counter", v})
						}
						break
					}
				}
			}
			if hosts, err := box.Hosts(ctx); err == nil {
				online := 0.0
				for _, h := range hosts {
					if h.Online() {
						online++
					}
				}
				metrics = append(metrics, metric{"fritzbox_hosts_known", "Devices the router remembers", "gauge", float64(len(hosts))})
				metrics = append(metrics, metric{"fritzbox_hosts_online", "Devices currently online", "gauge", online})
			}
			if len(metrics) == 0 {
				return apiErr(fmt.Errorf("router returned no metrics"))
			}
			sort.Slice(metrics, func(i, j int) bool { return metrics[i].name < metrics[j].name })
			out := cmd.OutOrStdout()
			// Prometheus text is the default because that is what a scrape job
			// wants, but --json and --agent are honoured like everywhere else
			// rather than emitting a body the caller cannot parse.
			if !wantsHumanTable(out, flags) {
				rows := make([]map[string]any, 0, len(metrics))
				for _, m := range metrics {
					rows = append(rows, map[string]any{
						"name": m.name, "type": m.kind, "help": m.help, "value": m.value,
					})
				}
				return fbPrintJSON(out, rows, flags, "live")
			}
			for _, m := range metrics {
				fmt.Fprintf(out, "# HELP %s %s\n# TYPE %s %s\n%s %g\n", m.name, m.help, m.name, m.kind, m.name, m.value)
			}
			return nil
		},
	}
	return cmd
}
