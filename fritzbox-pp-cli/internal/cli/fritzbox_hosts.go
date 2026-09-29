// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// hostListExpr is the data-path expression that returns every device the router
// knows, online and offline. The field list is fixed here rather than exposed,
// because every consumer in this package depends on these names.
const hostListExpr = "landevice:settings/landevice/list(name,ip,mac,active,UID,guest,ethernet,wlan)"

// fbHost is one device known to the router.
type fbHost struct {
	UID      string `json:"uid"`
	Name     string `json:"name"`
	IP       string `json:"ip"`
	MAC      string `json:"mac"`
	Active   string `json:"active"`
	Guest    string `json:"guest"`
	Ethernet string `json:"ethernet"`
	WLAN     string `json:"wlan"`
}

// Online reports whether the router currently sees the device.
func (h fbHost) Online() bool { return h.Active == "1" }

// Medium reports how the device is attached.
func (h fbHost) Medium() string {
	switch {
	case h.Guest == "1":
		return "guest"
	case h.Ethernet == "1":
		return "ethernet"
	case h.WLAN == "1":
		return "wifi"
	default:
		return "unknown"
	}
}

func (h fbHost) row() map[string]any {
	return map[string]any{
		"name": h.Name, "ip": h.IP, "mac": h.MAC,
		"online": h.Online(), "medium": h.Medium(), "uid": h.UID,
	}
}

// Hosts returns every device the router knows about.
func (b *fbBox) Hosts(ctx context.Context) ([]fbHost, error) {
	res, err := b.web.Query(ctx, map[string]string{"hosts": hostListExpr})
	if err != nil {
		return nil, fbErr(err)
	}
	raw, ok := res["hosts"]
	if !ok {
		return nil, apiErr(fmt.Errorf("router returned no device list"))
	}
	var hosts []fbHost
	if err := json.Unmarshal(raw, &hosts); err != nil {
		return nil, apiErr(fmt.Errorf("parsing the router device list: %w", err))
	}
	sort.Slice(hosts, func(i, j int) bool {
		// Online first, then by name, so the interesting rows lead.
		if hosts[i].Online() != hosts[j].Online() {
			return hosts[i].Online()
		}
		return strings.ToLower(hosts[i].Name) < strings.ToLower(hosts[j].Name)
	})
	return hosts, nil
}

// findHost resolves a device by MAC, IP, or a case-insensitive name substring.
//
// An ambiguous name is an error rather than a silent first-match, because the
// caller may be about to block internet access for the wrong device.
func findHost(hosts []fbHost, needle string) (fbHost, error) {
	want := strings.ToLower(strings.TrimSpace(needle))
	if want == "" {
		return fbHost{}, usageErr(fmt.Errorf("a device name, IP address, or MAC address is required"))
	}
	var partial []fbHost
	for _, h := range hosts {
		if strings.EqualFold(h.MAC, want) || h.IP == needle || strings.EqualFold(h.Name, needle) || h.UID == needle {
			return h, nil
		}
		if strings.Contains(strings.ToLower(h.Name), want) {
			partial = append(partial, h)
		}
	}
	switch len(partial) {
	case 0:
		return fbHost{}, notFoundErr(fmt.Errorf("no device matching %q; run 'fritzbox-pp-cli hosts list' to see what the router knows", needle))
	case 1:
		return partial[0], nil
	default:
		names := make([]string, 0, len(partial))
		for _, h := range partial {
			names = append(names, h.Name)
		}
		return fbHost{}, usageErr(fmt.Errorf("%q matches %d devices (%s); use the MAC or IP address instead", needle, len(partial), strings.Join(names, ", ")))
	}
}

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		parent := fbFind(root, "hosts")
		if parent == nil {
			parent = &cobra.Command{Use: "hosts", Short: "Devices known to the router"}
			fbAttach(root, parent)
		}
		for _, c := range []*cobra.Command{
			newHostsGetCmd(flags), newHostsBlockCmd(flags, true), newHostsBlockCmd(flags, false),
			newHostsProfilesCmd(flags), newHostsSetProfileCmd(flags), newHostsCleanupCmd(flags),
		} {
			fbAttach(parent, c)
		}
	})
}

func newHostsGetCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "get <device>",
		Short:       "Show one device by name, IP address, or MAC address",
		Example:     "  fritzbox-pp-cli hosts get 192.168.178.24 --agent",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:happy-args": "device=192.168.178.1"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "look up one device", nil)
			}
			if len(args) == 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("a device name, IP address, or MAC address is required"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			hosts, err := box.Hosts(ctx)
			if err != nil {
				return err
			}
			host, err := findHost(hosts, args[0])
			if err != nil {
				return err
			}
			row := host.row()
			// Internet access state lives on a different service and is only
			// meaningful for a device with an address, so it is best-effort.
			if host.IP != "" {
				if out, err := box.Call(ctx, "X_AVM-DE_HostFilter1", "GetWANAccessByIP", map[string]string{"IPv4Address": host.IP}); err == nil {
					row["wan_access"] = out["WANAccess"]
					row["wan_access_blocked"] = strings.EqualFold(out["WANAccess"], "disabled")
				}
			}
			return fbEmitObject(cmd, flags, row)
		},
	}
	return cmd
}

func newHostsBlockCmd(flags *rootFlags, block bool) *cobra.Command {
	verb, use := "unblock", "unblock"
	if block {
		verb, use = "block", "block"
	}
	var confirm bool
	cmd := &cobra.Command{
		Use:   use + " <device>",
		Short: fmt.Sprintf("%s a device's internet access", strings.ToUpper(verb[:1])+verb[1:]),
		Long: fmt.Sprintf(`%s a device's internet access by name, IP address, or MAC address.

The device keeps its local network access either way; only the route to the
internet changes. Prints what would happen unless --confirm is passed.`,
			strings.ToUpper(verb[:1])+verb[1:]),
		Example:     fmt.Sprintf("  fritzbox-pp-cli hosts %s laptop --confirm", use),
		Annotations: map[string]string{"pp:happy-args": "device=192.168.178.1"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, verb+" a device's internet access", nil)
			}
			if len(args) == 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("a device name, IP address, or MAC address is required"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			hosts, err := box.Hosts(ctx)
			if err != nil {
				return err
			}
			host, err := findHost(hosts, args[0])
			if err != nil {
				return err
			}
			if host.IP == "" || net.ParseIP(host.IP) == nil {
				return apiErr(fmt.Errorf("device %q has no current IP address, so its internet access cannot be changed", host.Name))
			}
			action := fmt.Sprintf("%s internet access for %s (%s)", verb, host.Name, host.IP)
			if !confirm {
				return fbDryRun(cmd, flags, action+" (pass --confirm to actually do it)", map[string]any{"device": host.Name, "ip": host.IP})
			}
			if refused, err := fbRefuseUnderHarness(cmd, flags, action); refused {
				return err
			}
			disallow := "0"
			if block {
				disallow = "1"
			}
			if _, err := box.Call(ctx, "X_AVM-DE_HostFilter1", "DisallowWANAccessByIP", map[string]string{
				"IPv4Address": host.IP, "Disallow": disallow,
			}); err != nil {
				return err
			}
			return fbEmitObject(cmd, flags, map[string]any{
				"device": host.Name, "ip": host.IP, "blocked": block, "changed": true,
			})
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Actually apply the change instead of printing what would happen")
	return cmd
}

func newHostsProfilesCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "profiles",
		Short:       "List the parental-control access profiles defined on the router",
		Example:     "  fritzbox-pp-cli hosts profiles --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "list the access profiles", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			out, err := box.Call(ctx, "X_AVM-DE_HostFilter1", "GetFilterProfiles", nil)
			if err != nil {
				return err
			}
			rows := parseProfileList(out["FilterProfileList"])
			if len(rows) == 0 && len(out) > 0 {
				// Firmware differences in the envelope shape should surface the
				// raw payload rather than an empty, confident-looking table.
				return fbEmitObject(cmd, flags, soapToRow(out))
			}
			return fbEmit(cmd, flags, rows, "This router has no access profiles defined.")
		},
	}
	return cmd
}

// parseProfileList decodes the FilterProfileList XML document FRITZ!OS returns
// from GetFilterProfiles. The built-in profiles carry an empty name, so the
// profile type stands in for it.
func parseProfileList(raw string) []map[string]any {
	var doc struct {
		Profiles []struct {
			ID   string `xml:"FilterProfileID"`
			Name string `xml:"Name"`
			Type string `xml:"FilterProfileType"`
		} `xml:"FilterProfile"`
	}
	if err := xml.Unmarshal([]byte(strings.TrimSpace(raw)), &doc); err != nil {
		return nil
	}
	rows := make([]map[string]any, 0, len(doc.Profiles))
	for _, p := range doc.Profiles {
		name := p.Name
		if name == "" {
			name = p.Type
		}
		rows = append(rows, map[string]any{"id": p.ID, "name": name, "type": p.Type})
	}
	return rows
}

func newHostsSetProfileCmd(flags *rootFlags) *cobra.Command {
	var profileID string
	var confirm bool
	cmd := &cobra.Command{
		Use:         "set-profile <device>",
		Short:       "Assign a parental-control access profile to a device",
		Example:     "  fritzbox-pp-cli hosts set-profile laptop --profile 1 --confirm",
		Annotations: map[string]string{"pp:happy-args": "device=192.168.178.1;--profile=1"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "assign an access profile", nil)
			}
			if len(args) == 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("a device name, IP address, or MAC address is required"))
			}
			if profileID == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--profile is required; run 'fritzbox-pp-cli hosts profiles' to list the available ids"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			hosts, err := box.Hosts(ctx)
			if err != nil {
				return err
			}
			host, err := findHost(hosts, args[0])
			if err != nil {
				return err
			}
			action := fmt.Sprintf("assign profile %s to %s", profileID, host.Name)
			if !confirm {
				return fbDryRun(cmd, flags, action+" (pass --confirm to actually do it)", map[string]any{"device": host.Name, "profile": profileID})
			}
			if refused, err := fbRefuseUnderHarness(cmd, flags, action); refused {
				return err
			}
			if _, err := box.Call(ctx, "X_AVM-DE_HostFilter1", "AddHostEntryToFilterProfile", map[string]string{
				"UID": profileID, "Type": "0", "HostEntry": host.MAC,
			}); err != nil {
				return err
			}
			return fbEmitObject(cmd, flags, map[string]any{"device": host.Name, "profile": profileID, "changed": true})
		},
	}
	cmd.Flags().StringVar(&profileID, "profile", "", "Access profile id from 'hosts profiles'")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Actually apply the change instead of printing what would happen")
	return cmd
}

// netDevParams asks the web UI's netDev page for its device lists; without an
// xhrId the page returns no data.
var netDevParams = map[string]string{"xhrId": "all", "useajax": "1"}

func newHostsCleanupCmd(flags *rootFlags) *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Remove the offline devices from the router's device list",
		Long: `Remove every device the router lists as unused, like the web UI's
"remove unused connections" button. Devices with custom settings (a fixed IP,
a profile) are kept by the router; removed devices that come back online simply
reappear.

Prints what would be removed unless --confirm is passed, and refuses while a
verification harness is active. TR-064 has no action for this, so it goes
through the web UI.`,
		Example: "  fritzbox-pp-cli hosts cleanup --confirm",
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "remove the unused devices from the device list", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			raw, err := box.web.Data(ctx, "netDev", netDevParams)
			if err != nil {
				return fbErr(err)
			}
			before, err := parseUnusedHosts(raw)
			if err != nil {
				return err
			}
			if !confirm {
				return fbEmit(cmd, flags, before, "No unused devices to remove.")
			}
			if refused, err := fbRefuseUnderHarness(cmd, flags, "remove the unused devices"); refused {
				return err
			}
			// The UI's button submits an empty "cleanup" field; the router may
			// answer "confirm", which the UI resubmits with "confirmed".
			form := map[string]string{"cleanup": ""}
			for attempt := 0; attempt < 2; attempt++ {
				answer, err := box.web.Data(ctx, "netDev", form)
				if err != nil {
					return fbErr(err)
				}
				if status, _ := jsonPath(answer, "data", "cleanup"); string(status) != `"confirm"` {
					break
				}
				form["confirmed"] = ""
			}
			// The reply carries no result, and the router drops the devices a
			// moment later, so the list is re-read until it shrinks.
			var after []map[string]any
			for attempt := 0; attempt < 5; attempt++ {
				time.Sleep(time.Second)
				raw, err = box.web.Data(ctx, "netDev", netDevParams)
				if err != nil {
					return fbErr(err)
				}
				if after, err = parseUnusedHosts(raw); err != nil {
					return err
				}
				if len(after) < len(before) {
					break
				}
			}
			if len(before) > 0 && len(after) == len(before) {
				return apiErr(fmt.Errorf("the router removed none of the %d unused devices; this firmware may not support the cleanup request", len(before)))
			}
			return fbEmitObject(cmd, flags, map[string]any{"removed": len(before) - len(after), "kept": after})
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Actually remove the devices instead of listing them")
	return cmd
}

// parseUnusedHosts reads the offline ("passive") devices from the netDev page.
func parseUnusedHosts(raw json.RawMessage) ([]map[string]any, error) {
	list, ok := jsonPath(raw, "data", "passive")
	if !ok {
		return nil, apiErr(fmt.Errorf("router returned no device list"))
	}
	var devices []struct {
		UID     string `json:"UID"`
		Name    string `json:"name"`
		MAC     string `json:"mac"`
		Options struct {
			Deleteable bool `json:"deleteable"`
		} `json:"options"`
		IPv4 struct {
			IP string `json:"ip"`
		} `json:"ipv4"`
	}
	if err := json.Unmarshal(list, &devices); err != nil {
		return nil, apiErr(fmt.Errorf("parsing the device list: %w", err))
	}
	rows := make([]map[string]any, 0, len(devices))
	for _, d := range devices {
		rows = append(rows, map[string]any{
			"uid": d.UID, "name": d.Name, "mac": d.MAC, "ip": d.IPv4.IP, "removable": d.Options.Deleteable,
		})
	}
	return rows, nil
}
