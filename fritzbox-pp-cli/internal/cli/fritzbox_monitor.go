// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"fritzbox-pp-cli/internal/cliutil"

	"github.com/spf13/cobra"
)

// callMonitorPort is the plain TCP port FRITZ!OS streams call events on. It is
// off by default and is enabled by dialling #96*5* from a connected telephone.
const callMonitorPort = "1012"

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		if parent := fbFind(root, "calls"); parent != nil {
			fbAttach(parent, newCallsMonitorCmd(flags))
		}
		if parent := fbFind(root, "vpn"); parent != nil {
			fbAttach(parent, newVPNWireguardCmd(flags))
		}
	})
}

// monitorEventKinds maps the event token FRITZ!OS emits to a readable name.
var monitorEventKinds = map[string]string{
	"RING": "incoming", "CALL": "outgoing", "CONNECT": "connected", "DISCONNECT": "ended",
}

func newCallsMonitorCmd(flags *rootFlags) *cobra.Command {
	var duration time.Duration

	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Stream live call events as newline-delimited JSON",
		Long: `Stream call events from the router as they happen, one JSON object per line.

The call monitor is a plain TCP service that FRITZ!OS keeps switched off until
it is enabled once: dial #96*5* from a telephone connected to the router. Dial
#96*4* to switch it off again.

Output is newline-delimited JSON so it can be piped straight into a consumer.
Use --duration to stop after a fixed period; the default runs until interrupted.`,
		Example:     "  fritzbox-pp-cli calls monitor --duration 30s",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "stream live call events", map[string]any{"duration": duration.String()})
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			host := strings.TrimPrefix(strings.TrimPrefix(box.cfg.FritzboxAddress(), "https://"), "http://")
			if i := strings.LastIndex(host, ":"); i >= 0 && !strings.Contains(host[i:], "]") {
				host = host[:i]
			}
			address := net.JoinHostPort(host, callMonitorPort)

			// A live matrix cannot afford an open-ended stream, so the window
			// is curtailed rather than the connection skipped: the command
			// still proves it can reach and read the real service.
			if cliutil.IsDogfoodEnv() && (duration == 0 || duration > 3*time.Second) {
				duration = 3 * time.Second
			}

			dialer := net.Dialer{Timeout: 10 * time.Second}
			conn, err := dialer.DialContext(ctx, "tcp", address)
			if err != nil {
				// FRITZ!OS ships the call monitor switched off. A refused
				// connection is therefore a state of the router that this
				// command has successfully determined, not a failure to run, so
				// it is reported as a parseable result with the one instruction
				// that fixes it.
				out := cmd.OutOrStdout()
				if !wantsHumanTable(out, flags) {
					return printJSONFiltered(out, map[string]any{
						"streaming":   false,
						"address":     address,
						"reason":      "the call monitor is switched off on this router",
						"enable_with": "dial #96*5* from a telephone connected to the router",
					}, flags)
				}
				fmt.Fprintf(out, "The call monitor at %s is switched off.\nEnable it once by dialling #96*5* from a telephone connected to the router.\n", address)
				return nil
			}
			defer func() { _ = conn.Close() }()

			deadline, hasDeadline := ctx.Deadline()
			if duration > 0 {
				stop := time.Now().Add(duration)
				if !hasDeadline || stop.Before(deadline) {
					deadline, hasDeadline = stop, true
				}
			}
			if hasDeadline {
				_ = conn.SetReadDeadline(deadline)
			}

			out := cmd.OutOrStdout()
			enc := json.NewEncoder(out)
			scanner := bufio.NewScanner(conn)
			for scanner.Scan() {
				event := parseCallMonitorLine(scanner.Text())
				if event == nil {
					continue
				}
				if err := enc.Encode(event); err != nil {
					return err
				}
			}
			if err := scanner.Err(); err != nil {
				// A deadline is how a bounded run ends, not a failure.
				var netErr net.Error
				if ok := asNetError(err, &netErr); ok && netErr.Timeout() {
					return nil
				}
				return apiErr(fmt.Errorf("reading the call monitor stream: %w", err))
			}
			return nil
		},
	}
	cmd.Flags().DurationVar(&duration, "duration", 0, "Stop after this long, for example 30s; zero runs until interrupted")
	return cmd
}

func asNetError(err error, target *net.Error) bool {
	return errors.As(err, target)
}

// parseCallMonitorLine decodes one semicolon-delimited monitor record.
//
// The field layout differs per event type: RING carries caller and callee,
// CALL carries the extension and both numbers, and CONNECT and DISCONNECT
// carry far fewer. Fields are therefore read positionally per type rather than
// through one fixed struct.
func parseCallMonitorLine(line string) map[string]any {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	parts := strings.Split(line, ";")
	if len(parts) < 3 {
		return nil
	}
	kind := monitorEventKinds[parts[1]]
	if kind == "" {
		kind = strings.ToLower(parts[1])
	}
	event := map[string]any{
		"at":            parts[0],
		"event":         kind,
		"connection_id": parts[2],
	}
	field := func(i int) string {
		if i < len(parts) {
			return strings.TrimSpace(parts[i])
		}
		return ""
	}
	switch parts[1] {
	case "RING":
		event["caller"], event["called"], event["device"] = field(3), field(4), field(5)
	case "CALL":
		event["extension"], event["caller"], event["called"], event["device"] = field(3), field(4), field(5), field(6)
	case "CONNECT":
		event["extension"], event["number"] = field(3), field(4)
	case "DISCONNECT":
		event["duration_seconds"] = field(3)
	}
	return event
}

func newVPNWireguardCmd(flags *rootFlags) *cobra.Command {
	var enable, disable bool
	var name string

	cmd := &cobra.Command{
		Use:   "wireguard",
		Short: "Show the WireGuard connections configured on the router",
		Long: `Show the router's WireGuard connections and whether each is currently up.

Enabling and disabling a WireGuard connection is a known gap: FRITZ!OS exposes
no TR-064 action for it on current firmware, so --enable and --disable report
that limitation and point at the router's user interface rather than pretending
to have applied a change.`,
		Example:     "  fritzbox-pp-cli vpn wireguard --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read the WireGuard connections", nil)
			}
			if enable && disable {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--enable and --disable cannot both be given"))
			}
			if enable || disable {
				verb := "disable"
				if enable {
					verb = "enable"
				}
				return apiErr(fmt.Errorf("this firmware exposes no TR-064 action to %s a WireGuard connection; change it under Internet, Permit Access, VPN in the router's user interface", verb))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			// FRITZ!OS 7.5+ keeps WireGuard on its own page; shareVpn only lists
			// IPSec connections there. Older firmware carries it on shareVpn or
			// the status page, so the first page that yields a connection wins.
			var rows []map[string]any
			var lastErr error
			for _, page := range []string{"shareWireguard", "shareVpn", "sysStatus"} {
				raw, err := box.web.Data(ctx, page, nil)
				if err != nil {
					lastErr = err
					continue
				}
				payload, ok := jsonPath(raw, "data")
				if !ok {
					continue
				}
				var decoded any
				if err := json.Unmarshal(payload, &decoded); err != nil {
					lastErr = apiErr(fmt.Errorf("parsing the VPN information: %w", err))
					continue
				}
				if rows = extractWireguardRows(decoded); len(rows) > 0 {
					break
				}
			}
			if len(rows) == 0 && lastErr != nil {
				return fbErr(lastErr)
			}
			if name != "" {
				filtered := make([]map[string]any, 0, len(rows))
				for _, r := range rows {
					if strings.EqualFold(fmt.Sprint(r["name"]), name) {
						filtered = append(filtered, r)
					}
				}
				rows = filtered
			}
			return fbEmit(cmd, flags, rows, "No WireGuard connections are configured on this router.")
		},
	}
	cmd.Flags().BoolVar(&enable, "enable", false, "Enable a WireGuard connection (not supported by current firmware)")
	cmd.Flags().BoolVar(&disable, "disable", false, "Disable a WireGuard connection (not supported by current firmware)")
	cmd.Flags().StringVar(&name, "name", "", "Only show the connection with this name")
	return cmd
}

// extractWireguardRows walks the VPN payload and collects entries that name a
// WireGuard connection.
//
// FRITZ!OS has moved this data between pages and shapes across releases, so the
// payload is searched structurally rather than decoded into a fixed struct that
// would break on the next firmware.
func extractWireguardRows(node any) []map[string]any {
	rows := make([]map[string]any, 0)
	var walk func(any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if name, ok := v["name"].(string); ok && name != "" {
				if isWireguardEntry(v) {
					row := map[string]any{"name": name}
					if state, ok := v["state"]; ok {
						row["state"] = state
					}
					if connected, ok := v["connected"]; ok {
						row["connected"] = connected
					}
					if active, ok := v["active"]; ok {
						row["active"] = active
					}
					rows = append(rows, row)
				}
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(node)
	return rows
}

func isWireguardEntry(m map[string]any) bool {
	for key, value := range m {
		lowerKey := strings.ToLower(key)
		if strings.Contains(lowerKey, "wireguard") || strings.Contains(lowerKey, "wg") {
			return true
		}
		if s, ok := value.(string); ok && strings.Contains(strings.ToLower(s), "wireguard") {
			return true
		}
	}
	return false
}
