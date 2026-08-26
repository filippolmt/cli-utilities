// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

// pp:data-source live — reads the router's home-network topology.
// pp:client-call — the API is reached through the fbBox wrapper in
// fritzbox_core.go rather than through flags.newClient(), so the static
// check cannot see the call site.

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// meshNode is one device in the router's home-network topology.
type meshNode struct {
	UID      string      `json:"uid"`
	Name     string      `json:"name"`
	Kind     string      `json:"kind"`
	Parent   string      `json:"parent"`
	Distance int         `json:"distance"`
	Children []*meshNode `json:"children,omitempty"`
}

func newNovelMeshCmd(flags *rootFlags) *cobra.Command {
	var flat bool

	cmd := &cobra.Command{
		Use:   "mesh",
		Short: "Render the mesh tree of repeaters and their connected clients",
		Long: `Show the home-network topology as a tree: the router, any repeaters or powerline
adapters below it, and the clients attached to each.

This answers which access point a device is actually connected to, which no
single status call reports.`,
		Example:     "  fritzbox-pp-cli mesh --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read the home-network topology", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			raw, err := box.web.Data(ctx, "homeNet", nil)
			if err != nil {
				return err
			}
			list, ok := jsonPath(raw, "data", "devices")
			if !ok {
				return apiErr(fmt.Errorf("router returned no home-network topology"))
			}
			var entries []struct {
				UID      string `json:"UID"`
				Parent   string `json:"parent"`
				DevType  string `json:"devtype"`
				Dist     int    `json:"dist"`
				NameInfo struct {
					Name string `json:"name"`
				} `json:"nameinfo"`
			}
			if err := json.Unmarshal(list, &entries); err != nil {
				return apiErr(fmt.Errorf("parsing the home-network topology: %w", err))
			}

			nodes := make(map[string]*meshNode, len(entries))
			order := make([]string, 0, len(entries))
			for _, e := range entries {
				name := strings.TrimSpace(e.NameInfo.Name)
				if name == "" {
					name = e.UID
				}
				nodes[e.UID] = &meshNode{UID: e.UID, Name: name, Kind: e.DevType, Parent: e.Parent, Distance: e.Dist}
				order = append(order, e.UID)
			}

			if flat {
				rows := make([]map[string]any, 0, len(order))
				for _, uid := range order {
					n := nodes[uid]
					parentName := ""
					if p, ok := nodes[n.Parent]; ok {
						parentName = p.Name
					}
					rows = append(rows, map[string]any{
						"name": n.Name, "kind": n.Kind, "attached_to": parentName, "hops": n.Distance,
					})
				}
				return fbEmit(cmd, flags, rows, "The router reported no topology.")
			}

			var roots []*meshNode
			for _, uid := range order {
				n := nodes[uid]
				// A device whose parent is absent from the payload is treated
				// as a root, so an incomplete topology still renders instead of
				// silently dropping that whole branch.
				if parent, ok := nodes[n.Parent]; ok && n.Parent != n.UID {
					parent.Children = append(parent.Children, n)
					continue
				}
				roots = append(roots, n)
			}
			sortMesh(roots)

			out := cmd.OutOrStdout()
			if !wantsHumanTable(out, flags) {
				return printJSONFiltered(out, roots, flags)
			}
			if len(roots) == 0 {
				fmt.Fprintln(out, "The router reported no topology.")
				return nil
			}
			for _, r := range roots {
				printMeshNode(out, r, "")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&flat, "flat", false, "Print one row per device instead of a tree")
	return cmd
}

func sortMesh(nodes []*meshNode) {
	sort.Slice(nodes, func(i, j int) bool {
		// Infrastructure first, then by name, so repeaters lead their clients.
		if (len(nodes[i].Children) > 0) != (len(nodes[j].Children) > 0) {
			return len(nodes[i].Children) > 0
		}
		return strings.ToLower(nodes[i].Name) < strings.ToLower(nodes[j].Name)
	})
	for _, n := range nodes {
		sortMesh(n.Children)
	}
}

func printMeshNode(w io.Writer, n *meshNode, indent string) {
	label := n.Name
	if n.Kind != "" {
		label += "  (" + n.Kind + ")"
	}
	fmt.Fprintf(w, "%s%s\n", indent, label)
	for _, c := range n.Children {
		printMeshNode(w, c, indent+"  ")
	}
}
