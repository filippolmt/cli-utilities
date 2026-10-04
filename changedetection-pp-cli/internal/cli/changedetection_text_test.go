// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-authored. Not generated; survives a reprint.

package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/spf13/cobra"
)

func TestPrintTextBody(t *testing.T) {
	run := func(flags *rootFlags, body string) (bool, string) {
		var out bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&out)
		handled, err := printTextBody(cmd, flags, json.RawMessage(body), DataProvenance{Source: "live"})
		if err != nil {
			t.Fatal(err)
		}
		return handled, out.String()
	}

	if handled, _ := run(&rootFlags{}, `{"a":1}`); handled {
		t.Fatal("JSON body must be left to the normal pipeline")
	}

	// A bytes.Buffer is not a terminal, so this takes the machine path.
	_, got := run(&rootFlags{}, "2026-10-08 19:30\n")
	var env struct{ Results string }
	if err := json.Unmarshal([]byte(got), &env); err != nil || env.Results != "2026-10-08 19:30\n" {
		t.Fatalf("machine output = %q (err %v)", got, err)
	}

	if _, got := run(&rootFlags{quiet: true}, "x"); got != "" {
		t.Fatalf("--quiet printed %q", got)
	}
}
