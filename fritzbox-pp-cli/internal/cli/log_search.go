// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

// pp:data-source auto — searches retained entries, fetching the live buffer once when empty.
// pp:client-call — the API is reached through the fbBox wrapper in
// fritzbox_core.go rather than through flags.newClient(), so the static
// check cannot see the call site.

package cli

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"fritzbox-pp-cli/internal/cliutil"

	"github.com/spf13/cobra"
)

type logSearchRow struct {
	At       string `json:"at"`
	Category string `json:"category"`
	Message  string `json:"message"`
}

func newNovelLogSearchCmd(flags *rootFlags) *cobra.Command {
	var since string
	var limit int
	var category string
	var dbPath string

	cmd := &cobra.Command{
		Use:   "search <term>",
		Short: "Search retained router log entries, including ones the router has rotated away",
		Long: `Full-text search the router log entries this CLI has retained locally.

The router keeps a fixed-size buffer and discards older lines. Every run of
'log tail' and 'snapshot' appends what it sees into the local database, so this
command reaches further back than the router itself can.

Use this command to search log history. Do NOT use it to watch live output; use
'log tail' instead.`,
		Example:     "  fritzbox-pp-cli log search dsl --since 7d --agent",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:happy-args": "term=a", "pp:no-error-path-probe": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "search the retained router log", map[string]any{"since": since})
			}
			if len(args) == 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("a search term is required"))
			}
			window, err := cliutil.ParseDurationLoose(since)
			if err != nil {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--since %q is not a duration; use forms like 90m, 24h, 7d, or 2w", since))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			if dbPath == "" {
				dbPath = defaultDBPath("fritzbox-pp-cli")
			}
			results := make([]logSearchRow, 0)
			if _, statErr := os.Stat(dbPath); os.IsNotExist(statErr) {
				fmt.Fprintf(cmd.ErrOrStderr(), "no local mirror at %s\nrun: fritzbox-pp-cli log tail\n", dbPath)
				if !wantsHumanTable(cmd.OutOrStdout(), flags) {
					return printJSONFiltered(cmd.OutOrStdout(), results, flags)
				}
				return nil
			}
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			db, err := box.openStore(ctx, dbPath)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			// On a fresh install the retained table is empty, so a first search
			// would return nothing even though the router is holding a full
			// buffer. Pull and retain that buffer once, then search it: the
			// command works out of the box and the store starts accumulating.
			var retained int
			if err := db.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM fb_log_entries`).Scan(&retained); err != nil {
				return fmt.Errorf("counting retained log entries: %w", err)
			}
			if retained == 0 {
				if entries, fetchErr := box.LogEntries(ctx); fetchErr == nil {
					if err := box.recordLog(ctx, entries); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not retain the current log buffer: %v\n", err)
					}
				} else {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: no retained log entries and the live buffer could not be read: %v\n", fetchErr)
				}
			}

			cutoff := time.Now().Add(-window).Unix()
			// The FTS table is joined by rowid rather than queried directly so
			// the time and category filters stay on the indexed base table.
			query := `
SELECT e.logged_at, e.category, e.message
FROM fb_log_fts f
JOIN fb_log_entries e ON e.rowid = f.rowid
WHERE fb_log_fts MATCH ? AND e.logged_at >= ?`
			params := []any{ftsQuery(args[0]), cutoff}
			if category != "" {
				query += " AND e.category = ?"
				params = append(params, category)
			}
			query += " ORDER BY e.logged_at DESC"
			if limit > 0 {
				query += " LIMIT ?"
				params = append(params, limit)
			}
			rows, err := db.DB().QueryContext(ctx, query, params...)
			if err != nil {
				// FTS5 is an optional SQLite module and its virtual table can
				// fail to construct on a database whose shadow tables were
				// written by a different build. Falling back to a scan keeps
				// the command working; the base table is the source of truth
				// either way, so only ranking is lost.
				fallback := `
SELECT e.logged_at, e.category, e.message
FROM fb_log_entries e
WHERE e.message LIKE ? AND e.logged_at >= ?`
				fallbackParams := []any{"%" + args[0] + "%", cutoff}
				if category != "" {
					fallback += " AND e.category = ?"
					fallbackParams = append(fallbackParams, category)
				}
				fallback += " ORDER BY e.logged_at DESC"
				if limit > 0 {
					fallback += " LIMIT ?"
					fallbackParams = append(fallbackParams, limit)
				}
				rows, err = db.DB().QueryContext(ctx, fallback, fallbackParams...)
				if err != nil {
					return fmt.Errorf("searching the retained log: %w", err)
				}
			}
			for rows.Next() {
				var at sql.NullInt64
				var cat, msg sql.NullString
				if err := rows.Scan(&at, &cat, &msg); err != nil {
					_ = rows.Close()
					return fmt.Errorf("reading a log row: %w", err)
				}
				stamp := ""
				if at.Valid && at.Int64 > 0 {
					stamp = time.Unix(at.Int64, 0).Format(time.RFC3339)
				}
				results = append(results, logSearchRow{At: stamp, Category: cat.String, Message: msg.String})
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				return fmt.Errorf("iterating log rows: %w", err)
			}
			if err := rows.Close(); err != nil {
				return fmt.Errorf("closing log rows: %w", err)
			}

			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(), results, flags)
			}
			out := cmd.OutOrStdout()
			if len(results) == 0 {
				fmt.Fprintf(out, "No retained log entry matched %q in the last %s.\n", args[0], since)
				return nil
			}
			for _, r := range results {
				fmt.Fprintf(out, "%s  %-6s %s\n", r.At, r.Category, r.Message)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "7d", "How far back to search, for example 90m, 24h, 7d, 2w")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum number of matches to return; 0 means all")
	cmd.Flags().StringVar(&category, "category", "", "Only search entries in this category, for example sys, wlan, or fon")
	cmd.Flags().StringVar(&dbPath, "db", "", "Database path")
	return cmd
}

// ftsQuery turns a user term into an FTS5 expression.
//
// FTS5 treats characters such as '-' and '"' as syntax, so a bare user string
// can be a syntax error rather than a search. Quoting makes the term literal
// and a trailing '*' keeps prefix matching, which is what users expect.
func ftsQuery(term string) string {
	term = strings.TrimSpace(term)
	if term == "" {
		return `""`
	}
	escaped := strings.ReplaceAll(term, `"`, `""`)
	return `"` + escaped + `"*`
}
