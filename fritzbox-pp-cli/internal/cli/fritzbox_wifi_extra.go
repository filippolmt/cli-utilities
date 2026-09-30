// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/spf13/cobra"
)

// renderQR draws a scannable QR code for payload using half-block characters.
//
// A QR encoder is Reed-Solomon coding plus mask selection, which is well past
// the point where hand-rolling beats a small dependency.
func renderQR(w io.Writer, payload string) error {
	// Medium recovery is the level phone cameras are tuned for and keeps the
	// code small enough to fit an ordinary terminal.
	code, err := qrcode.New(payload, qrcode.Medium)
	if err != nil {
		return fmt.Errorf("encoding the QR code: %w", err)
	}
	code.DisableBorder = false
	_, err = fmt.Fprint(w, code.ToSmallString(false))
	return err
}

// writeQRPNG writes the QR code for payload to path as a PNG.
func writeQRPNG(path, payload string, size int) error {
	if size <= 0 {
		size = 512
	}
	png, err := qrcode.Encode(payload, qrcode.Medium, size)
	if err != nil {
		return fmt.Errorf("encoding the QR code: %w", err)
	}
	return writeSecretFile(path, png)
}

// writeSecretFile writes data with owner-only permissions.
//
// Every file this CLI writes carries credentials of some kind: a configuration
// export contains every stored password, and a Wi-Fi QR code encodes the
// pre-shared key. Mode 0600 is applied at creation rather than by a follow-up
// chmod, so there is no window where the file is world-readable.
func writeSecretFile(path string, data []byte) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("creating the output directory: %w", err)
		}
	}
	// #nosec G304 -- path is the destination the user asked for on the command
	// line; refusing to honour it would defeat the command.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", path, err)
	}
	return nil
}

func newWifiChannelCmd(flags *rootFlags) *cobra.Command {
	var band string
	var channel int
	var confirm bool
	cmd := &cobra.Command{
		Use:   "channel",
		Short: "Show the current radio channel, or move the network to another one",
		Long: `Show which channel a wireless network is on, and the channels the router
considers usable. Pass --set together with --confirm to move the network.

Changing channel briefly drops every client on that radio.`,
		Example:     "  fritzbox-pp-cli wifi channel --band 2.4 --agent",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := wifiServiceFor(band)
			if err != nil {
				return err
			}
			if dryRunOK(flags) {
				return fbDryRun(cmd, flags, "read or change the wireless channel", map[string]any{"band": band})
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			box, err := newBox(flags)
			if err != nil {
				return err
			}
			if channel == 0 {
				out, err := box.Call(ctx, svc, "GetChannelInfo", nil)
				if err != nil {
					return err
				}
				return fbEmitObject(cmd, flags, map[string]any{
					"band":                 band,
					"channel":              out["Channel"],
					"auto_channel_enabled": out["X_AVM-DE_AutoChannelEnabled"] == "1",
					"possible_channels":    out["PossibleChannels"],
				})
			}
			if !confirm {
				return fbDryRun(cmd, flags, "move the wireless network to another channel (pass --confirm to actually do it)",
					map[string]any{"band": band, "channel": channel})
			}
			if refused, err := fbRefuseUnderHarness(cmd, flags, "change the wireless channel"); refused {
				return err
			}
			if _, err := box.Call(ctx, svc, "SetChannel", map[string]string{"Channel": strconv.Itoa(channel)}); err != nil {
				return err
			}
			return fbEmitObject(cmd, flags, map[string]any{"band": band, "channel": channel, "changed": true})
		},
	}
	cmd.Flags().StringVar(&band, "band", "2.4", "Which network to act on: 2.4, 5, or guest")
	cmd.Flags().IntVar(&channel, "set", 0, "Move the network to this channel; omit to just read the current one")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Actually change the channel instead of printing what would happen")
	return cmd
}
