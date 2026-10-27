// Hand-written: --agent output of ads search keeps what identifies a listing.

package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"subito-pp-cli/internal/cliutil/testenv"
)

func TestAdsSearchAgentKeepsListingFields(t *testing.T) {
	testenv.Isolate(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/search/items" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"count_all":1,"ads":[{"urn":"id:ad:x:list:1","type":{"key":"s"},"subject":"Soffione",` +
			`"body":"lungo testo","features":[{"uri":"/price","values":[{"key":"40"}]}],"urls":{"default":"https://www.subito.it/x-1.htm"}}]}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SUBITO_BASE_URL", srv.URL+"/v1")

	cmd := RootCmd()
	cmd.SetArgs([]string{"ads", "search", "--query", "soffione", "--agent", "--no-cache"})
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("error = %v, stderr:\n%s", err, errOut.String())
	}
	var env struct {
		Results []map[string]any
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("output is not an envelope: %v\n%s", err, out.String())
	}
	if len(env.Results) != 1 {
		t.Fatalf("results = %d, want 1:\n%s", len(env.Results), out.String())
	}
	for _, k := range []string{"urn", "subject", "features", "urls"} {
		if _, ok := env.Results[0][k]; !ok {
			t.Errorf("result lacks %q:\n%s", k, out.String())
		}
	}
	if _, ok := env.Results[0]["body"]; ok {
		t.Errorf("compact output kept body:\n%s", out.String())
	}
}
