// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"fritzbox-pp-cli/internal/cliutil"
	"fritzbox-pp-cli/internal/config"
	"fritzbox-pp-cli/internal/fritzbox"
	"fritzbox-pp-cli/internal/store"

	"github.com/spf13/cobra"
)

// fbErr maps a transport error onto the CLI's exit-code contract.
//
// The FRITZ!Box transports return errors that already carry the distinction
// callers act on: a missing password is an auth failure (exit 4) and a
// saturated router is a rate limit (exit 7). Wrapping those in apiErr would
// flatten every one of them to exit 5, so an agent asked to fix its
// credentials would instead be told the API broke. classifyAPIError already
// short-circuits on an error that carries a typed code, so this only has to
// add the transport-level throttle the generated classifier cannot see.
func fbErr(err error) error {
	if err == nil {
		return nil
	}
	var throttle *fritzbox.ThrottleError
	if errors.As(err, &throttle) {
		return rateLimitErr(err)
	}
	return classifyAPIError(err, nil)
}

// fbBox bundles everything a FRITZ!Box command needs: configuration, the two
// transports, and a lazily minted session.
type fbBox struct {
	cfg     *config.Config
	tr      *fritzbox.TR064
	web     *fritzbox.Web
	flags   *rootFlags
	limiter *cliutil.AdaptiveLimiter

	mu       sync.Mutex
	sid      string
	services []fritzbox.Service
}

// newBox wires a FRITZ!Box client set from configuration and environment.
func newBox(flags *rootFlags) (*fbBox, error) {
	cfg, err := config.Load(flags.configPath)
	if err != nil {
		return nil, configErr(err)
	}
	timeout := flags.timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	hc := &http.Client{Timeout: timeout}

	b := &fbBox{cfg: cfg, flags: flags}
	// One limiter for both transports and the login handshake: they all reach
	// the same box, so a limiter per transport would let two paced streams add
	// up to an unpaced one.
	b.limiter = fritzbox.NewLimiter(flags.rateLimit)
	b.tr = fritzbox.NewTR064(cfg.FritzboxAddress(), cfg.FritzboxUsername(), cfg.FritzboxPasswordValue(), hc, b.limiter)
	b.web = fritzbox.NewWeb(cfg.FritzboxAddress(), hc, b, b.limiter)
	b.sid = cfg.FritzboxSession()
	return b, nil
}

// requireCredentials fails early with a typed auth error when no password is
// configured, so commands report a credential problem rather than a transport
// error from the router.
func (b *fbBox) requireCredentials() error {
	if b.cfg.FritzboxPasswordValue() == "" {
		return authErr(fmt.Errorf("no FRITZ!Box password configured; set FRITZBOX_PASSWORD (and FRITZBOX_USERNAME if the box has more than one user)"))
	}
	return nil
}

// SID implements fritzbox.SessionProvider. It reuses a cached session id and
// only performs the handshake when none is available.
func (b *fbBox) SID(ctx context.Context) (string, error) {
	b.mu.Lock()
	cached := b.sid
	b.mu.Unlock()
	if cached != "" && cached != fritzbox.InvalidSID {
		return cached, nil
	}
	return b.Refresh(ctx)
}

// Refresh implements fritzbox.SessionProvider by forcing a new handshake and
// persisting the result so the next process reuses it.
func (b *fbBox) Refresh(ctx context.Context) (string, error) {
	if err := b.requireCredentials(); err != nil {
		return "", err
	}
	res, err := fritzbox.Login(ctx, &http.Client{Timeout: b.httpTimeout()}, b.cfg.FritzboxAddress(), b.cfg.FritzboxUsername(), b.cfg.FritzboxPasswordValue(), b.limiter)
	if err != nil {
		return "", authErr(err)
	}
	b.mu.Lock()
	b.sid = res.SID
	b.mu.Unlock()
	// A failure to cache is not fatal: the session works for this process, and
	// the next one simply performs the handshake again.
	if saveErr := b.cfg.SetFritzboxSession(res.SID); saveErr != nil {
		fmt.Fprintf(os.Stderr, "warning: could not cache the session id: %v\n", saveErr)
	}
	return res.SID, nil
}

func (b *fbBox) httpTimeout() time.Duration {
	if b.flags != nil && b.flags.timeout > 0 {
		return b.flags.timeout
	}
	return 30 * time.Second
}

// Services returns the router's TR-064 service catalog, fetched once per process.
func (b *fbBox) Services(ctx context.Context) ([]fritzbox.Service, error) {
	b.mu.Lock()
	cached := b.services
	b.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	if err := b.requireCredentials(); err != nil {
		return nil, err
	}
	svcs, err := b.tr.Describe(ctx)
	if err != nil {
		return nil, fbErr(err)
	}
	b.mu.Lock()
	b.services = svcs
	b.mu.Unlock()
	return svcs, nil
}

// Service resolves a service by its short name, case-insensitively.
//
// A bare family name such as "WLANConfiguration" resolves to the lowest
// numbered instance, because that is what a user typing it means; the explicit
// "WLANConfiguration2" form still selects a specific one.
func (b *fbBox) Service(ctx context.Context, name string) (fritzbox.Service, error) {
	svcs, err := b.Services(ctx)
	if err != nil {
		return fritzbox.Service{}, err
	}
	want := strings.ToLower(strings.TrimSpace(name))
	if want == "" {
		return fritzbox.Service{}, usageErr(fmt.Errorf("a service name is required"))
	}
	var prefixMatches []fritzbox.Service
	for _, s := range svcs {
		got := strings.ToLower(s.Name())
		if got == want {
			return s, nil
		}
		if strings.HasPrefix(got, want) {
			prefixMatches = append(prefixMatches, s)
		}
	}
	if len(prefixMatches) > 0 {
		sort.Slice(prefixMatches, func(i, j int) bool { return prefixMatches[i].Name() < prefixMatches[j].Name() })
		return prefixMatches[0], nil
	}
	return fritzbox.Service{}, notFoundErr(fmt.Errorf("no TR-064 service named %q on this router; run 'fritzbox-pp-cli tr064 services' to list them", name))
}

// Call resolves a service by name and invokes one action on it.
func (b *fbBox) Call(ctx context.Context, service, action string, args map[string]string) (map[string]string, error) {
	svc, err := b.Service(ctx, service)
	if err != nil {
		return nil, err
	}
	out, err := b.tr.Call(ctx, svc, action, args)
	if err != nil {
		return nil, fbErr(err)
	}
	return out, nil
}

// openStore opens the local database, creating the FRITZ!Box tables if needed.
//
// The open is retried once. Several processes creating the same database at the
// same moment can briefly observe it as unreadable, which surfaces as
// SQLITE_NOTADB rather than as a lock error, so a single short retry turns a
// hard failure into a pause. A genuinely corrupt file still fails on the
// second attempt.
func (b *fbBox) openStore(ctx context.Context, dbPath string) (*store.Store, error) {
	if dbPath == "" {
		dbPath = defaultDBPath("fritzbox-pp-cli")
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
		db, err := store.OpenWithContext(ctx, dbPath)
		if err != nil {
			lastErr = fmt.Errorf("opening the local database: %w", err)
			continue
		}
		if err := db.EnsureFritzboxSchema(ctx); err != nil {
			_ = db.Close()
			lastErr = err
			continue
		}
		return db, nil
	}
	return nil, lastErr
}

// fbDryRun writes the standard dry-run envelope and is the only thing a
// dry-run branch should emit.
//
// The generator in use does not emit a writeDryRun helper, so this is the local
// equivalent. Machine formats get a parseable object because an empty stdout
// under --json fails the live output-fidelity check.
func fbDryRun(cmd *cobra.Command, flags *rootFlags, action string, detail map[string]any) error {
	out := cmd.OutOrStdout()
	if !wantsHumanTable(out, flags) {
		payload := map[string]any{"dry_run": true, "action": action}
		if len(detail) > 0 {
			payload["would"] = detail
		}
		return printJSONFiltered(out, payload, flags)
	}
	fmt.Fprintf(out, "dry run: would %s\n", action)
	keys := make([]string, 0, len(detail))
	for k := range detail {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(out, "  %s: %v\n", k, detail[k])
	}
	return nil
}

// fbRefuseUnderHarness declines a visible side effect while a verification or
// dogfood harness is driving the CLI.
//
// Print-by-default plus an opt-in flag is not enough on its own: a live dogfood
// matrix runs real commands against a real router, so a reboot or a wireless
// shutdown would reach the operator's actual network.
func fbRefuseUnderHarness(cmd *cobra.Command, flags *rootFlags, action string) (bool, error) {
	// This generator version emits IsVerifyEnv and IsDogfoodEnv but no combined
	// helper, so both harnesses are checked explicitly.
	if !cliutil.IsVerifyEnv() && !cliutil.IsDogfoodEnv() {
		return false, nil
	}
	out := cmd.OutOrStdout()
	if !wantsHumanTable(out, flags) {
		return true, printJSONFiltered(out, map[string]any{
			"refused": true,
			"action":  action,
			"reason":  "running under a verification or dogfood harness; side effects on a live router are suppressed",
		}, flags)
	}
	fmt.Fprintf(out, "refused: would %s, but a verification harness is active\n", action)
	return true, nil
}

// fbEmit renders rows as machine output or as the generated human table.
func fbEmit(cmd *cobra.Command, flags *rootFlags, rows []map[string]any, empty string) error {
	out := cmd.OutOrStdout()
	if !wantsHumanTable(out, flags) {
		return printJSONFiltered(out, rows, flags)
	}
	if len(rows) == 0 {
		fmt.Fprintln(out, empty)
		return nil
	}
	return printAutoTable(out, rows)
}

// fbEmitObject renders a single object as machine output or key/value lines.
func fbEmitObject(cmd *cobra.Command, flags *rootFlags, obj map[string]any) error {
	out := cmd.OutOrStdout()
	if !wantsHumanTable(out, flags) {
		return printJSONFiltered(out, obj, flags)
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(out, "%-28s %v\n", k, obj[k])
	}
	return nil
}

// soapToRow converts SOAP output arguments into a stable ordered row.
func soapToRow(out map[string]string) map[string]any {
	row := make(map[string]any, len(out))
	for k, v := range out {
		row[k] = v
	}
	return row
}

// jsonPath walks a decoded JSON document by successive object keys.
func jsonPath(raw json.RawMessage, keys ...string) (json.RawMessage, bool) {
	cur := raw
	for _, key := range keys {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(cur, &obj); err != nil {
			return nil, false
		}
		next, ok := obj[key]
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}
