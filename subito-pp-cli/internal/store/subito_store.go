package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// SubitoListing is the indexed slice of one listing that subito novel
// commands query; the full JSON lives in resources under type "ads".
type SubitoListing struct {
	ListID      string
	URN         string
	UserID      string
	Company     bool
	CategoryID  string
	Subject     string
	SubjectNorm string
	Price       sql.NullFloat64
	Published   string
	URL         string
	Raw         json.RawMessage
}

// SubitoObservation is one recorded price/publish-date state of a listing.
type SubitoObservation struct {
	Price     sql.NullFloat64
	Published string
	SeenAt    string
}

// RecordSubitoListings stores listings seen in a search: the raw JSON in
// resources (type "ads", keyed by urn), the index row, and a new observation
// when price or publish date changed since the last one. It returns how many
// observations were added.
func (s *Store) RecordSubitoListings(ctx context.Context, listings []SubitoListing, now time.Time) (int, error) {
	if len(listings) == 0 {
		return 0, nil
	}
	raws := make([]json.RawMessage, 0, len(listings))
	for _, l := range listings {
		if len(l.Raw) > 0 {
			raws = append(raws, l.Raw)
		}
	}
	// UpsertBatch opens its own write transaction; run it before ours.
	if len(raws) > 0 {
		if _, _, err := s.UpsertBatch("ads", raws); err != nil {
			return 0, fmt.Errorf("storing listings: %w", err)
		}
	}
	stamp := now.UTC().Format(time.RFC3339)
	s.lockForWrite()
	defer s.unlockAfterWrite()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	added := 0
	for _, l := range listings {
		var lastPrice sql.NullFloat64
		var lastPublished string
		changed := false
		err := tx.QueryRowContext(ctx, `SELECT price, published FROM subito_observations WHERE list_id = ? ORDER BY seen_at DESC, rowid DESC LIMIT 1`, l.ListID).Scan(&lastPrice, &lastPublished)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			changed = true
		case err != nil:
			return 0, err
		default:
			changed = lastPublished != l.Published || lastPrice != l.Price
		}
		if changed {
			if _, err := tx.ExecContext(ctx, `INSERT INTO subito_observations (list_id, price, published, seen_at) VALUES (?, ?, ?, ?)`, l.ListID, l.Price, l.Published, stamp); err != nil {
				return 0, err
			}
			added++
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO subito_listings (list_id, urn, user_id, company, category_id, subject, subject_norm, price, published, url, first_seen, last_seen)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(list_id) DO UPDATE SET urn = excluded.urn, user_id = excluded.user_id, company = excluded.company,
				category_id = excluded.category_id, subject = excluded.subject, subject_norm = excluded.subject_norm,
				price = excluded.price, published = excluded.published, url = excluded.url, last_seen = excluded.last_seen`,
			l.ListID, l.URN, l.UserID, boolInt(l.Company), l.CategoryID, l.Subject, l.SubjectNorm, l.Price, l.Published, l.URL, stamp, stamp); err != nil {
			return 0, err
		}
	}
	return added, tx.Commit()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// SubitoListing loads one indexed listing; sql.ErrNoRows when unknown.
func (s *Store) SubitoListing(ctx context.Context, listID string) (SubitoListing, string, error) {
	var l SubitoListing
	var company int
	var firstSeen string
	err := s.db.QueryRowContext(ctx, `SELECT list_id, urn, user_id, company, category_id, subject, subject_norm, price, published, url, first_seen FROM subito_listings WHERE list_id = ?`, listID).
		Scan(&l.ListID, &l.URN, &l.UserID, &company, &l.CategoryID, &l.Subject, &l.SubjectNorm, &l.Price, &l.Published, &l.URL, &firstSeen)
	if err != nil {
		return l, "", err
	}
	l.Company = company == 1
	if raw, err := s.Get("ads", l.URN); err == nil {
		l.Raw = raw
	}
	return l, firstSeen, nil
}

// SubitoObservations returns a listing's recorded states, oldest first.
func (s *Store) SubitoObservations(ctx context.Context, listID string) ([]SubitoObservation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT price, published, seen_at FROM subito_observations WHERE list_id = ? ORDER BY seen_at, rowid`, listID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SubitoObservation, 0)
	for rows.Next() {
		var o SubitoObservation
		if err := rows.Scan(&o.Price, &o.Published, &o.SeenAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// SubitoSellerListings returns the other listings of one advertiser.
func (s *Store) SubitoSellerListings(ctx context.Context, userID, exceptListID string) ([]SubitoListing, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT list_id, urn, category_id, subject, subject_norm, price, published, url FROM subito_listings WHERE user_id = ? AND user_id != '' AND list_id != ? ORDER BY last_seen DESC`, userID, exceptListID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SubitoListing, 0)
	for rows.Next() {
		l := SubitoListing{UserID: userID}
		if err := rows.Scan(&l.ListID, &l.URN, &l.CategoryID, &l.Subject, &l.SubjectNorm, &l.Price, &l.Published, &l.URL); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// SubitoWatch is one saved search.
type SubitoWatch struct {
	Name      string            `json:"name"`
	Params    map[string]string `json:"params"`
	CreatedAt string            `json:"created_at"`
	LastRunAt string            `json:"last_run_at,omitempty"`
}

// SaveSubitoWatch creates or replaces a watch.
func (s *Store) SaveSubitoWatch(ctx context.Context, w SubitoWatch) error {
	params, err := json.Marshal(w.Params)
	if err != nil {
		return err
	}
	s.lockForWrite()
	defer s.unlockAfterWrite()
	_, err = s.db.ExecContext(ctx, `INSERT INTO subito_watches (name, params, created_at) VALUES (?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET params = excluded.params`, w.Name, string(params), w.CreatedAt)
	return err
}

// SubitoWatches lists watches by name; a non-empty name filters to one.
func (s *Store) SubitoWatches(ctx context.Context, name string) ([]SubitoWatch, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, params, created_at, last_run_at FROM subito_watches WHERE ? = '' OR name = ? ORDER BY name`, name, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SubitoWatch, 0)
	for rows.Next() {
		var w SubitoWatch
		var params string
		if err := rows.Scan(&w.Name, &params, &w.CreatedAt, &w.LastRunAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(params), &w.Params); err != nil {
			return nil, fmt.Errorf("watch %q has corrupt params: %w", w.Name, err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// DeleteSubitoWatch removes a watch and its seen set; false when absent.
func (s *Store) DeleteSubitoWatch(ctx context.Context, name string) (bool, error) {
	s.lockForWrite()
	defer s.unlockAfterWrite()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM subito_watches WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if _, err := tx.ExecContext(ctx, `DELETE FROM subito_watch_seen WHERE watch = ?`, name); err != nil {
		return false, err
	}
	return n > 0, tx.Commit()
}

// SubitoWatchSeenSet returns the listings a watch has already reported,
// with the price it last reported them at.
func (s *Store) SubitoWatchSeenSet(ctx context.Context, watch string) (map[string]sql.NullFloat64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT list_id, price FROM subito_watch_seen WHERE watch = ?`, watch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]sql.NullFloat64{}
	for rows.Next() {
		var id string
		var price sql.NullFloat64
		if err := rows.Scan(&id, &price); err != nil {
			return nil, err
		}
		out[id] = price
	}
	return out, rows.Err()
}

// MarkSubitoWatchSeen records the listings a run reported and stamps the run.
func (s *Store) MarkSubitoWatchSeen(ctx context.Context, watch string, prices map[string]sql.NullFloat64, now time.Time) error {
	stamp := now.UTC().Format(time.RFC3339)
	s.lockForWrite()
	defer s.unlockAfterWrite()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for id, p := range prices {
		if _, err := tx.ExecContext(ctx, `INSERT INTO subito_watch_seen (watch, list_id, price, first_seen) VALUES (?, ?, ?, ?)
			ON CONFLICT(watch, list_id) DO UPDATE SET price = excluded.price`, watch, id, p, stamp); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE subito_watches SET last_run_at = ? WHERE name = ?`, stamp, watch); err != nil {
		return err
	}
	return tx.Commit()
}
