// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

// pp:data-source auto — searches the stored catalog, reading it from the router when absent.
// pp:client-call — the API is reached through the fbBox wrapper in
// fritzbox_core.go rather than through flags.newClient(), so the static
// check cannot see the call site.

package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func newNovelActionsSearchCmd(flags *rootFlags) *cobra.Command {
	var refresh bool
	var dbPath string
	var limit int

	cmd := &cobra.Command{
		Use:   "search <term>",
		Short: "Search the TR-064 action catalog of your own firmware",
		Long: `Search every callable TR-064 action this router publishes, matching against
service names, action names, and argument names.

The catalog is read from the router's own service descriptors and cached
locally, so it reflects the exact firmware in front of you, including vendor
extensions, and works offline after the first run.`,
		Example:     "  fritzbox-pp-cli actions search reconnect --agent",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:happy-args": "term=get", "pp:no-error-path-probe": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "search the TR-064 action catalog", nil)
			}
			if len(args) == 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("a search term is required"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			db, err := box.openStore(ctx, dbPath)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			takenAt, err := latestCatalogSnapshot(ctx, db)
			if err != nil {
				return err
			}
			// An empty cache means the catalog has never been read; fetching it
			// silently is better than telling the user to run a sync first.
			if takenAt == 0 || refresh {
				takenAt, _, err = box.syncActionCatalog(ctx, db)
				if err != nil {
					return err
				}
			}
			catalog, err := loadCatalog(ctx, db, takenAt)
			if err != nil {
				return err
			}

			needle := strings.ToLower(args[0])
			matches := make([]catalogRow, 0)
			for _, row := range catalog {
				haystack := strings.ToLower(row.Service + " " + row.Action + " " + row.In + " " + row.Out)
				if strings.Contains(haystack, needle) {
					matches = append(matches, row)
				}
			}
			sort.Slice(matches, func(i, j int) bool {
				// An action-name hit is what the user usually meant, so those
				// rank above matches that only appear in an argument list.
				iName := strings.Contains(strings.ToLower(matches[i].Action), needle)
				jName := strings.Contains(strings.ToLower(matches[j].Action), needle)
				if iName != jName {
					return iName
				}
				if matches[i].Service != matches[j].Service {
					return matches[i].Service < matches[j].Service
				}
				return matches[i].Action < matches[j].Action
			})
			if limit > 0 && len(matches) > limit {
				matches = matches[:limit]
			}

			rows := make([]map[string]any, 0, len(matches))
			for _, m := range matches {
				rows = append(rows, map[string]any{
					"service": m.Service, "action": m.Action, "in": m.In, "out": m.Out,
					"call": fmt.Sprintf("fritzbox-pp-cli tr064 call %s %s", m.Service, m.Action),
				})
			}
			return fbEmitFrom(cmd, flags, "local", rows, fmt.Sprintf("No TR-064 action matched %q on this firmware.", args[0]))
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "Re-read the catalog from the router before searching")
	cmd.Flags().StringVar(&dbPath, "db", "", "Database path")
	cmd.Flags().IntVar(&limit, "limit", 40, "Maximum number of matches to return; 0 means all")
	return cmd
}
