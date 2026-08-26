// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// FRITZ!Box uses one credential pair over two different mechanics: HTTP Digest
// for TR-064, and a challenge-response handshake that mints a session id for
// the smart-home and web-data surfaces. The generated `auth set-token` stores a
// session id directly; `auth login` is what actually mints one from the
// username and password, which is the flow users have.
func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		if parent := fbFind(root, "auth"); parent != nil {
			fbAttach(parent, newFritzboxAuthLoginCmd(flags))
		}
	})
}

func newFritzboxAuthLoginCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Mint and cache a router session id from your username and password",
		Long: `Perform the FRITZ!OS challenge-response handshake and cache the resulting
session id.

Set FRITZBOX_USERNAME and FRITZBOX_PASSWORD first. Modern firmware uses PBKDF2
for the handshake and older firmware uses MD5; both are handled automatically.
The session id is refreshed on its own when it expires, so this normally only
needs running once.`,
		Example: "  fritzbox-pp-cli auth login",
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "mint a router session id", nil)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			if err := box.requireCredentials(); err != nil {
				return err
			}
			// Refresh rather than SID: an explicit login must always perform a
			// new handshake, otherwise it would silently report success from a
			// cached id that may already have been revoked.
			if _, err := box.Refresh(ctx); err != nil {
				return err
			}
			res, err := box.Call(ctx, "DeviceInfo1", "GetInfo", nil)
			if err != nil {
				// The session was minted; a TR-064 failure means the account
				// works for the web surface but not for the control interface.
				return authErr(fmt.Errorf("session id minted, but TR-064 rejected the same credentials: %w; enable TR-064 under Home Network, Network Settings, Allow access for applications", err))
			}
			return fbEmitObject(cmd, flags, map[string]any{
				"authenticated": true,
				"model":         res["ModelName"],
				"firmware":      res["SoftwareVersion"],
				"note":          "session id cached; both TR-064 and the smart-home interface are reachable",
			})
		},
	}
	return cmd
}
