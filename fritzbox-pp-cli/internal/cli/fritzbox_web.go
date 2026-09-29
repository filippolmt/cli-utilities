// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		fbAttach(root, newWebCmd(flags))
	})
}

func newWebCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "web", Short: "Raw read access to the router's web UI data pages"}
	var reveal bool
	page := &cobra.Command{
		Use:   "page <name>",
		Short: "Print the data behind one web UI page as JSON",
		Long: `Print the data the web UI loads for one page (data.lua), for settings
TR-064 does not expose: DNS servers (dnsSrv), port sharing (portoverview),
WireGuard (shareWireguard), channels (chan), LEDs (led), network (netSet).

Read-only: no form values are sent, so nothing is applied. An unknown page
name returns the overview page instead of an error, and a few pages load their
data only on request and come back empty here (netDev: use 'hosts cleanup'). Passwords, pre-shared
keys and other secrets are masked unless --reveal is passed.`,
		Example:     "  fritzbox-pp-cli web page dnsSrv --agent",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:happy-args": "name=dnsSrv"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read a web UI page", nil)
			}
			if len(args) != 1 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("exactly one page name is required, for example dnsSrv"))
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			raw, err := box.web.Data(ctx, args[0], nil)
			if err != nil {
				return fbErr(err)
			}
			data, ok := jsonPath(raw, "data")
			if !ok {
				return apiErr(fmt.Errorf("router returned no data for page %q", args[0]))
			}
			var v any
			if err := json.Unmarshal(data, &v); err != nil {
				return apiErr(fmt.Errorf("parsing page %q: %w", args[0], err))
			}
			if !reveal {
				v = redactSecrets(v)
			}
			return fbPrintJSON(cmd.OutOrStdout(), v, flags, "live")
		},
	}
	page.Flags().BoolVar(&reveal, "reveal", false, "Show passwords and keys instead of masking them")
	cmd.AddCommand(page)
	return cmd
}

// secretKey matches field names that carry credentials in FRITZ!OS pages.
// Public keys are not secret and stay visible (see isSecretKey).
// ponytail: name heuristic, so a secret under an unusual key name slips
// through; extend the pattern when one shows up.
var secretKey = regexp.MustCompile(`(?i)(psk|passw|passphrase|^pass$|_pass$|secret|token|pwd|pin$|(^|_)key$|privatekey)`)

func isSecretKey(k string) bool {
	return secretKey.MatchString(k) && !strings.Contains(strings.ToLower(k), "public")
}

// redactSecrets masks every non-empty scalar stored under a secret-looking
// key, and the value of {"name": <secret-looking>, "value": ...} pairs, the
// shape FRITZ!OS form pages use.
func redactSecrets(v any) any {
	switch t := v.(type) {
	case map[string]any:
		if name, ok := t["name"].(string); ok && isSecretKey(name) {
			if _, ok := t["value"]; ok && !isEmptyScalar(t["value"]) {
				t["value"] = "***"
			}
		}
		for k, val := range t {
			if isSecretKey(k) && !isEmptyScalar(val) && !isContainer(val) {
				t[k] = "***"
				continue
			}
			t[k] = redactSecrets(val)
		}
	case []any:
		for i, val := range t {
			t[i] = redactSecrets(val)
		}
	}
	return v
}

func isContainer(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return true
	}
	return false
}

func isEmptyScalar(v any) bool {
	return v == nil || v == ""
}
