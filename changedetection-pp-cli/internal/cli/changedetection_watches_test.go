// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-authored. Not generated; survives a reprint.

package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

const (
	testWatchUUID    = "adf44b68-64d7-4424-a6e2-476aaaa3608e"
	testUntitledUUID = "53b44fc2-63f3-4260-9a20-5a88d73ec1d2"
	testTagUUID      = "53ae5550-b177-43fd-b021-c9a53545147f"
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
			// /search carries no page_title; the untitled watch matches on its URL.
			_, _ = w.Write([]byte(`{"` + testWatchUUID + `":{"url":"https://example.com","title":"bob"},` +
				`"` + testUntitledUUID + `":{"url":"https://example.org","title":null}}`))
		case r.URL.Path == "/api/v1/tags":
			_, _ = w.Write([]byte(`{"` + testTagUUID + `":{"title":"Casa","uuid":"` + testTagUUID + `"},` +
				`"9bb926b5-4bbe-4f19-b307-03a5aad27cd4":{"title":"Vasco","uuid":"9bb926b5-4bbe-4f19-b307-03a5aad27cd4"}}`))
		case r.URL.Path == "/api/v1/systeminfo":
			_, _ = w.Write([]byte(`{"overdue_watches":["` + testWatchUUID + `","` + testUntitledUUID + `"]}`))
		case r.URL.Path == "/api/v1/watch/"+testWatchUUID:
			_, _ = w.Write([]byte(`{"paused":false,"time_schedule_limit":{"enabled":true}}`))
		case r.URL.Path == "/api/v1/watch/"+testUntitledUUID:
			_, _ = w.Write([]byte(`{"paused":true,"time_schedule_limit":{"enabled":false}}`))
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
	return runAgainstHome(t, srv, t.TempDir(), args...)
}

// runAgainstHome is runAgainst with a fixed --home, so several runs share one
// local store.
func runAgainstHome(t *testing.T, srv *httptest.Server, home string, args ...string) (string, error) {
	t.Helper()
	return runCLI(t, srv, home, append(args, "--no-cache")...)
}

// runCLI runs the CLI against srv with --home and nothing else added, so the
// HTTP response cache stays on.
func runCLI(t *testing.T, srv *httptest.Server, home string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("CHANGEDETECTION_BASE_URL", srv.URL+"/api/v1")
	t.Setenv("CHANGEDETECTION_CONFIG", "")
	t.Setenv("CHANGEDETECTION_API_KEY", "test")
	cmd := RootCmd()
	cmd.SetArgs(append(args, "--home", home))
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
// response into one row per watch, title untitled watches, and emit a single
// envelope. See .printing-press-patches/live-search.md.
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
	titles := map[string]string{}
	for _, r := range env.Results {
		titles[r.UUID] = r.Title
	}
	if len(titles) != 2 || titles[testWatchUUID] != "bob" {
		t.Fatalf("results = %+v, want both watches with their uuid\n%s", env.Results, out)
	}
	// An untitled watch shows its page title, as everywhere else.
	if titles[testUntitledUUID] != "Novità" {
		t.Fatalf("untitled watch title = %q, want its page title\n%s", titles[testUntitledUUID], out)
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

// Live list reads cache their rows for offline use. Every changedetection list
// is an object keyed by uuid, which the write-through cache used to store as
// one row without an id (warning "no extractable ID field"), so nothing landed.
func TestLiveListsFeedTheLocalStore(t *testing.T) {
	srv, _ := fakeInstance(t)
	home := t.TempDir()
	if out, err := runAgainstHome(t, srv, home, "watch", "list-watches", "--agent"); err != nil {
		t.Fatalf("list-watches: %v\n%s", err, out)
	}
	out, err := runAgainstHome(t, srv, home, "search", "bob", "--data-source", "local", "--agent")
	if err != nil {
		t.Fatalf("local search: %v\n%s", err, out)
	}
	if !strings.Contains(out, testWatchUUID) {
		t.Fatalf("local store has no row for the watch listed live:\n%s", out)
	}
}

// sync stores one row per watch and per tag. It used to fail with "missing id
// for watch" and store each uuid-keyed tag list as a single row.
func TestSyncStoresEveryWatchAndTag(t *testing.T) {
	srv, _ := fakeInstance(t)
	home := t.TempDir()
	if out, err := runAgainstHome(t, srv, home, "sync", "--agent", "--strict"); err != nil {
		t.Fatalf("sync: %v\n%s", err, out)
	}
	for query, want := range map[string]string{"bob": testWatchUUID, "vasco": "9bb926b5-4bbe-4f19-b307-03a5aad27cd4"} {
		out, err := runAgainstHome(t, srv, home, "search", query, "--data-source", "local", "--agent")
		if err != nil {
			t.Fatalf("local search %q: %v\n%s", query, err, out)
		}
		if !strings.Contains(out, want) {
			t.Fatalf("local search %q: no row %s after sync:\n%s", query, want, out)
		}
	}
}

// `find --partial` is a switch: bare, it must not swallow the next flag (it
// used to take "--agent" as its value, and the server answered 500).
func TestFindPartialIsASwitch(t *testing.T) {
	srv, _ := fakeInstance(t)
	out, err := runAgainst(t, srv, "find", "--q", "bo", "--partial", "--agent")
	if err != nil {
		t.Fatalf("find --partial: %v\n%s", err, out)
	}
	if !strings.Contains(out, testWatchUUID) || !strings.Contains(out, `"source": "live"`) {
		t.Fatalf("find --partial --agent: want the watch in an agent envelope\n%s", out)
	}
	if _, err := runAgainst(t, srv, "find", "--q", "bo", "--partial", "false", "--agent"); err == nil {
		t.Fatal("find --partial false: a stray positional must be an error, not silently ignored")
	}
}

// The server lists paused watches as overdue; they are not due. A watch
// limited to a time window is flagged, since it is overdue outside it by design.
func TestOverdueSkipsPausedWatches(t *testing.T) {
	srv, _ := fakeInstance(t)
	out, err := runAgainst(t, srv, "overdue", "--agent")
	if err != nil {
		t.Fatalf("overdue: %v\n%s", err, out)
	}
	var env struct {
		Results []struct {
			UUID            string
			ScheduleLimited bool `json:"schedule_limited"`
		}
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Results) != 1 || env.Results[0].UUID != testWatchUUID {
		t.Fatalf("results = %+v, want only the unpaused watch\n%s", env.Results, out)
	}
	if !env.Results[0].ScheduleLimited {
		t.Fatalf("schedule_limited not set for a watch with time_schedule_limit enabled\n%s", out)
	}
}

// GET endpoints whose query flags change state: watch get --paused/--muted/
// --recheck, tag get --muted/--recheck, watch list-watches --recheck-all.
var stateChangingGets = []struct {
	cmd         []string
	flag, query string
	values      []string
}{
	{[]string{"watch", "get", testWatchUUID}, "--paused", "paused", []string{"paused", "unpaused", "paused"}},
	{[]string{"tag", "get", testTagUUID}, "--muted", "muted", []string{"muted", "unmuted", "muted"}},
	{[]string{"watch", "list-watches"}, "--recheck-all", "recheck_all", []string{"1", "1"}},
}

// Each state change must reach the server. Served from the response cache,
// paused -> unpaused -> paused within five minutes left the watch unpaused
// while reporting success.
func TestStateChangingGetsBypassTheCache(t *testing.T) {
	for _, tc := range stateChangingGets {
		t.Run(strings.Join(tc.cmd[:2], " "), func(t *testing.T) {
			first := tc.values[0]
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get(tc.query) == first {
					hits.Add(1)
				}
				_, _ = w.Write([]byte(`{"uuid":"` + testWatchUUID + `"}`))
			}))
			t.Cleanup(srv.Close)
			home := t.TempDir()
			want := int32(0)
			for _, v := range tc.values {
				if v == first {
					want++
				}
				args := append(append([]string{}, tc.cmd...), tc.flag, v, "--agent")
				if out, err := runCLI(t, srv, home, args...); err != nil {
					t.Fatalf("%v: %v\n%s", args, err, out)
				}
			}
			if n := hits.Load(); n != want {
				t.Fatalf("server saw %d %s=%s requests, want %d: the rest came from cache", n, tc.query, first, want)
			}
		})
	}
}

// Because they can change state, these commands are not read-only for MCP.
func TestStateChangingGetsAreNotMarkedReadOnly(t *testing.T) {
	for _, tc := range stateChangingGets {
		cmd, _, err := RootCmd().Find(tc.cmd[:2])
		if err != nil {
			t.Fatal(err)
		}
		if cmd.Annotations["mcp:read-only"] == "true" {
			t.Errorf("%s is annotated mcp:read-only but %s changes state", cmd.CommandPath(), tc.flag)
		}
	}
}

// Responses that are not entities (a status object, a {timestamp: path} map,
// the --dry-run sentinel) skip the write-through cache instead of warning
// that they have no id.
func TestNonEntityReadsDoNotWarn(t *testing.T) {
	srv, _ := fakeInstance(t)
	for _, args := range [][]string{
		{"systeminfo"},
		{"watch", "history", "get-watch", testWatchUUID},
		{"watch", "get", testWatchUUID, "--dry-run"},
	} {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		stderr := os.Stderr
		os.Stderr = w
		_, runErr := runAgainst(t, srv, append(args, "--agent")...)
		os.Stderr = stderr
		_ = w.Close()
		captured, _ := io.ReadAll(r)
		if runErr != nil {
			t.Fatalf("%v: %v", args, runErr)
		}
		if strings.Contains(string(captured), "not cached locally") {
			t.Errorf("%v warned:\n%s", args, captured)
		}
	}
}

// An update sends only the fields the user set. Flags with defaults (processor
// text_json_diff, fetch_backend system, conditions_match_logic ALL) used to
// ride along, so renaming a watch reset its fetch backend and processor.
func TestUpdateSendsOnlyChangedFields(t *testing.T) {
	for _, target := range [][]string{
		{"watch", "update", testWatchUUID},
		{"tag", "update", testTagUUID},
	} {
		t.Run(target[0], func(t *testing.T) {
			var body map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPut {
					_ = json.NewDecoder(r.Body).Decode(&body)
				}
				_, _ = w.Write([]byte(`{}`))
			}))
			t.Cleanup(srv.Close)
			if out, err := runAgainst(t, srv, append(target, "--title", "nuovo", "--agent", "--yes")...); err != nil {
				t.Fatalf("%v: %v\n%s", target, err, out)
			}
			if len(body) != 1 || body["title"] != "nuovo" {
				t.Fatalf("PUT body = %v, want only the title", body)
			}
		})
	}
}
