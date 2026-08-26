// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import "github.com/spf13/cobra"

// The novel commands hang off the parents this CLI already defines, so the
// scaffolded parent wrappers for calls, log, portmap, and wan were removed
// rather than left to shadow the richer hand-authored ones.
func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		attachTo := map[string][]*cobra.Command{
			"hosts":   {newNovelHostsDiffCmd(flags)},
			"wan":     {newNovelWanHistoryCmd(flags)},
			"log":     {newNovelLogSearchCmd(flags)},
			"calls":   {newNovelCallsDigestCmd(flags), newNovelCallsUnknownCmd(flags)},
			"portmap": {newNovelPortmapAuditCmd(flags)},
		}
		for parentName, children := range attachTo {
			parent := fbFind(root, parentName)
			if parent == nil {
				continue
			}
			for _, child := range children {
				fbAttach(parent, child)
			}
		}
	})
}
