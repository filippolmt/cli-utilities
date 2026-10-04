// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-authored. Not generated; survives a reprint.

package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// printTextBody prints a non-JSON response body (the snapshot and difference
// endpoints answer text/plain or text/html). It reports false when data is
// JSON, leaving the caller's normal output pipeline in charge. Machine output
// gets the text as a JSON string inside the usual provenance envelope.
func printTextBody(cmd *cobra.Command, flags *rootFlags, data json.RawMessage, prov DataProvenance) (bool, error) {
	if json.Valid(data) {
		return false, nil
	}
	out := cmd.OutOrStdout()
	if flags.quiet {
		return true, nil
	}
	if flags.asJSON || (!isTerminal(out) && !flags.csv && !flags.plain) {
		s, err := json.Marshal(string(data))
		if err != nil {
			return true, err
		}
		wrapped, err := wrapWithProvenance(s, prov)
		if err != nil {
			return true, err
		}
		return true, printOutput(out, wrapped, true)
	}
	_, err := fmt.Fprintln(out, strings.TrimRight(string(data), "\n"))
	return true, err
}
