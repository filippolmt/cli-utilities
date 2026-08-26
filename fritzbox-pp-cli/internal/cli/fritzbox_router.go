// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		fbAttach(root, newDeviceCmd(flags))
		fbAttach(root, newWifiCmd(flags))
		if parent := fbFind(root, "wan"); parent != nil {
			fbAttach(parent, newWanStatusCmd(flags))
			fbAttach(parent, newWanReconnectCmd(flags))
			fbAttach(parent, newWanStatsCmd(flags))
			fbAttach(parent, newWanLinkCmd(flags))
		}
		fbAttach(root, newDslCmd(flags))
		fbAttach(root, newLanCmd(flags))
	})
}

// wanService picks the connection service this router actually uses.
//
// A FRITZ!Box exposes both WANPPPConnection and WANIPConnection, but only one
// carries the live connection: PPPoE lines answer on the former and everything
// else on the latter. Guessing wrong returns a plausible-looking record full of
// zeros, so the choice is made from the reported connection status rather than
// from the access type.
func (b *fbBox) wanService(ctx context.Context) (string, map[string]string, error) {
	candidates := []string{"WANPPPConnection1", "WANIPConnection1"}
	var firstErr error
	var fallbackName string
	var fallbackInfo map[string]string
	for _, name := range candidates {
		info, err := b.Call(ctx, name, "GetInfo", nil)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if strings.EqualFold(info["ConnectionStatus"], "Connected") {
			return name, info, nil
		}
		if fallbackName == "" {
			fallbackName, fallbackInfo = name, info
		}
	}
	if fallbackName != "" {
		return fallbackName, fallbackInfo, nil
	}
	if firstErr != nil {
		return "", nil, firstErr
	}
	return "", nil, apiErr(fmt.Errorf("router exposed no usable WAN connection service"))
}

// ---------------------------------------------------------------- device ----

func newDeviceCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "device", Short: "Router hardware, firmware, and system actions"}
	cmd.AddCommand(newDeviceInfoCmd(flags), newDeviceRebootCmd(flags), newDeviceLedCmd(flags), newDeviceKeylockCmd(flags), newDeviceBackupCmd(flags))
	return cmd
}

func newDeviceInfoCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use: "get",
		// "info" was the original name and stays as an alias: the CLI's own
		// docs and any script written against the first release use it.
		Aliases:     []string{"info"},
		Short:       "Show model, firmware version, serial number, and uptime",
		Example:     "  fritzbox-pp-cli device get --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read router device information", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			info, err := box.Call(ctx, "DeviceInfo1", "GetInfo", nil)
			if err != nil {
				return err
			}
			row := map[string]any{
				"model":            info["ModelName"],
				"description":      info["Description"],
				"software_version": info["SoftwareVersion"],
				"serial_number":    info["SerialNumber"],
				"uptime_seconds":   info["UpTime"],
				"uptime_human":     humanUptime(info["UpTime"]),
			}
			// The update check is a separate service and is allowed to fail
			// without taking the whole command down: firmware currency is a
			// nice-to-have next to the identity fields above.
			if upd, updErr := box.Call(ctx, "UserInterface1", "GetInfo", nil); updErr == nil {
				row["update_available"] = upd["UpgradeAvailable"] == "1"
				if v := upd["NewX_AVM-DE_Version"]; v != "" {
					row["update_version"] = v
				}
			}
			return fbEmitObject(cmd, flags, row)
		},
	}
	return cmd
}

func humanUptime(seconds string) string {
	n, err := strconv.ParseInt(strings.TrimSpace(seconds), 10, 64)
	if err != nil || n < 0 {
		return ""
	}
	days := n / 86400
	hours := (n % 86400) / 3600
	mins := (n % 3600) / 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

func newDeviceRebootCmd(flags *rootFlags) *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{
		Use:     "reboot",
		Short:   "Restart the router, dropping the WAN link and every wireless client for a minute or two",
		Long:    "Restart the router. Prints what would happen unless --confirm is passed, and always refuses while a verification harness is active.",
		Example: "  fritzbox-pp-cli device reboot --confirm",
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "reboot the router", nil)
			}
			if !confirm {
				return fbDryRun(cmd, flags, "reboot the router (pass --confirm to actually do it)", nil)
			}
			if refused, err := fbRefuseUnderHarness(cmd, flags, "reboot the router"); refused {
				return err
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			if _, err := box.Call(ctx, "DeviceConfig1", "Reboot", nil); err != nil {
				return err
			}
			return fbEmitObject(cmd, flags, map[string]any{"rebooting": true, "note": "the router will be unreachable for a minute or two"})
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Actually reboot instead of printing what would happen")
	return cmd
}

func newDeviceLedCmd(flags *rootFlags) *cobra.Command {
	var on, off bool
	var brightness int
	cmd := &cobra.Command{
		Use:     "led",
		Short:   "Read or change the status LED display and brightness",
		Example: "  fritzbox-pp-cli device led --agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			write := on || off || brightness > 0
			if on && off {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--on and --off cannot both be given"))
			}
			if brightness != 0 && (brightness < 1 || brightness > 3) {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--brightness must be 1, 2, or 3"))
			}
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read or change the status LEDs", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			if !write {
				out, err := box.Call(ctx, "UserInterface1", "X_AVM-DE_GetInfo", nil)
				if err != nil {
					return err
				}
				return fbEmitObject(cmd, flags, soapToRow(out))
			}
			if refused, err := fbRefuseUnderHarness(cmd, flags, "change the status LEDs"); refused {
				return err
			}
			args2 := map[string]string{}
			if on {
				args2["LEDDisplay"] = "0"
			}
			if off {
				args2["LEDDisplay"] = "2"
			}
			if brightness > 0 {
				args2["LEDBrightness"] = strconv.Itoa(brightness)
			}
			if _, err := box.Call(ctx, "UserInterface1", "X_AVM-DE_SetConfig", args2); err != nil {
				return err
			}
			return fbEmitObject(cmd, flags, map[string]any{"changed": true, "settings": args2})
		},
	}
	cmd.Flags().BoolVar(&on, "on", false, "Turn the status LEDs on")
	cmd.Flags().BoolVar(&off, "off", false, "Turn the status LEDs off")
	cmd.Flags().IntVar(&brightness, "brightness", 0, "LED brightness from 1 (dim) to 3 (bright)")
	return cmd
}

func newDeviceKeylockCmd(flags *rootFlags) *cobra.Command {
	var on, off bool
	cmd := &cobra.Command{
		Use:     "keylock",
		Short:   "Read or change the front-panel key lock",
		Example: "  fritzbox-pp-cli device keylock --agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			if on && off {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--on and --off cannot both be given"))
			}
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read or change the key lock", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			if !on && !off {
				out, err := box.Call(ctx, "UserInterface1", "X_AVM-DE_GetInfo", nil)
				if err != nil {
					return err
				}
				return fbEmitObject(cmd, flags, map[string]any{"keylock_enabled": out["KeyLockEnabled"] == "1"})
			}
			if refused, err := fbRefuseUnderHarness(cmd, flags, "change the key lock"); refused {
				return err
			}
			value := "0"
			if on {
				value = "1"
			}
			if _, err := box.Call(ctx, "UserInterface1", "X_AVM-DE_SetConfig", map[string]string{"KeyLockEnabled": value}); err != nil {
				return err
			}
			return fbEmitObject(cmd, flags, map[string]any{"changed": true, "keylock_enabled": on})
		},
	}
	cmd.Flags().BoolVar(&on, "on", false, "Enable the key lock")
	cmd.Flags().BoolVar(&off, "off", false, "Disable the key lock")
	return cmd
}

func newDeviceBackupCmd(flags *rootFlags) *cobra.Command {
	var password, outPath string
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Export the router configuration to a file",
		Long: strings.Trim(`
Export the router configuration. FRITZ!OS requires an export password, and the
resulting file contains credentials for every service the router knows about,
so treat it as a secret.
`, "\n"),
		Example: "  fritzbox-pp-cli device backup --password secret --out ./fritzbox-config.export",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "export the router configuration", map[string]any{"out": outPath})
			}
			if password == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--password is required; FRITZ!OS refuses to export an unencrypted configuration"))
			}
			if outPath == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--out is required"))
			}
			if refused, err := fbRefuseUnderHarness(cmd, flags, "write a configuration export to disk"); refused {
				return err
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			out, err := box.Call(ctx, "DeviceConfig1", "X_AVM-DE_GetConfigFile", map[string]string{"X_AVM-DE_Password": password})
			if err != nil {
				return err
			}
			url := out["X_AVM-DE_ConfigFileUrl"]
			if url == "" {
				return apiErr(fmt.Errorf("router returned no export URL"))
			}
			body, err := box.web.Fetch(ctx, url)
			if err != nil {
				return fbErr(err)
			}
			if err := writeSecretFile(outPath, body); err != nil {
				return err
			}
			return fbEmitObject(cmd, flags, map[string]any{"written": outPath, "bytes": len(body)})
		},
	}
	cmd.Flags().StringVar(&password, "password", "", "Export password required by FRITZ!OS")
	cmd.Flags().StringVar(&outPath, "out", "", "Path to write the exported configuration to")
	return cmd
}

// ------------------------------------------------------------------ wifi ----

// wifiServices maps the CLI's band names onto the router's numbered
// WLANConfiguration instances. FRITZ!OS assigns them positionally: the first is
// 2.4 GHz, the second 5 GHz, and the last one present is the guest network.
func wifiServiceFor(band string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(band)) {
	case "", "2.4", "2.4ghz", "2g":
		return "WLANConfiguration1", nil
	case "5", "5ghz", "5g":
		return "WLANConfiguration2", nil
	case "guest":
		return "WLANConfiguration3", nil
	default:
		return "", usageErr(fmt.Errorf("--band must be one of 2.4, 5, or guest; got %q", band))
	}
}

func newWifiCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "wifi", Short: "Wireless networks: state, statistics, keys, and channels"}
	cmd.AddCommand(newWifiListCmd(flags), newWifiStatusCmd(flags), newWifiOnCmd(flags), newWifiOffCmd(flags),
		newWifiStatsCmd(flags), newWifiKeyCmd(flags), newWifiQRCmd(flags), newWifiChannelCmd(flags))
	return cmd
}

func newWifiListCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "list",
		Short:       "List every wireless network with its state and client count",
		Example:     "  fritzbox-pp-cli wifi list --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "list the wireless networks", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, 3)
			for _, band := range []string{"2.4", "5", "guest"} {
				svc, _ := wifiServiceFor(band)
				info, err := box.Call(ctx, svc, "GetInfo", nil)
				if err != nil {
					// Not every model has all three radios; a missing instance
					// is a fact about the hardware, not a command failure.
					continue
				}
				row := map[string]any{
					"band":     band,
					"ssid":     info["SSID"],
					"enabled":  info["Enable"] == "1",
					"status":   info["Status"],
					"channel":  info["Channel"],
					"standard": info["Standard"],
				}
				if assoc, aErr := box.Call(ctx, svc, "GetTotalAssociations", nil); aErr == nil {
					row["clients"] = assoc["TotalAssociations"]
				}
				rows = append(rows, row)
			}
			return fbEmit(cmd, flags, rows, "This router exposed no wireless networks.")
		},
	}
	return cmd
}

func newWifiStatusCmd(flags *rootFlags) *cobra.Command {
	var band string
	cmd := &cobra.Command{
		Use:         "status",
		Short:       "Show the full settings of one wireless network",
		Example:     "  fritzbox-pp-cli wifi status --band 5 --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read a wireless network's settings", map[string]any{"band": band})
			}
			svc, err := wifiServiceFor(band)
			if err != nil {
				return err
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			info, err := box.Call(ctx, svc, "GetInfo", nil)
			if err != nil {
				return err
			}
			return fbEmitObject(cmd, flags, soapToRow(info))
		},
	}
	cmd.Flags().StringVar(&band, "band", "2.4", "Which network to act on: 2.4, 5, or guest")
	return cmd
}

func newWifiOnCmd(flags *rootFlags) *cobra.Command  { return newWifiToggleCmd(flags, true) }
func newWifiOffCmd(flags *rootFlags) *cobra.Command { return newWifiToggleCmd(flags, false) }

func newWifiToggleCmd(flags *rootFlags, enable bool) *cobra.Command {
	var band string
	var confirm bool
	verb, use := "off", "off"
	if enable {
		verb, use = "on", "on"
	}
	cmd := &cobra.Command{
		Use:     use,
		Short:   fmt.Sprintf("Turn a wireless network %s", verb),
		Long:    fmt.Sprintf("Turn a wireless network %s. Prints what would happen unless --confirm is passed, and refuses while a verification harness is active.", verb),
		Example: fmt.Sprintf("  fritzbox-pp-cli wifi %s --band guest --confirm", use),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := wifiServiceFor(band)
			if err != nil {
				return err
			}
			action := fmt.Sprintf("turn the %s wireless network %s", band, verb)
			if dryRunOK(flags) || !confirm {
				detail := map[string]any{"band": band, "service": svc}
				if !confirm && !dryRunOK(flags) {
					return fbDryRun(cmd, flags, action+" (pass --confirm to actually do it)", detail)
				}
				return fbDryRun(cmd, flags, action, detail)
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
			value := "0"
			if enable {
				value = "1"
			}
			if _, err := box.Call(ctx, svc, "SetEnable", map[string]string{"Enable": value}); err != nil {
				return err
			}
			return fbEmitObject(cmd, flags, map[string]any{"band": band, "enabled": enable, "changed": true})
		},
	}
	cmd.Flags().StringVar(&band, "band", "2.4", "Which network to act on: 2.4, 5, or guest")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Actually apply the change instead of printing what would happen")
	return cmd
}

func newWifiStatsCmd(flags *rootFlags) *cobra.Command {
	var band string
	cmd := &cobra.Command{
		Use:         "stats",
		Short:       "Show packet and association statistics for a wireless network",
		Example:     "  fritzbox-pp-cli wifi stats --band 2.4 --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read wireless statistics", map[string]any{"band": band})
			}
			svc, err := wifiServiceFor(band)
			if err != nil {
				return err
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			row := map[string]any{"band": band}
			if pkt, err := box.Call(ctx, svc, "GetPacketStatistics", nil); err == nil {
				for k, v := range pkt {
					row[strings.ToLower(k)] = v
				}
			}
			if assoc, err := box.Call(ctx, svc, "GetTotalAssociations", nil); err == nil {
				row["clients"] = assoc["TotalAssociations"]
			}
			if len(row) == 1 {
				return apiErr(fmt.Errorf("router returned no statistics for the %s network", band))
			}
			return fbEmitObject(cmd, flags, row)
		},
	}
	cmd.Flags().StringVar(&band, "band", "2.4", "Which network to act on: 2.4, 5, or guest")
	return cmd
}

func newWifiKeyCmd(flags *rootFlags) *cobra.Command {
	var band string
	var reveal bool
	cmd := &cobra.Command{
		Use:   "key",
		Short: "Show the pre-shared key of a wireless network",
		Long: strings.Trim(`
Show a wireless network's pre-shared key.

The key is masked unless --reveal is passed, because this command is frequently
run in a shared terminal or piped into a transcript.
`, "\n"),
		Example:     "  fritzbox-pp-cli wifi key --band guest --reveal",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read a wireless pre-shared key", map[string]any{"band": band})
			}
			svc, err := wifiServiceFor(band)
			if err != nil {
				return err
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			keys, err := box.Call(ctx, svc, "GetSecurityKeys", nil)
			if err != nil {
				return err
			}
			info, err := box.Call(ctx, svc, "GetInfo", nil)
			if err != nil {
				return err
			}
			key := keys["KeyPassphrase"]
			shown := key
			if !reveal {
				shown = maskSecret(key)
			}
			return fbEmitObject(cmd, flags, map[string]any{
				"band": band, "ssid": info["SSID"], "key": shown, "revealed": reveal,
			})
		},
	}
	cmd.Flags().StringVar(&band, "band", "2.4", "Which network to act on: 2.4, 5, or guest")
	cmd.Flags().BoolVar(&reveal, "reveal", false, "Print the key instead of masking it")
	return cmd
}

func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
}

func newWifiQRCmd(flags *rootFlags) *cobra.Command {
	var band string
	var pngPath string
	cmd := &cobra.Command{
		Use:   "qr",
		Short: "Print the Wi-Fi join string for a network as a scannable QR code",
		Long: strings.Trim(`
Print the standard Wi-Fi join payload for a network, and render it as a QR code
in the terminal.

The payload contains the network's pre-shared key in clear text, which is what
makes it scannable. Do not paste the output into a shared channel.
`, "\n"),
		Example:     "  fritzbox-pp-cli wifi qr --band guest",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "build a Wi-Fi join QR code", map[string]any{"band": band})
			}
			svc, err := wifiServiceFor(band)
			if err != nil {
				return err
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			info, err := box.Call(ctx, svc, "GetInfo", nil)
			if err != nil {
				return err
			}
			keys, err := box.Call(ctx, svc, "GetSecurityKeys", nil)
			if err != nil {
				return err
			}
			payload := wifiJoinPayload(info["SSID"], keys["KeyPassphrase"], info["BeaconType"])
			if pngPath != "" {
				if err := writeQRPNG(pngPath, payload, 512); err != nil {
					return err
				}
				return fbEmitObject(cmd, flags, map[string]any{"ssid": info["SSID"], "written": pngPath})
			}
			out := cmd.OutOrStdout()
			if !wantsHumanTable(out, flags) {
				return printJSONFiltered(out, map[string]any{"ssid": info["SSID"], "payload": payload}, flags)
			}
			fmt.Fprintf(out, "%s\n\n", payload)
			return renderQR(out, payload)
		},
	}
	cmd.Flags().StringVar(&band, "band", "2.4", "Which network to act on: 2.4, 5, or guest")
	cmd.Flags().StringVar(&pngPath, "png", "", "Write the QR code to this path as a PNG instead of drawing it")
	return cmd
}

// wifiJoinPayload builds the WIFI: join string phone cameras understand.
//
// Backslash, semicolon, comma, colon and double quote are the reserved
// characters in that format; leaving them unescaped produces a code that scans
// into a truncated password rather than failing visibly.
func wifiJoinPayload(ssid, key, beaconType string) string {
	auth := "WPA"
	if key == "" {
		auth = "nopass"
	} else if strings.Contains(strings.ToUpper(beaconType), "WEP") {
		auth = "WEP"
	}
	return fmt.Sprintf("WIFI:T:%s;S:%s;P:%s;;", auth, escapeWifiField(ssid), escapeWifiField(key))
}

func escapeWifiField(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\', ';', ',', ':', '"':
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ------------------------------------------------------------------- wan ----

func newWanStatusCmd(flags *rootFlags) *cobra.Command {
	var ipOnly bool
	cmd := &cobra.Command{
		Use:         "status",
		Short:       "Show the internet connection state, external address, and uptime",
		Example:     "  fritzbox-pp-cli wan status --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read the internet connection state", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			svc, info, err := box.wanService(ctx)
			if err != nil {
				return err
			}
			if ipOnly {
				fmt.Fprintln(cmd.OutOrStdout(), info["ExternalIPAddress"])
				return nil
			}
			return fbEmitObject(cmd, flags, map[string]any{
				"service":            svc,
				"status":             info["ConnectionStatus"],
				"connection_type":    info["ConnectionType"],
				"external_ip":        info["ExternalIPAddress"],
				"uptime_seconds":     info["Uptime"],
				"uptime_human":       humanUptime(info["Uptime"]),
				"last_error":         info["LastConnectionError"],
				"upstream_max_bps":   info["UpstreamMaxBitRate"],
				"downstream_max_bps": info["DownstreamMaxBitRate"],
			})
		},
	}
	cmd.Flags().BoolVar(&ipOnly, "ip-only", false, "Print just the external IP address, for piping")
	return cmd
}

func newWanReconnectCmd(flags *rootFlags) *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{
		Use:     "reconnect",
		Short:   "Force the router to re-establish the internet connection",
		Long:    "Drop and re-establish the internet connection, which usually yields a new external address. Prints what would happen unless --confirm is passed.",
		Example: "  fritzbox-pp-cli wan reconnect --confirm",
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "force an internet reconnection", nil)
			}
			if !confirm {
				return fbDryRun(cmd, flags, "force an internet reconnection (pass --confirm to actually do it)", nil)
			}
			if refused, err := fbRefuseUnderHarness(cmd, flags, "drop the internet connection"); refused {
				return err
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			svc, before, err := box.wanService(ctx)
			if err != nil {
				return err
			}
			if _, err := box.Call(ctx, svc, "ForceTermination", nil); err != nil {
				return err
			}
			return fbEmitObject(cmd, flags, map[string]any{
				"reconnecting": true,
				"previous_ip":  before["ExternalIPAddress"],
				"note":         "the connection takes a few seconds to come back; run 'wan status' to see the new address",
			})
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Actually reconnect instead of printing what would happen")
	return cmd
}

func newWanStatsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "stats",
		Short:       "Show WAN byte and packet counters and the link rate",
		Example:     "  fritzbox-pp-cli wan stats --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read WAN counters", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			row := map[string]any{}
			if props, err := box.Call(ctx, "WANCommonInterfaceConfig1", "GetCommonLinkProperties", nil); err == nil {
				row["access_type"] = props["WANAccessType"]
				row["physical_link"] = props["PhysicalLinkStatus"]
				row["upstream_max_bps"] = props["Layer1UpstreamMaxBitRate"]
				row["downstream_max_bps"] = props["Layer1DownstreamMaxBitRate"]
			}
			for action, key := range map[string]string{
				"GetTotalBytesSent":       "bytes_sent",
				"GetTotalBytesReceived":   "bytes_received",
				"GetTotalPacketsSent":     "packets_sent",
				"GetTotalPacketsReceived": "packets_received",
			} {
				if out, err := box.Call(ctx, "WANCommonInterfaceConfig1", action, nil); err == nil {
					for _, v := range out {
						row[key] = v
						break
					}
				}
			}
			if len(row) == 0 {
				return apiErr(fmt.Errorf("router returned no WAN counters"))
			}
			return fbEmitObject(cmd, flags, row)
		},
	}
	return cmd
}

func newWanLinkCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "link",
		Short:       "Show the physical WAN link state",
		Example:     "  fritzbox-pp-cli wan link --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read the physical WAN link state", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			out, err := box.Call(ctx, "WANCommonInterfaceConfig1", "GetCommonLinkProperties", nil)
			if err != nil {
				return err
			}
			return fbEmitObject(cmd, flags, soapToRow(out))
		},
	}
	return cmd
}

// ------------------------------------------------------------- dsl / lan ----

func newDslCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "dsl", Short: "DSL line metrics"}
	cmd.AddCommand(&cobra.Command{
		Use:         "status",
		Short:       "Show DSL sync rates, noise margin, and attenuation",
		Example:     "  fritzbox-pp-cli dsl status --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read DSL line metrics", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			out, err := box.Call(ctx, "WANDSLInterfaceConfig1", "GetInfo", nil)
			if err != nil {
				return err
			}
			row := soapToRow(out)
			// A fibre or cable line reports the DSL interface as disabled; say
			// so plainly rather than returning a wall of zeros.
			if out["Status"] == "Disabled" || out["Enable"] == "0" {
				row["note"] = "this router has no active DSL line; the WAN link is not DSL"
			}
			return fbEmitObject(cmd, flags, row)
		},
	})
	return cmd
}

func newLanCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "lan", Short: "Wired LAN interface state"}
	cmd.AddCommand(&cobra.Command{
		Use:         "status",
		Short:       "Show the wired LAN interface state and counters",
		Example:     "  fritzbox-pp-cli lan status --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read the wired LAN state", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			row := map[string]any{}
			if info, err := box.Call(ctx, "LANEthernetInterfaceConfig1", "GetInfo", nil); err == nil {
				for k, v := range info {
					row[strings.ToLower(k)] = v
				}
			}
			if stats, err := box.Call(ctx, "LANEthernetInterfaceConfig1", "GetStatistics", nil); err == nil {
				for k, v := range stats {
					row[strings.ToLower(k)] = v
				}
			}
			if len(row) == 0 {
				return apiErr(fmt.Errorf("router returned no LAN interface information"))
			}
			return fbEmitObject(cmd, flags, row)
		},
	})
	return cmd
}
