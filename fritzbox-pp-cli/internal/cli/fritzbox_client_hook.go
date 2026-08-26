// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"net/http"
	"time"

	"fritzbox-pp-cli/internal/client"
	"fritzbox-pp-cli/internal/fritzbox"
)

// The generated endpoints authenticate by sending a session id as the `sid`
// query parameter. That id has to be minted through the challenge-response
// handshake, which the generated client knows nothing about, so this hook
// supplies one before any generated command issues a request.
func init() {
	registerClientHook(func(c *client.Client) error {
		if c == nil || c.Config == nil {
			return nil
		}
		cfg := c.Config
		if sid := cfg.FritzboxSession(); sid != "" && sid != fritzbox.InvalidSID {
			cfg.AuthHeaderVal = sid
			return nil
		}
		if cfg.FritzboxPasswordValue() == "" {
			// No credentials at all: leave the client as it is so the generated
			// command reports the missing-credential error it already has.
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		res, err := fritzbox.Login(ctx, &http.Client{Timeout: 20 * time.Second},
			cfg.FritzboxAddress(), cfg.FritzboxUsername(), cfg.FritzboxPasswordValue(),
			fritzbox.NewLimiter(0))
		if err != nil {
			// A handshake failure is reported by the command that follows, with
			// the request context that makes it actionable.
			return nil
		}
		cfg.AuthHeaderVal = res.SID
		// Persisting is best effort: the id already works for this process.
		_ = cfg.SetFritzboxSession(res.SID)
		return nil
	})
}
