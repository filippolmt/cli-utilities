// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

// pp:data-source live — reads the router's power monitor.
// pp:client-call — the API is reached through the fbBox wrapper in
// fritzbox_core.go rather than through flags.newClient(), so the static
// check cannot see the call site.

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/spf13/cobra"
)

// energyDrain is one subsystem's share of the router's power budget.
type energyDrain struct {
	Name       string `json:"name"`
	ActPercent int    `json:"act_percent"`
	CumPercent int    `json:"cum_percent"`
	Status     string `json:"status,omitempty"`
}

// EnergyDrain reads the router's per-subsystem power monitor.
//
// The status field arrives as either a single string or an array of strings
// depending on how many notes the subsystem has, so it is decoded permissively
// rather than pinned to one shape.
func (b *fbBox) EnergyDrain(ctx context.Context) ([]energyDrain, error) {
	raw, err := b.web.Data(ctx, "energy", nil)
	if err != nil {
		return nil, fbErr(err)
	}
	list, ok := jsonPath(raw, "data", "drain")
	if !ok {
		return nil, apiErr(fmt.Errorf("router returned no power-monitor data"))
	}
	var rows []struct {
		Name     string          `json:"name"`
		ActPerc  int             `json:"actPerc"`
		CumPerc  int             `json:"cumPerc"`
		Statuses json.RawMessage `json:"statuses"`
	}
	if err := json.Unmarshal(list, &rows); err != nil {
		return nil, apiErr(fmt.Errorf("parsing the power monitor: %w", err))
	}
	out := make([]energyDrain, 0, len(rows))
	for _, r := range rows {
		out = append(out, energyDrain{
			Name: r.Name, ActPercent: r.ActPerc, CumPercent: r.CumPerc,
			Status: flattenStatuses(r.Statuses),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ActPercent > out[j].ActPercent })
	return out, nil
}

// flattenStatuses accepts either a JSON string or an array of strings.
func flattenStatuses(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return single
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err == nil {
		joined := ""
		for i, s := range many {
			if i > 0 {
				joined += "; "
			}
			joined += s
		}
		return joined
	}
	return ""
}

func newNovelEnergyCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "energy",
		Short: "Show power draw broken down by router subsystem",
		Long: `Show how the router's power budget is split across its subsystems.

Each snapshot is also recorded locally by 'snapshot', so the share taken by the
wireless radios or the DSL line can be compared over time.`,
		Example:     "  fritzbox-pp-cli energy --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read the router power monitor", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			drains, err := box.EnergyDrain(ctx)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(drains))
			for _, d := range drains {
				rows = append(rows, map[string]any{
					"subsystem": d.Name, "current_percent": d.ActPercent,
					"average_percent": d.CumPercent, "status": d.Status,
				})
			}
			return fbEmit(cmd, flags, rows, "This router exposes no power monitor.")
		},
	}
	return cmd
}
