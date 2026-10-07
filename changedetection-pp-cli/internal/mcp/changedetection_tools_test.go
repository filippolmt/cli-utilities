// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-authored. Not generated; survives a reprint.

package mcp

import (
	"regexp"
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

// stateChangingParam names the query parameters changedetection changes state
// through on a GET: ?paused=, ?muted=, ?recheck=, ?recheck_all=.
var stateChangingParam = regexp.MustCompile(`^(paused|muted)$|recheck`)

// A tool that can pause, mute or recheck watches must not be read-only, or an
// MCP client may auto-approve it.
func TestStateChangingToolsAreNotReadOnly(t *testing.T) {
	s := server.NewMCPServer("changedetection", "test")
	RegisterTools(s)
	found := 0
	for name, tool := range s.ListTools() {
		for param := range tool.Tool.InputSchema.Properties {
			if !stateChangingParam.MatchString(param) {
				continue
			}
			found++
			if hint := tool.Tool.Annotations.ReadOnlyHint; hint == nil || *hint {
				t.Errorf("%s takes %q but readOnlyHint = %v, want false", name, param, hint)
			}
		}
	}
	if found == 0 {
		t.Fatal("no state-changing tool parameters found; is stateChangingParam stale?")
	}
}
