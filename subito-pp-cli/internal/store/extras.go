// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package store

import (
	"context"
	"database/sql"
	"fmt"
)

// migrateExtras runs after the generated store migrations and before the
// schema-version stamp. It is the canonical place for novel-feature auxiliary
// tables that need to live in the local store.
//
// Edit this file when adding tables for novel commands. Keep migrations
// idempotent with CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS so
// every store open can safely re-run them.
func (s *Store) migrateExtras(ctx context.Context, conn *sql.Conn) error {
	migrations := []string{
		// subito: one row per listing ever seen, for seller and repost queries.
		`CREATE TABLE IF NOT EXISTS subito_listings (
			list_id TEXT PRIMARY KEY,
			urn TEXT NOT NULL,
			user_id TEXT NOT NULL DEFAULT '',
			company INTEGER NOT NULL DEFAULT 0,
			category_id TEXT NOT NULL DEFAULT '',
			subject TEXT NOT NULL DEFAULT '',
			subject_norm TEXT NOT NULL DEFAULT '',
			price REAL,
			published TEXT NOT NULL DEFAULT '',
			url TEXT NOT NULL DEFAULT '',
			first_seen TEXT NOT NULL,
			last_seen TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS subito_listings_user ON subito_listings(user_id)`,
		// subito: a new row whenever a listing's price or publish date changes.
		`CREATE TABLE IF NOT EXISTS subito_observations (
			list_id TEXT NOT NULL,
			price REAL,
			published TEXT NOT NULL DEFAULT '',
			seen_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS subito_observations_list ON subito_observations(list_id, seen_at)`,
		// subito: saved searches and the listings each one has already reported.
		`CREATE TABLE IF NOT EXISTS subito_watches (
			name TEXT PRIMARY KEY,
			params TEXT NOT NULL,
			created_at TEXT NOT NULL,
			last_run_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS subito_watch_seen (
			watch TEXT NOT NULL,
			list_id TEXT NOT NULL,
			price REAL,
			first_seen TEXT NOT NULL,
			PRIMARY KEY (watch, list_id)
		)`,
	}
	for _, m := range migrations {
		if _, err := conn.ExecContext(ctx, m); err != nil {
			return fmt.Errorf("extra migration failed: %w", err)
		}
	}
	return nil
}
