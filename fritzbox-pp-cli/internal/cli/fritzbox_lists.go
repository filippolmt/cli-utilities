// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// The generator promotes each single-endpoint resource to a bare leaf command,
// so `hosts`, `tam`, and `dect` are the raw data-path passthroughs. These
// `list` subcommands sit alongside them with decoded fields and real filters,
// which is the shape both users and agents reach for first.
func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		if parent := fbFind(root, "hosts"); parent != nil {
			fbAttach(parent, newHostsListCmd(flags))
		}
		if parent := fbFind(root, "tam"); parent != nil {
			fbAttach(parent, newTamListCmd(flags))
		}
		if parent := fbFind(root, "dect"); parent != nil {
			fbAttach(parent, newDectListCmd(flags))
		}
	})
}

func newHostsListCmd(flags *rootFlags) *cobra.Command {
	var onlineOnly, countOnly bool
	var medium string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List devices known to the router, with filters",
		Long: `List the devices the router knows about.

Devices are listed online first, then alphabetically. Filter by connection
medium with --medium, restrict to devices currently online with --online, or
ask for just a total with --count.`,
		Example:     "  fritzbox-pp-cli hosts list --online --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "list the devices known to the router", nil)
			}
			switch strings.ToLower(medium) {
			case "", "all", "wifi", "ethernet", "guest", "unknown":
			default:
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--medium must be one of all, wifi, ethernet, guest, or unknown"))
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
			rows := make([]map[string]any, 0, len(hosts))
			online := 0
			for _, h := range hosts {
				if onlineOnly && !h.Online() {
					continue
				}
				if m := strings.ToLower(medium); m != "" && m != "all" && h.Medium() != m {
					continue
				}
				if h.Online() {
					online++
				}
				rows = append(rows, h.row())
			}
			if countOnly {
				return fbEmitObject(cmd, flags, map[string]any{
					"total": len(rows), "online": online, "medium": strings.ToLower(medium),
				})
			}
			return fbEmit(cmd, flags, rows, "The router knows no devices matching that filter.")
		},
	}
	cmd.Flags().BoolVar(&onlineOnly, "online", false, "Only list devices the router currently sees")
	cmd.Flags().BoolVar(&countOnly, "count", false, "Print totals instead of the device list")
	cmd.Flags().StringVar(&medium, "medium", "", "Only list devices on this medium: wifi, ethernet, guest, or unknown")
	return cmd
}

func newTamListCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "list",
		Short:       "List the configured answering machines and whether each is active",
		Example:     "  fritzbox-pp-cli tam list --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "list the answering machines", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			res, err := box.web.Query(ctx, map[string]string{"tam": "tam:settings/TAM/list(Name,Active,Display)"})
			if err != nil {
				return fbErr(err)
			}
			var entries []struct {
				Name    string `json:"Name"`
				Active  string `json:"Active"`
				Display string `json:"Display"`
			}
			if err := json.Unmarshal(res["tam"], &entries); err != nil {
				return apiErr(fmt.Errorf("parsing the answering-machine list: %w", err))
			}
			rows := make([]map[string]any, 0, len(entries))
			for i, e := range entries {
				// Only the slots the router surfaces in its own interface are
				// worth listing; the rest are unconfigured placeholders.
				if e.Display != "1" {
					continue
				}
				rows = append(rows, map[string]any{
					"index": i, "name": e.Name, "active": e.Active == "1",
				})
			}
			return fbEmit(cmd, flags, rows, "No answering machines are configured.")
		},
	}
	return cmd
}

func newDectListCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "list",
		Short:       "List the registered DECT handsets",
		Example:     "  fritzbox-pp-cli dect list --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "list the DECT handsets", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			res, err := box.web.Query(ctx, map[string]string{"dect": "dect:settings/Handset/list(Name,Id)"})
			if err != nil {
				return fbErr(err)
			}
			var entries []struct {
				Name string `json:"Name"`
				ID   string `json:"Id"`
			}
			if err := json.Unmarshal(res["dect"], &entries); err != nil {
				return apiErr(fmt.Errorf("parsing the handset list: %w", err))
			}
			rows := make([]map[string]any, 0, len(entries))
			for _, e := range entries {
				// Unregistered slots come back with an empty name.
				if strings.TrimSpace(e.Name) == "" {
					continue
				}
				rows = append(rows, map[string]any{"id": e.ID, "name": e.Name})
			}
			return fbEmit(cmd, flags, rows, "No DECT handsets are registered.")
		},
	}
	return cmd
}
