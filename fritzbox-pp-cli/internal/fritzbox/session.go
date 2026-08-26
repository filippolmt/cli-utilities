// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

// Package fritzbox implements the three transports an AVM FRITZ!Box actually
// speaks: the challenge-response session handshake, TR-064 SOAP over HTTP
// Digest, and the smart-home AHA HTTP interface.
package fritzbox

import (
	"context"
	"crypto/md5" // #nosec G501 -- required by the legacy FRITZ!OS login challenge; see md5UTF16LE.
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf16"

	"fritzbox-pp-cli/internal/cliutil"
)

// InvalidSID is the all-zero session id FRITZ!OS returns when a login attempt
// is rejected. It is a valid-looking string, not an error status, so callers
// must compare against it explicitly.
const InvalidSID = "0000000000000000"

// sessionInfo is the shape of both the challenge response and the login
// response from /login_sid.lua.
type sessionInfo struct {
	SID       string `xml:"SID"`
	Challenge string `xml:"Challenge"`
	BlockTime int    `xml:"BlockTime"`
	Rights    struct {
		Name   []string `xml:"Name"`
		Access []int    `xml:"Access"`
	} `xml:"Rights"`
}

// Right is one access grant the box reports for the logged-in user.
type Right struct {
	Name   string `json:"name"`
	Access int    `json:"access"`
}

// LoginResult reports the outcome of a successful handshake.
type LoginResult struct {
	SID    string  `json:"sid"`
	Rights []Right `json:"rights"`
}

// BlockedError reports that FRITZ!OS is rate-limiting login attempts. The box
// applies a rising delay after failures, so retrying before BlockTime elapses
// cannot succeed and extends the block further.
type BlockedError struct {
	Seconds int
}

func (e *BlockedError) Error() string {
	return fmt.Sprintf("login temporarily blocked by the router for %d more second(s); wait before retrying", e.Seconds)
}

// Login performs the FRITZ!OS challenge-response handshake and returns a
// session id plus the rights granted to it.
//
// Two challenge formats exist in the wild. FRITZ!OS 7.24 and newer answer with
// "2$<iter1>$<salt1>$<iter2>$<salt2>" and expect two chained PBKDF2-HMAC-SHA256
// rounds. Older firmware answers with an opaque nonce and expects an MD5 of the
// nonce and password encoded as UTF-16LE. Both are implemented because the
// device in front of the CLI decides which one applies.
// limiter paces the two handshake requests; pass nil to leave them unpaced.
func Login(ctx context.Context, hc *http.Client, base, username, password string, limiter *cliutil.AdaptiveLimiter) (*LoginResult, error) {
	first, err := fetchSession(ctx, hc, base+"/login_sid.lua?version=2", limiter)
	if err != nil {
		return nil, err
	}
	if first.BlockTime > 0 {
		return nil, &BlockedError{Seconds: first.BlockTime}
	}
	if first.Challenge == "" {
		return nil, fmt.Errorf("router returned no login challenge; is %s a FRITZ!Box?", base)
	}

	answer, err := challengeResponse(first.Challenge, password)
	if err != nil {
		return nil, err
	}

	q := url.Values{}
	q.Set("version", "2")
	q.Set("username", username)
	q.Set("response", answer)
	second, err := fetchSession(ctx, hc, base+"/login_sid.lua?"+q.Encode(), limiter)
	if err != nil {
		return nil, err
	}
	if second.SID == "" || second.SID == InvalidSID {
		if second.BlockTime > 0 {
			return nil, &BlockedError{Seconds: second.BlockTime}
		}
		return nil, fmt.Errorf("router rejected the credentials; check FRITZBOX_USERNAME and FRITZBOX_PASSWORD")
	}

	out := &LoginResult{SID: second.SID}
	// Name and Access are sibling repeated elements rather than a nested pair,
	// so they are zipped by position; a truncated tail is ignored rather than
	// panicking on a length mismatch.
	for i, name := range second.Rights.Name {
		if i >= len(second.Rights.Access) {
			break
		}
		out.Rights = append(out.Rights, Right{Name: name, Access: second.Rights.Access[i]})
	}
	return out, nil
}

// challengeResponse computes the answer for either challenge generation.
func challengeResponse(challenge, password string) (string, error) {
	if !strings.HasPrefix(challenge, "2$") {
		// Legacy MD5 path: the box hashes the UTF-16LE encoding of
		// "<challenge>-<password>". Characters outside the BMP are replaced
		// because FRITZ!OS itself cannot represent them in this scheme.
		return challenge + "-" + md5UTF16LE(challenge+"-"+password), nil
	}

	parts := strings.Split(challenge, "$")
	if len(parts) != 5 {
		return "", fmt.Errorf("malformed PBKDF2 challenge from router: %q", challenge)
	}
	iter1, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", fmt.Errorf("malformed PBKDF2 challenge: first iteration count %q: %w", parts[1], err)
	}
	salt1, err := hex.DecodeString(parts[2])
	if err != nil {
		return "", fmt.Errorf("malformed PBKDF2 challenge: first salt: %w", err)
	}
	iter2, err := strconv.Atoi(parts[3])
	if err != nil {
		return "", fmt.Errorf("malformed PBKDF2 challenge: second iteration count %q: %w", parts[3], err)
	}
	salt2, err := hex.DecodeString(parts[4])
	if err != nil {
		return "", fmt.Errorf("malformed PBKDF2 challenge: second salt: %w", err)
	}
	if iter1 <= 0 || iter2 <= 0 {
		return "", fmt.Errorf("malformed PBKDF2 challenge: non-positive iteration count")
	}

	hash1, err := pbkdf2.Key(sha256.New, password, salt1, iter1, sha256.Size)
	if err != nil {
		return "", fmt.Errorf("deriving first PBKDF2 round: %w", err)
	}
	// The second round keys on the raw bytes of the first, which is why the
	// intermediate is passed through as a string rather than hex-encoded.
	hash2, err := pbkdf2.Key(sha256.New, string(hash1), salt2, iter2, sha256.Size)
	if err != nil {
		return "", fmt.Errorf("deriving second PBKDF2 round: %w", err)
	}
	return parts[4] + "$" + hex.EncodeToString(hash2), nil
}

// md5UTF16LE hashes s encoded as UTF-16LE, the encoding legacy FRITZ!OS uses.
func md5UTF16LE(s string) string {
	// FRITZ!OS replaces any code point above U+FFFF with U+002E before hashing,
	// because its own implementation is UCS-2 rather than UTF-16.
	cleaned := make([]rune, 0, len(s))
	for _, r := range s {
		if r > 0xFFFF {
			r = '.'
		}
		cleaned = append(cleaned, r)
	}
	units := utf16.Encode(cleaned)
	buf := make([]byte, len(units)*2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(buf[i*2:], u)
	}
	// #nosec G401 -- FRITZ!OS pins its login challenge to MD5; the hash is the
	// wire format of the protocol, not a security choice this CLI can make.
	sum := md5.Sum(buf)
	return hex.EncodeToString(sum[:])
}

func fetchSession(ctx context.Context, hc *http.Client, endpoint string, limiter *cliutil.AdaptiveLimiter) (*sessionInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	limiter.Wait()
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reaching the router: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// A saturated box answers the login endpoint before it answers anything
	// else, so the throttle check comes before the generic status check.
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		limiter.OnRateLimit()
		return nil, newThrottleError(resp, endpoint)
	}
	limiter.OnSuccess()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("router returned HTTP %d from the login endpoint", resp.StatusCode)
	}
	// The login endpoint answers with a small fixed document; the cap stops a
	// misrouted request to some other web server from being read without bound.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var info sessionInfo
	if err := xml.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("parsing the router login response: %w", err)
	}
	return &info, nil
}
