// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-authored shared helpers for the changedetection.io novel commands
// (since / stale / errored / diff / watch-search). Kept in its own file so a
// regen preserves it.

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"changedetection-pp-cli/internal/client"
)

// watchRow is the subset of the /watch response the novel commands need.
// The changedetection.io GET /watch endpoint returns a JSON object keyed by
// watch UUID; each value carries these fields (plus many more we ignore).
type watchRow struct {
	UUID        string          `json:"uuid"`
	URL         string          `json:"url"`
	Link        string          `json:"link"`
	Title       string          `json:"title"`
	LastChecked int64           `json:"last_checked"`
	LastChanged int64           `json:"last_changed"`
	LastError   json.RawMessage `json:"last_error"`
	Paused      bool            `json:"paused"`
}

// errorText returns the watch's error message, or "" when the watch is healthy.
// changedetection sets last_error to false (bool) when there is no error, or to
// a string message when a fetch failed.
func (w watchRow) errorText() string {
	s := strings.TrimSpace(string(w.LastError))
	switch s {
	case "", "null", "false", `""`, `"false"`:
		return ""
	}
	var msg string
	if json.Unmarshal(w.LastError, &msg) == nil {
		return strings.TrimSpace(msg)
	}
	return s
}

// fetchWatches GETs /watch and flattens the uuid-keyed map into a slice,
// most-recently-changed first, injecting the map key as UUID when the object
// omits it. A single malformed entry is skipped rather than failing the list.
func fetchWatches(ctx context.Context, c *client.Client) ([]watchRow, error) {
	data, err := c.Get(ctx, "/watch", nil)
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing /watch response: %w", err)
	}
	rows := make([]watchRow, 0, len(raw))
	for uuid, item := range raw {
		var w watchRow
		if json.Unmarshal(item, &w) != nil {
			continue
		}
		if w.UUID == "" {
			w.UUID = uuid
		}
		rows = append(rows, w)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].LastChanged > rows[j].LastChanged })
	return rows, nil
}

// isoOrNever renders an epoch-seconds timestamp as RFC3339 UTC, or "never" for 0.
func isoOrNever(epoch int64) string {
	if epoch <= 0 {
		return "never"
	}
	return time.Unix(epoch, 0).UTC().Format(time.RFC3339)
}
