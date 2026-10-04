// Hand-written: fetching a Subito ad page (www.subito.it), which lives off
// the hades API host the generated client talks to.

package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"time"

	"subito-pp-cli/internal/subito"
)

// adPageURL is the page for a ref; Subito resolves /vi/<id>.htm to the ad.
func adPageURL(ref subito.Ref) string {
	if ref.URL != "" {
		return ref.URL
	}
	return subito.SiteURL + "/vi/" + ref.ListID + ".htm"
}

// fetchAdPage downloads one ad page and parses its JSON-LD block.
func fetchAdPage(ctx context.Context, flags *rootFlags, url string) (subito.Detail, error) {
	timeout := flags.timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if u, err := neturl.Parse(url); err != nil || !subito.IsSubitoHost(u.Hostname()) {
		return subito.Detail{}, usageErr(fmt.Errorf("%q is not a subito.it page", url))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return subito.Detail{}, err
	}
	req.Header.Set("User-Agent", "subito-pp-cli/"+version)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "it-IT,it;q=0.9")
	hc := &http.Client{Timeout: timeout, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		if !subito.IsSubitoHost(r.URL.Hostname()) {
			return fmt.Errorf("refusing redirect off subito.it to %s", r.URL.Hostname())
		}
		return nil
	}}
	resp, err := hc.Do(req)
	if err != nil {
		return subito.Detail{}, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound:
		return subito.Detail{}, notFoundErr(fmt.Errorf("%s: listing removed or expired (HTTP %d)", url, resp.StatusCode))
	case resp.StatusCode == http.StatusTooManyRequests:
		return subito.Detail{}, rateLimitErr(fmt.Errorf("%s: rate limited (HTTP 429); wait and retry", url))
	case resp.StatusCode == http.StatusForbidden:
		return subito.Detail{}, apiErr(fmt.Errorf("%s: blocked by Subito's edge (HTTP 403); wait a few minutes and retry", url))
	case resp.StatusCode != http.StatusOK:
		return subito.Detail{}, apiErr(fmt.Errorf("%s: HTTP %d", url, resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return subito.Detail{}, fmt.Errorf("reading %s: %w", url, err)
	}
	d, err := subito.ParseAdPage(body)
	if err != nil {
		return subito.Detail{}, apiErr(fmt.Errorf("%s: %w", url, err))
	}
	if d.URL == "" {
		d.URL = resp.Request.URL.String()
	}
	return d, nil
}
