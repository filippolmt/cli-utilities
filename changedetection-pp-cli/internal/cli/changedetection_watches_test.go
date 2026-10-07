// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-authored. Not generated; survives a reprint.

package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const (
	testWatchUUID    = "adf44b68-64d7-4424-a6e2-476aaaa3608e"
	testUntitledUUID = "53b44fc2-63f3-4260-9a20-5a88d73ec1d2"
)

// fakeInstance serves the endpoints the novel commands read and counts requests.
func fakeInstance(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch {
		case r.URL.Path == "/api/v1/watch":
			_, _ = w.Write([]byte(`{"` + testWatchUUID + `":{"url":"https://example.com","title":"bob","last_changed":1,"last_checked":1,"last_error":"boom"},` +
				`"` + testUntitledUUID + `":{"url":"https://example.org","title":null,"page_title":"Novità","last_changed":1,"last_checked":1,"last_error":false}}`))
		case r.URL.Path == "/api/v1/search":
			// Like the real server: without partial=true, q must match exactly.
			if r.URL.Query().Get("partial") != "true" {
				_, _ = w.Write([]byte(`{}`))
				return
			}
			_, _ = w.Write([]byte(`{"` + testWatchUUID + `":{"url":"https://example.com","title":"bob"}}`))
		case r.URL.Path == "/api/v1/systeminfo":
			_, _ = w.Write([]byte(`{"overdue_watches":["` + testWatchUUID + `"]}`))
		case r.URL.Path == "/api/v1/watch/"+testWatchUUID+"/history":
			_, _ = w.Write([]byte(`{"1":"a.txt","2":"b.txt"}`))
		case strings.HasPrefix(r.URL.Path, "/api/v1/watch/"+testWatchUUID+"/history/"):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("2026-10-08 19:30"))
		case strings.HasPrefix(r.URL.Path, "/api/v1/watch/"+testWatchUUID+"/difference/"):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("(added) x"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func runAgainst(t *testing.T, srv *httptest.Server, args ...string) (string, error) {
	t.Helper()
	t.Setenv("CHANGEDETECTION_BASE_URL", srv.URL+"/api/v1")
	t.Setenv("CHANGEDETECTION_CONFIG", "")
	t.Setenv("CHANGEDETECTION_API_KEY", "test")
	cmd := RootCmd()
	cmd.SetArgs(append(args, "--home", t.TempDir(), "--no-cache"))
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	err := cmd.Execute()
	return out.String(), err
}

var novelLiveCommands = [][]string{
	{"since", "30d"},
	{"stale", "--days", "1"},
	{"errored"},
	{"overdue"},
	{"watch-search", "bob"},
	{"diff", testWatchUUID},
}

// The novel commands always read the API, so the agent envelope must say
// "live": the generic printer used to label them "local".
func TestNovelCommandsReportLiveSource(t *testing.T) {
	for _, args := range novelLiveCommands {
		t.Run(args[0], func(t *testing.T) {
			srv, _ := fakeInstance(t)
			out, err := runAgainst(t, srv, append(args, "--agent")...)
			if err != nil {
				t.Fatalf("error = %v, output:\n%s", err, out)
			}
			var env struct {
				Meta struct{ Source string }
			}
			if err := json.Unmarshal([]byte(out), &env); err != nil {
				t.Fatalf("output is not an envelope: %v\n%s", err, out)
			}
			if env.Meta.Source != "live" {
				t.Fatalf("meta.source = %q, want live\n%s", env.Meta.Source, out)
			}
		})
	}
}

// --data-source local has no store path in these commands: it must fail
// without touching the network instead of silently reading the API.
func TestNovelCommandsRejectLocalSource(t *testing.T) {
	for _, args := range novelLiveCommands {
		t.Run(args[0], func(t *testing.T) {
			srv, hits := fakeInstance(t)
			_, err := runAgainst(t, srv, append(args, "--agent", "--data-source", "local")...)
			if err == nil {
				t.Fatal("expected an error for --data-source local")
			}
			if n := hits.Load(); n != 0 {
				t.Fatalf("made %d API request(s) with --data-source local", n)
			}
		})
	}
}

// Live search must match substrings (partial=true), flatten the uuid-keyed
// response into one row per watch, and emit a single envelope. The generated
// command did none of the three; see .printing-press-patches/live-search.md.
func TestLiveSearchFindsWatchesBySubstring(t *testing.T) {
	srv, _ := fakeInstance(t)
	out, err := runAgainst(t, srv, "search", "bo", "--data-source", "live", "--agent")
	if err != nil {
		t.Fatalf("error = %v, output:\n%s", err, out)
	}
	var env struct {
		Meta    struct{ Source string }
		Results []struct{ UUID, Title string }
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("results are not a flat row list: %v\n%s", err, out)
	}
	if env.Meta.Source != "live" {
		t.Fatalf("meta.source = %q, want live\n%s", env.Meta.Source, out)
	}
	if len(env.Results) != 1 || env.Results[0].UUID != testWatchUUID || env.Results[0].Title != "bob" {
		t.Fatalf("results = %+v, want the one watch with its uuid\n%s", env.Results, out)
	}
}

// `stale 7` must mean 7 days, like `since 7d`: the positional used to be
// ignored, silently falling back to 30.
func TestStaleReadsPositionalDays(t *testing.T) {
	srv, _ := fakeInstance(t)
	if _, err := runAgainst(t, srv, "stale", "not-a-number", "--agent"); err == nil {
		t.Fatal("stale not-a-number: expected a usage error, the positional was ignored")
	}
	if _, err := runAgainst(t, srv, "stale", "7", "--days", "30", "--agent"); err == nil {
		t.Fatal("stale 7 --days 30: expected a conflict error")
	}
	if out, err := runAgainst(t, srv, "stale", "7", "--agent"); err != nil {
		t.Fatalf("stale 7: %v\n%s", err, out)
	}
}

// A watch without a user-set title shows the fetched page title, as the web UI
// does, and watch-search matches it.
func TestUntitledWatchFallsBackToPageTitle(t *testing.T) {
	srv, _ := fakeInstance(t)
	out, err := runAgainst(t, srv, "watch-search", "novità", "--agent")
	if err != nil {
		t.Fatalf("error = %v, output:\n%s", err, out)
	}
	var env struct {
		Results []struct{ UUID, Title string }
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Results) != 1 || env.Results[0].UUID != testUntitledUUID || env.Results[0].Title != "Novità" {
		t.Fatalf("results = %+v, want the untitled watch titled by its page title\n%s", env.Results, out)
	}
}

// Snapshots and diffs are text, not JSON. Reprint check for
// .printing-press-patches/text-response-endpoints.md: without it both
// generated commands fail with "API returned a non-JSON response".
func TestTextEndpointsReturnTheBody(t *testing.T) {
	cases := map[string][]string{
		"snapshot": {"watch", "history", "get-watch-snapshot", testWatchUUID, "latest"},
		"diff":     {"watch", "difference", "get-watch-history-diff", testWatchUUID, "previous", "latest"},
	}
	want := map[string]string{"snapshot": "2026-10-08 19:30", "diff": "(added) x"}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			srv, _ := fakeInstance(t)
			out, err := runAgainst(t, srv, append(args, "--agent")...)
			if err != nil {
				t.Fatalf("error = %v, output:\n%s", err, out)
			}
			var env struct{ Results string }
			if err := json.Unmarshal([]byte(out), &env); err != nil || env.Results != want[name] {
				t.Fatalf("results = %q (err %v), want %q\n%s", env.Results, err, want[name], out)
			}
		})
	}
}
