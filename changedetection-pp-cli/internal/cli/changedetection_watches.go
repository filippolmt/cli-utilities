// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-authored shared helpers for the changedetection.io novel commands
// (since / stale / errored / diff / watch-search). Kept in its own file so a
// regen preserves it.

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
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
	PageTitle   string          `json:"page_title"`
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

// newLiveClient builds the client for the novel commands, which read the live
// API only: they have no local-store path, so --data-source local is rejected
// instead of silently going to the network.
func newLiveClient(flags *rootFlags) (*client.Client, error) {
	if flags.dataSource == "local" {
		return nil, usageErr(fmt.Errorf("this command reads the live API only; --data-source local is not supported"))
	}
	return flags.newClient()
}

// printLiveJSON is printJSONFiltered with meta.source "live": the generic
// printer labels every result "local", which is wrong for data just read from
// the API.
func printLiveJSON(w io.Writer, v any, flags *rootFlags) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return printOutputWithFlagsMeta(w, json.RawMessage(raw), flags, map[string]any{"source": "live"})
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
		// title is the user's label and is often unset; the web UI then shows
		// the fetched page title, and so do we.
		if strings.TrimSpace(w.Title) == "" {
			w.Title = w.PageTitle
		}
		rows = append(rows, w)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].LastChanged > rows[j].LastChanged })
	return rows, nil
}

var uuidKey = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// flattenUUIDMap turns a uuid-keyed object ({"<uuid>": {...}}, the shape of
// /watch, /tags and /search) into an array of its values with "uuid" set, the
// shape the generic search and write-through pipelines expect. Any other body,
// including a detail object, is returned unchanged.
func flattenUUIDMap(data json.RawMessage) json.RawMessage {
	var m map[string]map[string]json.RawMessage
	if json.Unmarshal(data, &m) != nil {
		return data
	}
	uuids := make([]string, 0, len(m))
	for uuid := range m {
		if !uuidKey.MatchString(uuid) {
			return data
		}
		uuids = append(uuids, uuid)
	}
	sort.Strings(uuids)
	rows := make([]map[string]json.RawMessage, 0, len(m))
	for _, uuid := range uuids {
		row := m[uuid]
		if row == nil {
			return data
		}
		if _, ok := row["uuid"]; !ok {
			row["uuid"], _ = json.Marshal(uuid)
		}
		rows = append(rows, row)
	}
	out, err := json.Marshal(rows)
	if err != nil {
		return data
	}
	return out
}

// withPageTitles gives untitled rows of a flattened watch list the page title,
// as fetchWatches does: /search returns no page_title. Best effort: when no
// row needs it, or /watch fails, rows come back unchanged.
func withPageTitles(ctx context.Context, c *client.Client, rows json.RawMessage) json.RawMessage {
	var items []map[string]json.RawMessage
	if json.Unmarshal(rows, &items) != nil {
		return rows
	}
	untitled := false
	for _, item := range items {
		var title string
		_ = json.Unmarshal(item["title"], &title)
		if strings.TrimSpace(title) == "" {
			untitled = true
			break
		}
	}
	if !untitled {
		return rows
	}
	watches, err := fetchWatches(ctx, c)
	if err != nil {
		return rows
	}
	titles := make(map[string]string, len(watches))
	for _, w := range watches {
		titles[w.UUID] = w.Title
	}
	for _, item := range items {
		var title, uuid string
		_ = json.Unmarshal(item["title"], &title)
		_ = json.Unmarshal(item["uuid"], &uuid)
		if strings.TrimSpace(title) == "" && titles[uuid] != "" {
			item["title"], _ = json.Marshal(titles[uuid])
		}
	}
	out, err := json.Marshal(items)
	if err != nil {
		return rows
	}
	return out
}

// isoOrNever renders an epoch-seconds timestamp as RFC3339 UTC, or "never" for 0.
func isoOrNever(epoch int64) string {
	if epoch <= 0 {
		return "never"
	}
	return time.Unix(epoch, 0).UTC().Format(time.RFC3339)
}
