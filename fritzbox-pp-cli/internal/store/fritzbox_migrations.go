// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
)

// identPattern is the shape every table and column name reaching a statement
// builder in this package must have.
//
// SQLite cannot bind an identifier as a parameter, so table and column names
// have to be concatenated into the statement text. Validating them turns
// "the callers only ever pass constants" from a comment into an enforced
// invariant, so a later caller that threads a user-supplied name through
// cannot quietly open an injection hole.
var identPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidateIdentifier rejects anything that is not a bare SQL identifier.
func ValidateIdentifier(name string) error {
	if !identPattern.MatchString(name) {
		return fmt.Errorf("refusing to build a statement for %q: not a plain SQL identifier", name)
	}
	return nil
}

// ValidateIdentifiers checks a table name and its column names together.
func ValidateIdentifiers(table string, columns []string) error {
	if err := ValidateIdentifier(table); err != nil {
		return err
	}
	for _, c := range columns {
		if err := ValidateIdentifier(c); err != nil {
			return err
		}
	}
	return nil
}

// fritzboxSchema holds the tables the FRITZ!Box surfaces need beyond the
// generated resource mirror. They live here rather than in the generated
// migration slice so a regeneration cannot drop them.
//
// The history tables are what make this CLI different from every other
// FRITZ!Box tool: the router keeps no host history and rotates its own log, so
// the questions "what changed" and "what did it say last week" are only
// answerable from a local append-only mirror.
const fritzboxSchema = `
CREATE TABLE IF NOT EXISTS fb_host_snapshots (
	taken_at   INTEGER NOT NULL,
	mac        TEXT    NOT NULL,
	name       TEXT    NOT NULL DEFAULT '',
	ip         TEXT    NOT NULL DEFAULT '',
	active     INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (taken_at, mac)
);
CREATE INDEX IF NOT EXISTS fb_host_snapshots_mac ON fb_host_snapshots(mac, taken_at);

CREATE TABLE IF NOT EXISTS fb_wan_samples (
	taken_at    INTEGER NOT NULL PRIMARY KEY,
	uptime      INTEGER NOT NULL DEFAULT 0,
	external_ip TEXT    NOT NULL DEFAULT '',
	status      TEXT    NOT NULL DEFAULT '',
	last_error  TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS fb_log_entries (
	id         TEXT    NOT NULL PRIMARY KEY,
	logged_at  INTEGER NOT NULL,
	message    TEXT    NOT NULL DEFAULT '',
	category   TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS fb_log_entries_time ON fb_log_entries(logged_at);
CREATE VIRTUAL TABLE IF NOT EXISTS fb_log_fts USING fts5(message, content='fb_log_entries', content_rowid='rowid');

CREATE TABLE IF NOT EXISTS fb_calls (
	id        TEXT    NOT NULL PRIMARY KEY,
	called_at INTEGER NOT NULL,
	direction TEXT    NOT NULL DEFAULT '',
	name      TEXT    NOT NULL DEFAULT '',
	number    TEXT    NOT NULL DEFAULT '',
	duration  TEXT    NOT NULL DEFAULT '',
	port      TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS fb_calls_time ON fb_calls(called_at);

CREATE TABLE IF NOT EXISTS fb_phonebook (
	id       TEXT NOT NULL PRIMARY KEY,
	book     TEXT NOT NULL DEFAULT '',
	name     TEXT NOT NULL DEFAULT '',
	number   TEXT NOT NULL DEFAULT '',
	kind     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS fb_phonebook_number ON fb_phonebook(number);

CREATE TABLE IF NOT EXISTS fb_actions (
	snapshot_at  INTEGER NOT NULL,
	service      TEXT    NOT NULL,
	service_type TEXT    NOT NULL DEFAULT '',
	control_url  TEXT    NOT NULL DEFAULT '',
	scpd_url     TEXT    NOT NULL DEFAULT '',
	action       TEXT    NOT NULL,
	args_in      TEXT    NOT NULL DEFAULT '',
	args_out     TEXT    NOT NULL DEFAULT '',
	PRIMARY KEY (snapshot_at, service, action)
);
CREATE INDEX IF NOT EXISTS fb_actions_lookup ON fb_actions(service, action);

CREATE TABLE IF NOT EXISTS fb_portmaps (
	synced_at    INTEGER NOT NULL,
	idx          INTEGER NOT NULL,
	description  TEXT    NOT NULL DEFAULT '',
	protocol     TEXT    NOT NULL DEFAULT '',
	external     TEXT    NOT NULL DEFAULT '',
	internal_ip  TEXT    NOT NULL DEFAULT '',
	internal_port TEXT   NOT NULL DEFAULT '',
	enabled      INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (synced_at, idx)
);

CREATE TABLE IF NOT EXISTS fb_energy_samples (
	taken_at INTEGER NOT NULL,
	subsystem TEXT   NOT NULL,
	percent   INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (taken_at, subsystem)
);

CREATE TABLE IF NOT EXISTS fb_people (
	person TEXT NOT NULL,
	mac    TEXT NOT NULL,
	PRIMARY KEY (person, mac)
);
`

// EnsureFritzboxSchema creates the FRITZ!Box-specific tables if they are
// missing. It is safe to call on every command, and cheap: SQLite parses the
// IF NOT EXISTS statements without touching the data pages when the tables
// already exist.
func (s *Store) EnsureFritzboxSchema(ctx context.Context) error {
	if _, err := s.DB().ExecContext(ctx, fritzboxSchema); err != nil {
		return fmt.Errorf("creating the FRITZ!Box local tables: %w", err)
	}
	return nil
}

// InsertLogEntries appends log lines and keeps the FTS index in step.
//
// The router re-serves the same lines on every poll, so INSERT OR IGNORE on a
// content hash primary key is what makes repeated polling idempotent. The FTS
// row is only written when the insert actually added a row; otherwise repeated
// polls would multiply search hits for a single log line.
func (s *Store) InsertLogEntries(ctx context.Context, entries []LogEntry) (int, error) {
	if len(entries) == 0 {
		return 0, nil
	}
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("opening the log write transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	inserted := 0
	for _, e := range entries {
		res, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO fb_log_entries(id, logged_at, message, category) VALUES (?, ?, ?, ?)`,
			e.ID, e.LoggedAt, e.Message, e.Category)
		if err != nil {
			return 0, fmt.Errorf("inserting a log entry: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("counting inserted log entries: %w", err)
		}
		if affected == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO fb_log_fts(rowid, message) SELECT rowid, message FROM fb_log_entries WHERE id = ?`,
			e.ID); err != nil {
			return 0, fmt.Errorf("indexing a log entry for search: %w", err)
		}
		inserted++
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing log entries: %w", err)
	}
	return inserted, nil
}

// LogEntry is one stored router log line.
type LogEntry struct {
	ID       string `json:"id"`
	LoggedAt int64  `json:"logged_at"`
	Message  string `json:"message"`
	Category string `json:"category"`
}

// ReplaceRows swaps the full contents of one FRITZ!Box table in a single
// transaction. Used for the datasets the router returns whole rather than
// incrementally: the phonebook, the call journal, and port mappings.
func (s *Store) ReplaceRows(ctx context.Context, table string, columns []string, rows [][]any) error {
	if len(columns) == 0 {
		return fmt.Errorf("replacing rows in %s: no columns supplied", table)
	}
	// Identifiers cannot be bound, so they are checked instead; values always
	// go through placeholders below.
	if err := ValidateIdentifiers(table, columns); err != nil {
		return err
	}
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("opening the %s write transaction: %w", table, err)
	}
	defer func() { _ = tx.Rollback() }()

	// #nosec G202 -- table validated by ValidateIdentifiers above.
	if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
		return fmt.Errorf("clearing %s: %w", table, err)
	}
	placeholders := ""
	for i := range columns {
		if i > 0 {
			placeholders += ", "
		}
		placeholders += "?"
	}
	// #nosec G202 -- table and columns validated by ValidateIdentifiers above;
	// every value is a placeholder.
	stmt := "INSERT OR REPLACE INTO " + table + " (" + joinColumns(columns) + ") VALUES (" + placeholders + ")"
	for _, row := range rows {
		if len(row) != len(columns) {
			return fmt.Errorf("replacing rows in %s: row has %d values for %d columns", table, len(row), len(columns))
		}
		if _, err := tx.ExecContext(ctx, stmt, row...); err != nil {
			return fmt.Errorf("inserting into %s: %w", table, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing %s: %w", table, err)
	}
	return nil
}

func joinColumns(cols []string) string {
	out := ""
	for i, c := range cols {
		if i > 0 {
			out += ", "
		}
		out += c
	}
	return out
}

// MarshalJSONRow is a small helper for callers storing a JSON blob column.
func MarshalJSONRow(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
