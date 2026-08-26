// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package fritzbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"fritzbox-pp-cli/internal/cliutil"
)

// maxWebBody caps a single web-surface response read. The system log is the
// largest payload involved and sits in the low hundreds of kilobytes.
const maxWebBody = 32 << 20

// ErrSessionExpired reports that the router rejected the session id. Callers
// mint a fresh one and retry once.
var ErrSessionExpired = fmt.Errorf("router session id expired or invalid")

// SessionProvider hands out a session id and can force a fresh handshake.
// The concrete implementation lives in the CLI layer so this package stays
// free of config and filesystem concerns.
type SessionProvider interface {
	SID(ctx context.Context) (string, error)
	Refresh(ctx context.Context) (string, error)
}

// Web talks to the two session-authenticated surfaces: the smart-home AHA
// interface and the web UI's data endpoint.
type Web struct {
	base    string
	hc      *http.Client
	limiter *cliutil.AdaptiveLimiter
	session SessionProvider
}

// NewWeb builds a client bound to a session provider.
// NewWeb builds the session-authenticated transport. limiter paces every
// request and should be the same limiter the TR-064 transport was given: both
// land on the same embedded CPU, so pacing them independently would let two
// half-rate streams add up to a full-rate burst.
func NewWeb(base string, hc *http.Client, session SessionProvider, limiter *cliutil.AdaptiveLimiter) *Web {
	return &Web{base: strings.TrimRight(base, "/"), hc: hc, session: session, limiter: limiter}
}

// AHA issues one smart-home command and returns the raw response body.
//
// The interface answers with XML for the list commands and bare text for
// scalar reads, so the body is returned undecoded and each caller parses the
// shape its command actually expects.
func (w *Web) AHA(ctx context.Context, switchcmd string, params map[string]string) ([]byte, error) {
	return w.withSession(ctx, func(sid string) (*http.Request, error) {
		q := url.Values{}
		q.Set("sid", sid)
		q.Set("switchcmd", switchcmd)
		for k, v := range params {
			if v != "" {
				q.Set(k, v)
			}
		}
		return http.NewRequestWithContext(ctx, http.MethodGet, w.base+"/webservices/homeautoswitch.lua?"+q.Encode(), nil)
	})
}

// Data fetches one page of the web UI's JSON data endpoint.
//
// This endpoint accepts form-encoded POST only: a JSON body is refused with
// "getcgivars(): Unsupported Content-Type", and a GET silently ignores the
// page parameter and returns the overview page instead. Both failure modes are
// silent-wrong-answer shaped, which is why the encoding is pinned here.
func (w *Web) Data(ctx context.Context, page string, extra map[string]string) (json.RawMessage, error) {
	raw, err := w.withSession(ctx, func(sid string) (*http.Request, error) {
		form := url.Values{}
		form.Set("xhr", "1")
		form.Set("sid", sid)
		form.Set("page", page)
		form.Set("no_sidrenew", "")
		for k, v := range extra {
			form.Set(k, v)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.base+"/data.lua", strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return req, nil
	})
	if err != nil {
		return nil, err
	}
	if !json.Valid(raw) {
		// An expired session makes this endpoint serve the HTML login page with
		// HTTP 200, so an unparseable body is a session problem, not a bug.
		return nil, fmt.Errorf("router returned a non-JSON body for page %q; the session id was probably rejected", page)
	}
	return json.RawMessage(raw), nil
}

// Fetch retrieves a session-authenticated URL the router itself handed us.
//
// Call lists, phonebooks and answering-machine messages are not returned
// inline: the SOAP action answers with a download URL that already carries a
// session id. Tools that stop at the SOAP response ship empty output, so this
// second hop is a required part of those commands, not an optimisation.
func (w *Web) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	w.limiter.Wait()
	resp, err := w.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", redactSID(rawURL), err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		return nil, newThrottleError(resp, redactSID(rawURL))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("router returned HTTP %d for %s", resp.StatusCode, redactSID(rawURL))
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxWebBody))
}

// withSession runs build with a valid session id, minting a fresh one and
// retrying exactly once when the router rejects the current one.
func (w *Web) withSession(ctx context.Context, build func(sid string) (*http.Request, error)) ([]byte, error) {
	sid, err := w.session.SID(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := w.roundTrip(ctx, build, sid)
	if err == nil {
		return raw, nil
	}
	if err != ErrSessionExpired {
		return nil, err
	}
	sid, err = w.session.Refresh(ctx)
	if err != nil {
		return nil, err
	}
	raw, err = w.roundTrip(ctx, build, sid)
	if err == ErrSessionExpired {
		return nil, fmt.Errorf("router rejected a freshly minted session id; check that the account has the required rights")
	}
	return raw, err
}

func (w *Web) roundTrip(ctx context.Context, build func(sid string) (*http.Request, error), sid string) ([]byte, error) {
	req, err := build(sid)
	if err != nil {
		return nil, err
	}
	w.limiter.Wait()
	resp, err := w.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reaching the router at %s: %w", w.base, err)
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		w.limiter.OnRateLimit()
	} else {
		w.limiter.OnSuccess()
	}
	defer func() { _ = resp.Body.Close() }()
	// Ahead of the session branch: a throttled box must not be mistaken for an
	// expired session, or withSession answers the throttle with a fresh login
	// handshake and a retry, adding load to a box that just asked for less.
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		return nil, newThrottleError(resp, w.base)
	}
	// FRITZ!OS answers an unusable session id with 403 on the smart-home
	// interface and with a login page on the data endpoint.
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrSessionExpired
	}
	if resp.StatusCode == http.StatusBadRequest {
		return nil, fmt.Errorf("router rejected the request as malformed; the command or its arguments are not supported by this firmware")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("router returned HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxWebBody))
}

// redactSID removes a session id from a URL before it reaches an error message.
// These URLs come straight from the router and carry a live credential.
func redactSID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "the router-supplied URL"
	}
	q := u.Query()
	if q.Get("sid") != "" {
		q.Set("sid", "REDACTED")
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// Query runs one or more FRITZ!OS data-path expressions through /query.lua and
// returns the decoded object, keyed by the caller-supplied result names.
//
// This is the only surface on the box that answers arbitrary data-path reads as
// JSON over a plain GET, which makes it the cheapest way to pull list data such
// as the known-device table.
func (w *Web) Query(ctx context.Context, expressions map[string]string) (map[string]json.RawMessage, error) {
	if len(expressions) == 0 {
		return nil, fmt.Errorf("query requires at least one data-path expression")
	}
	raw, err := w.withSession(ctx, func(sid string) (*http.Request, error) {
		q := url.Values{}
		q.Set("sid", sid)
		for name, expr := range expressions {
			q.Set(name, expr)
		}
		return http.NewRequestWithContext(ctx, http.MethodGet, w.base+"/query.lua?"+q.Encode(), nil)
	})
	if err != nil {
		return nil, err
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		// An expired session makes this endpoint serve HTML with HTTP 200.
		return nil, fmt.Errorf("router returned a non-JSON query result; the session id was probably rejected")
	}
	return out, nil
}
