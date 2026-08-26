// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package config

import (
	"os"
	"strings"
)

// FritzboxUsername returns the FRITZ!Box account name used for both HTTP
// Digest on TR-064 and the challenge-response session handshake.
//
// FRITZ!OS permits a password-only login when the box has exactly one user, so
// an empty result is a valid configuration and must not be reported as an error.
func (c *Config) FritzboxUsername() string {
	return strings.TrimSpace(os.Getenv("FRITZBOX_USERNAME"))
}

// FritzboxPasswordValue returns the FRITZ!Box password, preferring the
// environment over the stored config so a shell export always wins.
func (c *Config) FritzboxPasswordValue() string {
	if v := strings.TrimSpace(os.Getenv("FRITZBOX_PASSWORD")); v != "" {
		return v
	}
	return strings.TrimSpace(c.FritzboxPassword)
}

// FritzboxAddress returns the router origin with any trailing slash removed.
func (c *Config) FritzboxAddress() string {
	if v := strings.TrimSpace(os.Getenv("FRITZBOX_ADDRESS")); v != "" {
		return strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(c.BaseURL); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://fritz.box"
}

// FritzboxSession returns the cached session id, preferring the environment.
func (c *Config) FritzboxSession() string {
	if v := strings.TrimSpace(os.Getenv("FRITZBOX_SID")); v != "" {
		return v
	}
	return strings.TrimSpace(c.FritzboxSid)
}

// SetFritzboxSession persists a freshly minted session id so later invocations
// reuse it instead of repeating the handshake.
//
// The envOverrides delete mirrors SaveCredential: when FRITZBOX_SID is exported,
// the override would otherwise win at save time and the newly minted value would
// never reach disk.
func (c *Config) SetFritzboxSession(sid string) error {
	c.FritzboxSid = sid
	delete(c.envOverrides, "FritzboxSid")
	c.updateFileConfigField("FritzboxSid")
	// AuthHeader() prefers AuthHeaderVal, then the password, then the session
	// id. The generated endpoints send that value as the `sid` query parameter,
	// so without this the password would be sent where a session id belongs and
	// the router would answer with its login page.
	c.AuthHeaderVal = sid
	delete(c.envOverrides, "AuthHeaderVal")
	c.updateFileConfigField("AuthHeaderVal")
	return c.save()
}
