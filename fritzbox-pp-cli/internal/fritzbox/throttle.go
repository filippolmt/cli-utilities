// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package fritzbox

import (
	"fmt"
	"net/http"
	"time"

	"fritzbox-pp-cli/internal/cliutil"
)

// ThrottleError reports that the router refused a request because it is
// saturated, not because the request was wrong.
//
// A FRITZ!Box is one small embedded device with a serialised SOAP stack. A
// handful of concurrent TR-064 actions is enough to make FRITZ!OS answer 503,
// and 7.5x and newer answer the session-authenticated surfaces with 429 under
// the same pressure. Both mean "same request, slower". Reporting them as the
// generic "router returned HTTP n" tells a caller the request was malformed
// and invites an immediate retry, which is exactly what a saturated box
// cannot absorb. Typing them lets the CLI exit with its rate-limit code and
// lets callers wait RetryAfter before trying again.
type ThrottleError struct {
	Status     int
	URL        string
	RetryAfter time.Duration
}

func (e *ThrottleError) Error() string {
	msg := fmt.Sprintf("router is throttling requests: HTTP %d", e.Status)
	if e.URL != "" {
		msg += " for " + e.URL
	}
	if e.RetryAfter > 0 {
		msg += fmt.Sprintf("; retry after %s", e.RetryAfter)
	}
	return msg
}

// newThrottleError builds a ThrottleError from the throttling response.
//
// The Retry-After parse is shared with the generated client so a FRITZ!Box
// that sends the header in seconds, as an HTTP date, or not at all is handled
// the same way everywhere in this CLI.
func newThrottleError(resp *http.Response, url string) error {
	return &ThrottleError{
		Status:     resp.StatusCode,
		URL:        url,
		RetryAfter: cliutil.RetryAfter(resp),
	}
}

// DefaultRateLimit is the pace the router transports start at, in requests per
// second.
//
// A FRITZ!Box is not a web API with a queue in front of it: it is one embedded
// CPU running a serialised SOAP stack, and `sync` fans its resource reads out
// across goroutines. Eight requests a second is far more than any single
// command needs when it runs its calls in order, so a sequential command pays
// nothing measurable, while a fan-out burst is stopped from being the thing
// that pushes the box into 503s. The limiter ramps up from here after
// consecutive successes and halves whenever the router does throttle.
const DefaultRateLimit = 8.0

// NewLimiter builds the shared limiter for the router transports from the
// CLI's --rate-limit value.
//
// A positive value is the user's explicit ceiling and is honoured as one.
// Anything else adapts from DefaultRateLimit. This deliberately differs from
// the generated client's mapping, where zero disables pacing altogether: that
// is a safe default for an API gateway that can absorb a burst and shed it as
// 429s, and an unsafe one for a router that answers a burst by falling over.
// There is nothing here for "unpaced" to be safe against, so it is not offered.
func NewLimiter(ratePerSec float64) *cliutil.AdaptiveLimiter {
	if ratePerSec > 0 {
		return cliutil.NewAdaptiveLimiter(ratePerSec)
	}
	return cliutil.NewAdaptiveLimiterAuto(DefaultRateLimit)
}
