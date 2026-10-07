// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-authored. Not generated; survives a reprint.

package mcp

import (
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

// These tools can pause, mute or recheck watches, so an MCP client must not
// auto-approve them as read-only.
func TestStateChangingToolsAreNotReadOnly(t *testing.T) {
	s := server.NewMCPServer("changedetection", "test")
	RegisterTools(s)
	tools := s.ListTools()
	for _, name := range []string{"watch_get", "tag_get", "watch_list-watches"} {
		tool, ok := tools[name]
		if !ok {
			t.Fatalf("%s tool missing", name)
		}
		if hint := tool.Tool.Annotations.ReadOnlyHint; hint == nil || *hint {
			t.Errorf("%s readOnlyHint = %v, want false", name, hint)
		}
	}
}
