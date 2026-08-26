// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package fritzbox

import (
	"crypto/md5" // #nosec G501 -- RFC 2617 Digest is MD5-only; see md5Hex.
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// digestChallenge is the parsed content of a WWW-Authenticate: Digest header.
//
// net/http has no Digest support, and TR-064 is Digest-only, so the scheme is
// implemented here rather than pulled in as a dependency. Only the subset
// FRITZ!OS emits is handled: MD5 with qop=auth.
type digestChallenge struct {
	realm     string
	nonce     string
	qop       string
	opaque    string
	algorithm string
}

// parseDigestChallenge extracts the Digest parameters from a WWW-Authenticate
// header value. A header advertising some other scheme yields a nil challenge
// and no error, so the caller can report an honest "unsupported auth" message.
func parseDigestChallenge(header string) *digestChallenge {
	const prefix = "digest "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return nil
	}
	c := &digestChallenge{}
	for _, part := range splitDigestParams(header[len(prefix):]) {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch key {
		case "realm":
			c.realm = value
		case "nonce":
			c.nonce = value
		case "qop":
			c.qop = value
		case "opaque":
			c.opaque = value
		case "algorithm":
			c.algorithm = value
		}
	}
	if c.nonce == "" {
		return nil
	}
	return c
}

// splitDigestParams splits on commas that sit outside quoted strings. A plain
// strings.Split would break on any parameter whose value legitimately contains
// a comma, which realm values sometimes do.
func splitDigestParams(s string) []string {
	var out []string
	var buf strings.Builder
	inQuotes := false
	for i := 0; i < len(s); i++ {
		switch ch := s[i]; {
		case ch == '"':
			inQuotes = !inQuotes
			buf.WriteByte(ch)
		case ch == ',' && !inQuotes:
			out = append(out, buf.String())
			buf.Reset()
		default:
			buf.WriteByte(ch)
		}
	}
	if buf.Len() > 0 {
		out = append(out, buf.String())
	}
	return out
}

// authorization builds the Authorization header value answering the challenge.
func (c *digestChallenge) authorization(username, password, method, uri string, nonceCount int) (string, error) {
	if c.algorithm != "" && !strings.EqualFold(c.algorithm, "MD5") {
		return "", fmt.Errorf("router requested unsupported digest algorithm %q", c.algorithm)
	}

	ha1 := md5Hex(username + ":" + c.realm + ":" + password)
	ha2 := md5Hex(method + ":" + uri)

	fields := []string{
		fmt.Sprintf(`username=%q`, username),
		fmt.Sprintf(`realm=%q`, c.realm),
		fmt.Sprintf(`nonce=%q`, c.nonce),
		fmt.Sprintf(`uri=%q`, uri),
	}

	var response string
	if c.qop == "" {
		response = md5Hex(ha1 + ":" + c.nonce + ":" + ha2)
	} else {
		// FRITZ!OS advertises qop="auth"; if it ever offers a list, pick auth
		// explicitly rather than echoing the whole list back.
		qop := "auth"
		cnonce, err := randomHex(8)
		if err != nil {
			return "", err
		}
		nc := fmt.Sprintf("%08x", nonceCount)
		response = md5Hex(strings.Join([]string{ha1, c.nonce, nc, cnonce, qop, ha2}, ":"))
		fields = append(fields,
			"qop="+qop,
			"nc="+nc,
			fmt.Sprintf(`cnonce=%q`, cnonce),
		)
	}

	fields = append(fields, fmt.Sprintf(`response=%q`, response))
	if c.opaque != "" {
		fields = append(fields, fmt.Sprintf(`opaque=%q`, c.opaque))
	}
	return "Digest " + strings.Join(fields, ", "), nil
}

func md5Hex(s string) string {
	// #nosec G401 -- RFC 2617 Digest fixes the hash at MD5 and TR-064 offers no
	// other scheme, so this is protocol conformance rather than a weak choice.
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating digest client nonce: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
