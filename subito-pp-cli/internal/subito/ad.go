// Package subito holds the hand-written domain logic for subito-pp-cli:
// parsing hades listings, comparing them against the market and spotting
// scam signals. It has no network or store code, so it is tested in isolation.
package subito

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Ad is the flattened view of one hades listing that the market and risk
// logic works on. Raw keeps the original JSON for output and storage.
type Ad struct {
	URN        string          `json:"urn"`
	ListID     string          `json:"list_id"`
	Subject    string          `json:"subject"`
	Body       string          `json:"-"`
	Price      float64         `json:"price"`
	HasPrice   bool            `json:"-"`
	SizeM2     float64         `json:"size_m2,omitempty"`
	CategoryID string          `json:"category_id"`
	Category   string          `json:"category"`
	AdType     string          `json:"ad_type"`
	Region     string          `json:"region"`
	Province   string          `json:"province"`
	Town       string          `json:"town"`
	UserID     string          `json:"advertiser_id"`
	Company    bool            `json:"company"`
	ShopName   string          `json:"shop_name,omitempty"`
	Shippable  bool            `json:"shippable"`
	Condition  string          `json:"condition,omitempty"`
	Published  string          `json:"published"`
	URL        string          `json:"url"`
	ImageCount int             `json:"image_count"`
	Raw        json.RawMessage `json:"-"`
}

type rawValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type rawAd struct {
	URN     string `json:"urn"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	Type    struct {
		Key string `json:"key"`
	} `json:"type"`
	Category struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	} `json:"category"`
	Dates struct {
		Display string `json:"display"`
	} `json:"dates"`
	Images   []json.RawMessage `json:"images"`
	Features []struct {
		URI    string     `json:"uri"`
		Values []rawValue `json:"values"`
	} `json:"features"`
	Advertiser struct {
		UserID   string `json:"user_id"`
		Company  bool   `json:"company"`
		ShopName string `json:"shop_name"`
	} `json:"advertiser"`
	Geo struct {
		Region struct {
			Value string `json:"value"`
		} `json:"region"`
		City struct {
			Value string `json:"value"`
		} `json:"city"`
		Town struct {
			Value string `json:"value"`
		} `json:"town"`
	} `json:"geo"`
	URLs struct {
		Default string `json:"default"`
	} `json:"urls"`
}

var (
	listIDFromURN = regexp.MustCompile(`:list:(\d+)$`)
	thousandsRe   = regexp.MustCompile(`^\d{1,3}(\.\d{3})+(,\d+)?$`)
)

// ParseAd flattens one listing object from /v1/search/items.
func ParseAd(raw json.RawMessage) (Ad, error) {
	var r rawAd
	if err := json.Unmarshal(raw, &r); err != nil {
		return Ad{}, fmt.Errorf("parsing listing: %w", err)
	}
	if r.URN == "" {
		return Ad{}, fmt.Errorf("listing has no urn")
	}
	ad := Ad{
		URN:        r.URN,
		Subject:    strings.TrimSpace(r.Subject),
		Body:       r.Body,
		CategoryID: r.Category.Key,
		Category:   r.Category.Value,
		AdType:     r.Type.Key,
		Region:     r.Geo.Region.Value,
		Province:   r.Geo.City.Value,
		Town:       r.Geo.Town.Value,
		UserID:     r.Advertiser.UserID,
		Company:    r.Advertiser.Company,
		ShopName:   r.Advertiser.ShopName,
		Published:  r.Dates.Display,
		URL:        r.URLs.Default,
		ImageCount: len(r.Images),
		Raw:        raw,
	}
	if m := listIDFromURN.FindStringSubmatch(r.URN); m != nil {
		ad.ListID = m[1]
	}
	for _, f := range r.Features {
		if len(f.Values) == 0 {
			continue
		}
		v := f.Values[0]
		switch f.URI {
		case "/price":
			if p, ok := ParseNumber(v.Key); ok {
				ad.Price, ad.HasPrice = p, true
			}
		case "/size":
			if s, ok := ParseNumber(v.Key); ok {
				ad.SizeM2 = s
			}
		case "/item_shippable":
			ad.Shippable = v.Key == "1"
		case "/item_condition":
			ad.Condition = v.Value
		}
	}
	return ad, nil
}

// SearchPage is one decoded /v1/search/items response.
type SearchPage struct {
	Total int
	// Returned counts listings in the response, including any that failed
	// to parse, so pagination advances by what the server sent.
	Returned int
	Ads      []Ad
}

// ParseSearch decodes a search response. Listings that fail to parse are
// skipped rather than failing the page: one malformed ad must not hide the rest.
func ParseSearch(raw json.RawMessage) (SearchPage, error) {
	var env struct {
		CountAll int               `json:"count_all"`
		Ads      []json.RawMessage `json:"ads"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return SearchPage{}, fmt.Errorf("parsing search response: %w", err)
	}
	page := SearchPage{Total: env.CountAll, Returned: len(env.Ads), Ads: make([]Ad, 0, len(env.Ads))}
	for _, a := range env.Ads {
		if ad, err := ParseAd(a); err == nil {
			page.Ads = append(page.Ads, ad)
		}
	}
	return page, nil
}

// ParseNumber reads hades numeric keys such as "180", "0,99" or "1.500".
// A dot followed by exactly three digits is a thousands separator.
func ParseNumber(s string) (float64, bool) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "€"))
	if s == "" {
		return 0, false
	}
	if thousandsRe.MatchString(s) {
		s = strings.ReplaceAll(s, ".", "")
	}
	s = strings.ReplaceAll(s, ",", ".")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 {
		return 0, false
	}
	return f, true
}

var (
	adPathRe = regexp.MustCompile(`^/.*-(\d+)\.htm$`)
	digitsRe = regexp.MustCompile(`^\d{6,12}$`)
)

// Ref identifies one listing given by the user as a URL, urn or list id.
type Ref struct {
	ListID string
	URN    string
	URL    string
}

// SiteURL is the origin of Subito's ad pages.
const SiteURL = "https://www.subito.it"

// siteDomain is the registrable domain every Subito host sits under.
var siteDomain = strings.TrimPrefix(strings.TrimPrefix(SiteURL, "https://"), "www.")

// IsSubitoHost reports whether host is subito.it or one of its subdomains.
func IsSubitoHost(host string) bool {
	host = strings.ToLower(host)
	return host == siteDomain || strings.HasSuffix(host, "."+siteDomain)
}

// ParseRef accepts https://www.subito.it/<cat>/<slug>-<id>.htm, an
// id:ad:<uuid>:list:<id> urn, or a bare list id. URLs on any other host are
// rejected: the CLI fetches them, so they must not reach arbitrary servers.
func ParseRef(s string) (Ref, error) {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(s, "id:ad:"):
		m := listIDFromURN.FindStringSubmatch(s)
		if m == nil {
			return Ref{}, fmt.Errorf("urn %q has no list id", s)
		}
		return Ref{ListID: m[1], URN: s}, nil
	case digitsRe.MatchString(s):
		return Ref{ListID: s}, nil
	case strings.Contains(strings.ToLower(s), siteDomain):
		raw := s
		if !strings.Contains(raw, "://") {
			raw = "https://" + strings.TrimPrefix(raw, "//")
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !IsSubitoHost(u.Hostname()) {
			return Ref{}, fmt.Errorf("%q is not a subito.it listing URL", s)
		}
		m := adPathRe.FindStringSubmatch(u.Path)
		if m == nil {
			return Ref{}, fmt.Errorf("%q is not a subito.it listing URL (expected /<category>/<title>-<id>.htm)", s)
		}
		return Ref{ListID: m[1], URL: "https://" + strings.ToLower(u.Hostname()) + u.Path}, nil
	}
	return Ref{}, fmt.Errorf("%q is not a Subito listing URL, urn or list id", s)
}
