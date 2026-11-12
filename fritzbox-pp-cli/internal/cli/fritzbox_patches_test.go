// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fritzbox-pp-cli/internal/fritzbox"
)

// fakeQueryBox answers /query.lua the way FRITZ!OS does: each requested
// name maps to its expression's value.
func fakeQueryBox(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/query.lua" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Has("ssid") {
			_, _ = w.Write([]byte(`{"ssid":"Home","enabled":"1"}`))
			return
		}
		_, _ = w.Write([]byte(`{"result":"Home"}`))
	}))
	t.Cleanup(srv.Close)
	home := t.TempDir()
	t.Setenv("FRITZBOX_ADDRESS", srv.URL)
	t.Setenv("FRITZBOX_BASE_URL", srv.URL)
	t.Setenv("FRITZBOX_SID", "abc")
	t.Setenv("FRITZBOX_PASSWORD", "p")
	t.Setenv("FRITZBOX_CONFIG", filepath.Join(home, "config.toml"))
	t.Setenv("FRITZBOX_NO_LEARN", "true")
}

// runCapturingStderr runs the CLI and returns everything written to stderr,
// the process's included: the store warns on os.Stderr, not the command's.
func runCapturingStderr(t *testing.T, args ...string) (string, string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	realStderr := os.Stderr
	os.Stderr = w
	root := RootCmd()
	var out, cmdErr bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&cmdErr)
	root.SetArgs(append(args, "--agent", "--home", t.TempDir()))
	runErr := root.Execute()
	os.Stderr = realStderr
	_ = w.Close()
	var proc bytes.Buffer
	_, _ = proc.ReadFrom(r)
	if runErr != nil {
		t.Fatalf("%v: %v\n%s%s", args, runErr, cmdErr.String(), proc.String())
	}
	return out.String(), strings.TrimSpace(cmdErr.String() + proc.String())
}

// Reprint check for .printing-press-patches/query-live.md, and for the wlan
// override in internal/store/fritzbox_id_fields.go.
func TestQueryAndWlanDoNotWarn(t *testing.T) {
	fakeQueryBox(t)
	for _, args := range [][]string{{"query", "--result", "wlan:settings/ssid"}, {"wlan"}} {
		out, stderr := runCapturingStderr(t, args...)
		if stderr != "" {
			t.Errorf("%v: stderr = %q, want empty", args, stderr)
		}
		if !strings.Contains(out, "Home") {
			t.Errorf("%v: output lacks the answer:\n%s", args, out)
		}
	}
}

func TestTR064CredentialsAreAnAuthError(t *testing.T) {
	err := fbErr(fmt.Errorf("calling Hosts1: %w", fritzbox.ErrTR064Credentials))
	var ce *cliError
	if !errors.As(err, &ce) || ce.code != 4 {
		t.Fatalf("fbErr(rejected TR-064 credentials) = %#v, want exit code 4", err)
	}
}
