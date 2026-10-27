// Hand-written: tests for the generated-file patches in .printing-press-patches/
// (listing-compact-keep, export-ads-path, ads-check-live), run against a fake
// hades server.

package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"subito-pp-cli/internal/cliutil/testenv"
)

const testAd = `{"urn":"id:ad:x:list:%d","type":{"key":"s"},"subject":"Soffione","body":"lungo testo",` +
	`"features":[{"uri":"/price","values":[{"key":"40"}]}],"urls":{"default":"https://www.subito.it/x-1.htm"}}`

// fakeHades serves the endpoints these tests read and records the search queries.
// /search/items holds total listings and pages them over start/lim.
func fakeHades(t *testing.T, total int) (*httptest.Server, func() []url.Values) {
	t.Helper()
	var mu sync.Mutex
	var searches []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/search/items":
			mu.Lock()
			searches = append(searches, r.URL.Query())
			mu.Unlock()
			start, _ := strconv.Atoi(r.URL.Query().Get("start"))
			lim, _ := strconv.Atoi(r.URL.Query().Get("lim"))
			if lim == 0 {
				lim = 30
			}
			var ads []string
			for i := start; i < total && i < start+lim; i++ {
				ads = append(ads, strings.Replace(testAd, "%d", strconv.Itoa(i), 1))
			}
			_, _ = w.Write([]byte(`{"count_all":` + strconv.Itoa(total) + `,"ads":[` + strings.Join(ads, ",") + `]}`))
		case "/v1/search/items/detail-recommended":
			_, _ = w.Write([]byte(`{"ads":[` + strings.Replace(testAd, "%d", "1", 1) + `]}`))
		case "/v1/search/items/check":
			_, _ = w.Write([]byte(`{"newads":false,"count":0}`))
		case "/v1/search/categories/2/ad_types/s/filters":
			_, _ = w.Write([]byte(`{"filters":[{"uri":"/category","type":"category","label":"Categoria",` +
				`"query_string":"c","datasource":"/v1/values/categories","chip":false}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []url.Values {
		mu.Lock()
		defer mu.Unlock()
		return searches
	}
}

func runSubito(t *testing.T, srv *httptest.Server, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	t.Setenv("SUBITO_BASE_URL", srv.URL+"/v1")
	cmd := RootCmd()
	cmd.SetArgs(append(args, "--no-cache"))
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestAgentKeepsListingFields(t *testing.T) {
	for _, tc := range []struct {
		args       []string
		want, drop []string
	}{
		{[]string{"ads", "search", "--query", "soffione"}, []string{"urn", "subject", "features", "urls"}, []string{"body"}},
		{[]string{"ads", "recommended", "--urn", "id:ad:x:list:1"}, []string{"urn", "subject", "features", "urls"}, []string{"body"}},
		{[]string{"categories", "filters", "2", "s"}, []string{"uri", "label", "query_string", "datasource"}, []string{"chip"}},
	} {
		t.Run(strings.Join(tc.args[:2], " "), func(t *testing.T) {
			testenv.Isolate(t)
			srv, _ := fakeHades(t, 1)
			out, stderr, err := runSubito(t, srv, append(tc.args, "--agent")...)
			if err != nil {
				t.Fatalf("error = %v, stderr:\n%s", err, stderr)
			}
			var env struct {
				Results []map[string]any
			}
			if err := json.Unmarshal([]byte(out), &env); err != nil || len(env.Results) != 1 {
				t.Fatalf("want an envelope with 1 result (err %v):\n%s", err, out)
			}
			for _, k := range tc.want {
				if _, ok := env.Results[0][k]; !ok {
					t.Errorf("result lacks %q:\n%s", k, out)
				}
			}
			for _, k := range tc.drop {
				if _, ok := env.Results[0][k]; ok {
					t.Errorf("compact output kept %q:\n%s", k, out)
				}
			}
		})
	}
}

func exportedLines(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	n := 0
	for s := bufio.NewScanner(f); s.Scan(); n++ {
	}
	return n
}

func TestExportAdsPagesWithSearchDefaults(t *testing.T) {
	testenv.Isolate(t)
	srv, searches := fakeHades(t, 250)
	file := filepath.Join(t.TempDir(), "ads.jsonl")
	if _, stderr, err := runSubito(t, srv, "export", "ads", "--limit", "150", "--output", file); err != nil {
		t.Fatalf("error = %v, stderr:\n%s", err, stderr)
	}
	if n := exportedLines(t, file); n != 150 {
		t.Errorf("exported %d listings, want 150 across pages", n)
	}
	got := searches()
	if len(got) != 2 {
		t.Fatalf("search requests = %d, want 2 pages: %v", len(got), got)
	}
	for _, q := range got {
		if q.Get("t") != "s" || q.Get("sort") != "datedesc" {
			t.Errorf("request lacks the sync defaults t=s, sort=datedesc: %v", q)
		}
	}
	if got[1].Get("start") != "100" {
		t.Errorf("second page start = %q, want 100", got[1].Get("start"))
	}
}

func TestExportAdsWithoutLimitStopsAndSaysSo(t *testing.T) {
	testenv.Isolate(t)
	srv, _ := fakeHades(t, 250)
	file := filepath.Join(t.TempDir(), "ads.jsonl")
	_, stderr, err := runSubito(t, srv, "export", "ads", "--output", file)
	if err != nil {
		t.Fatalf("error = %v, stderr:\n%s", err, stderr)
	}
	if n := exportedLines(t, file); n != exportAdsDefaultLimit {
		t.Errorf("exported %d listings, want %d", n, exportAdsDefaultLimit)
	}
	if !strings.Contains(stderr, "hint: export ads stops at") {
		t.Errorf("no hint about the default cap on stderr:\n%s", stderr)
	}
}

func TestAdsCheckDoesNotWarn(t *testing.T) {
	testenv.Isolate(t)
	srv, _ := fakeHades(t, 1)
	// The store's write-through warning goes to os.Stderr, not the command's.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	realStderr := os.Stderr
	os.Stderr = w
	out, stderr, err := runSubito(t, srv, "ads", "check", "--query", "soffione", "--last", "id:ad:x:list:1", "--agent")
	os.Stderr = realStderr
	_ = w.Close()
	var procStderr bytes.Buffer
	_, _ = procStderr.ReadFrom(r)
	if err != nil {
		t.Fatalf("error = %v, stderr:\n%s", err, stderr)
	}
	if all := strings.TrimSpace(stderr + procStderr.String()); all != "" {
		t.Errorf("stderr = %q, want empty", all)
	}
	if !strings.Contains(out, `"newads"`) {
		t.Errorf("output lacks the check answer:\n%s", out)
	}
}
