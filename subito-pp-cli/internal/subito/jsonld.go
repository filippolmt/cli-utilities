package subito

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"

	"subito-pp-cli/internal/cliutil"
)

// Detail is what an ad page publishes as schema.org JSON-LD.
type Detail struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Images      []string `json:"images"`
	Price       float64  `json:"price,omitempty"`
	Currency    string   `json:"currency,omitempty"`
	Condition   string   `json:"condition,omitempty"`
	URL         string   `json:"url,omitempty"`
}

var ldScriptRe = regexp.MustCompile(`(?s)<script[^>]+type="application/ld\+json"[^>]*>(.*?)</script>`)

// ParseAdPage extracts the schema.org Product block from an ad page.
func ParseAdPage(page []byte) (Detail, error) {
	for _, m := range ldScriptRe.FindAllSubmatch(page, -1) {
		var node map[string]any
		if json.Unmarshal(m[1], &node) != nil {
			continue
		}
		if t, _ := node["@type"].(string); t != "Product" {
			continue
		}
		d := Detail{
			Name:        clean(node["name"]),
			Description: clean(node["description"]),
			URL:         clean(node["url"]),
			Images:      stringList(node["image"]),
		}
		if c, ok := node["itemCondition"].(string); ok {
			d.Condition = path.Base(c)
		}
		if offers, ok := node["offers"].(map[string]any); ok {
			switch p := offers["price"].(type) {
			case float64:
				d.Price = p
			case string:
				d.Price, _ = ParseNumber(p)
			}
			d.Currency = clean(offers["priceCurrency"])
			if c, ok := offers["itemCondition"].(string); ok && d.Condition == "" {
				d.Condition = path.Base(c)
			}
			if d.URL == "" {
				d.URL = clean(offers["url"])
			}
		}
		return d, nil
	}
	return Detail{}, fmt.Errorf("no schema.org Product block on the page (removed listing or a challenge page)")
}

func clean(v any) string {
	s, _ := v.(string)
	return cliutil.CleanText(s)
}

func stringList(v any) []string {
	out := make([]string, 0)
	switch t := v.(type) {
	case string:
		out = append(out, t)
	case []any:
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}
