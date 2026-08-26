// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"fritzbox-pp-cli/internal/store"
)

// catalogRow is one action from a stored service-descriptor snapshot.
type catalogRow struct {
	Service string `json:"service"`
	Action  string `json:"action"`
	In      string `json:"in"`
	Out     string `json:"out"`
}

// syncActionCatalog reads every service descriptor from the router and records
// the resulting action list as one dated snapshot.
//
// Snapshots are kept rather than overwritten: comparing two of them is what
// makes 'actions diff' able to report what a firmware update changed.
func (b *fbBox) syncActionCatalog(ctx context.Context, db *store.Store) (int64, int, error) {
	services, err := b.Services(ctx)
	if err != nil {
		return 0, 0, err
	}
	takenAt := time.Now().Unix()
	rows := make([][]any, 0, 512)
	for _, svc := range services {
		actions, err := b.tr.Actions(ctx, svc)
		if err != nil {
			// One unreadable descriptor must not discard the whole catalog.
			continue
		}
		for _, a := range actions {
			rows = append(rows, []any{
				takenAt, svc.Name(), svc.Type, svc.ControlURL, svc.SCPDURL, a.Name,
				strings.Join(argNames(a.In()), ","), strings.Join(argNames(a.Out()), ","),
			})
		}
	}
	if len(rows) == 0 {
		return 0, 0, apiErr(fmt.Errorf("router published no callable actions"))
	}
	if err := insertRows(ctx, db, "fb_actions",
		[]string{"snapshot_at", "service", "service_type", "control_url", "scpd_url", "action", "args_in", "args_out"},
		rows); err != nil {
		return 0, 0, err
	}
	return takenAt, len(rows), nil
}

// latestCatalogSnapshot returns the most recent stored snapshot timestamp.
func latestCatalogSnapshot(ctx context.Context, db *store.Store) (int64, error) {
	var at sql.NullInt64
	if err := db.DB().QueryRowContext(ctx, `SELECT MAX(snapshot_at) FROM fb_actions`).Scan(&at); err != nil {
		return 0, fmt.Errorf("reading the stored action catalog: %w", err)
	}
	if !at.Valid {
		return 0, nil
	}
	return at.Int64, nil
}

// catalogSnapshots lists stored snapshot timestamps, newest first.
func catalogSnapshots(ctx context.Context, db *store.Store) ([]int64, error) {
	rows, err := db.DB().QueryContext(ctx, `SELECT DISTINCT snapshot_at FROM fb_actions ORDER BY snapshot_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("listing catalog snapshots: %w", err)
	}
	var out []int64
	for rows.Next() {
		var at int64
		if err := rows.Scan(&at); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("reading a catalog snapshot: %w", err)
		}
		out = append(out, at)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterating catalog snapshots: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing catalog snapshots: %w", err)
	}
	return out, nil
}

// loadCatalog returns every action recorded in one snapshot, keyed by
// "Service.Action".
func loadCatalog(ctx context.Context, db *store.Store, takenAt int64) (map[string]catalogRow, error) {
	rows, err := db.DB().QueryContext(ctx,
		`SELECT service, action, args_in, args_out FROM fb_actions WHERE snapshot_at = ?`, takenAt)
	if err != nil {
		return nil, fmt.Errorf("reading catalog snapshot %d: %w", takenAt, err)
	}
	out := map[string]catalogRow{}
	for rows.Next() {
		var svc, action string
		var in, outArgs sql.NullString
		if err := rows.Scan(&svc, &action, &in, &outArgs); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("reading a catalog row: %w", err)
		}
		out[svc+"."+action] = catalogRow{Service: svc, Action: action, In: in.String, Out: outArgs.String}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterating catalog rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("closing catalog rows: %w", err)
	}
	return out, nil
}
