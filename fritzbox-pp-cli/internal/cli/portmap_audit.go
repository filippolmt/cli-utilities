// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

// pp:data-source live — reads the live forwarding table and device list, then joins them.
// pp:client-call — the API is reached through the fbBox wrapper in
// fritzbox_core.go rather than through flags.newClient(), so the static
// check cannot see the call site.

package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

type portmapAuditRow struct {
	Index        int    `json:"index"`
	Description  string `json:"description"`
	Protocol     string `json:"protocol"`
	ExternalPort string `json:"external_port"`
	InternalHost string `json:"internal_host"`
	InternalPort string `json:"internal_port"`
	Enabled      bool   `json:"enabled"`
	Verdict      string `json:"verdict"`
	Reason       string `json:"reason,omitempty"`
}

func newNovelPortmapAuditCmd(flags *rootFlags) *cobra.Command {
	var staleOnly bool

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Flag port forwarding rules whose target host is no longer known",
		Long: `Cross-reference every port forwarding rule against the devices the router knows
about, and flag the rules pointing somewhere that no longer exists.

A forward left behind after a device was replaced or re-addressed keeps a hole
open to whatever now answers on that address, which is why this is worth
checking after any network cleanup.`,
		Example:     "  fritzbox-pp-cli portmap audit --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "audit the port forwarding rules", nil)
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
			hosts, err := box.Hosts(ctx)
			if err != nil {
				return err
			}
			byIP := make(map[string]fbHost, len(hosts))
			for _, h := range hosts {
				if h.IP != "" {
					byIP[h.IP] = h
				}
			}

			rows := make([]portmapAuditRow, 0, len(mappings))
			stale := 0
			for _, m := range mappings {
				row := portmapAuditRow{
					Index: m.Index, Description: m.Description, Protocol: m.Protocol,
					ExternalPort: m.ExternalPort, InternalHost: m.InternalHost,
					InternalPort: m.InternalPort, Enabled: m.Enabled,
				}
				host, known := byIP[m.InternalHost]
				switch {
				case !known:
					row.Verdict = "stale"
					row.Reason = "no device with this address is known to the router"
					stale++
				case !host.Online():
					row.Verdict = "check"
					row.Reason = fmt.Sprintf("%s is known but currently offline", host.Name)
				default:
					row.Verdict = "ok"
					row.Reason = host.Name
				}
				if staleOnly && row.Verdict == "ok" {
					continue
				}
				rows = append(rows, row)
			}

			out := cmd.OutOrStdout()
			if !wantsHumanTable(out, flags) {
				return printJSONFiltered(out, rows, flags)
			}
			if len(mappings) == 0 {
				fmt.Fprintln(out, "No port forwarding rules are configured.")
				return nil
			}
			if len(rows) == 0 {
				fmt.Fprintln(out, "Every port forwarding rule points at a known device.")
				return nil
			}
			for _, r := range rows {
				fmt.Fprintf(out, "%-8s %-6s %-8s -> %-16s %-6s  %s\n",
					strings.ToUpper(r.Verdict), r.Protocol, r.ExternalPort, r.InternalHost, r.InternalPort, r.Reason)
			}
			if stale > 0 {
				fmt.Fprintf(out, "\n%d rule(s) point at an address the router no longer knows.\n", stale)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&staleOnly, "stale-only", false, "Only report rules that need attention")
	return cmd
}
