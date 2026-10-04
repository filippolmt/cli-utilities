package subito

import (
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Words that Subito keyword search ignores anyway; requiring them in titles
// would drop good matches ("bici da corsa" vs "Bici corsa Bianchi").
var stopwords = map[string]bool{
	"da": true, "di": true, "del": true, "della": true, "e": true, "ed": true,
	"il": true, "la": true, "lo": true, "le": true, "i": true, "gli": true,
	"un": true, "una": true, "per": true, "con": true, "in": true, "a": true,
	"the": true, "and": true, "of": true, "for": true,
}

// words splits text into lowercase words, keeping stopwords.
func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// Tokens splits a query or title into lowercase words, dropping stopwords.
func Tokens(s string) []string {
	fields := words(s)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if !stopwords[f] {
			out = append(out, f)
		}
	}
	return out
}

// MatchesAll reports whether a title contains every query token. A token
// matches a whole word, or, from four characters up, any part of the title
// with spaces removed, so "tracer9" still matches "tracer 9".
func MatchesAll(title string, tokens []string) bool {
	words := Tokens(title)
	set := make(map[string]bool, len(words))
	for _, w := range words {
		set[w] = true
	}
	compact := strings.Join(words, "")
	for _, t := range tokens {
		if set[t] {
			continue
		}
		if len(t) >= 4 && strings.Contains(compact, t) {
			continue
		}
		return false
	}
	return true
}

// ContainsAny reports whether a title contains any of the tokens as a word.
func ContainsAny(title string, tokens []string) bool {
	for _, w := range Tokens(title) {
		if slices.Contains(tokens, w) {
			return true
		}
	}
	return false
}

// NormalizeSubject folds case, punctuation and spacing so the same item
// cross-posted in several cities compares equal.
func NormalizeSubject(s string) string { return strings.Join(Tokens(s), " ") }

// Group is one item after cross-post collapse: the newest ad plus the
// other list ids and towns it was posted under.
type Group struct {
	Ad
	Duplicates []string `json:"duplicate_list_ids,omitempty"`
	Towns      []string `json:"towns,omitempty"`
}

// Dedupe collapses ads that share advertiser, normalized title and price,
// keeping input order (newest first when fed datedesc results).
func Dedupe(ads []Ad) []Group {
	idx := map[string]int{}
	out := make([]Group, 0, len(ads))
	seenList := map[string]bool{}
	for _, a := range ads {
		if a.ListID != "" && seenList[a.ListID] {
			continue
		}
		seenList[a.ListID] = true
		key := a.UserID + "|" + NormalizeSubject(a.Subject) + "|" + strconv.FormatFloat(a.Price, 'f', 2, 64)
		if i, ok := idx[key]; ok && a.UserID != "" {
			out[i].Duplicates = append(out[i].Duplicates, a.ListID)
			if a.Town != "" && !slices.Contains(out[i].Towns, a.Town) {
				out[i].Towns = append(out[i].Towns, a.Town)
			}
			continue
		}
		idx[key] = len(out)
		g := Group{Ad: a}
		if a.Town != "" {
			g.Towns = []string{a.Town}
		}
		out = append(out, g)
	}
	return out
}

// Percentile returns the p-th percentile (0-100) with linear interpolation.
// The input does not need to be sorted. It returns NaN for an empty slice.
func Percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	s := append([]float64(nil), values...)
	sort.Float64s(s)
	if len(s) == 1 {
		return s[0]
	}
	rank := p / 100 * float64(len(s)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	return s[lo] + (s[hi]-s[lo])*(rank-float64(lo))
}

// Median is Percentile(values, 50).
func Median(values []float64) float64 { return Percentile(values, 50) }

// InLineBand is the +/- percent gap still judged "in line" with the market.
const InLineBand = 10.0

// Verdict classifies a gap from the market median, in percent.
func Verdict(gapPct float64) string {
	switch {
	case gapPct <= -InLineBand:
		return "below"
	case gapPct >= InLineBand:
		return "above"
	default:
		return "in_line"
	}
}

// GapPct is the percent difference of value from median.
func GapPct(value, median float64) float64 {
	if median == 0 {
		return 0
	}
	return math.Round((value-median)/median*1000) / 10
}

// Priced reports whether an ad carries a real asking price. Subito sellers
// often post 0 or 1 euro as a placeholder for "contact me"; those would drag
// every median down.
func Priced(a Ad) bool { return a.HasPrice && a.Price > 1 }

// Metric is the value compared across ads: price, or price per square metre
// when perM2 is set and the ad declares a size.
func Metric(a Ad, perM2 bool) (float64, bool) {
	if !Priced(a) {
		return 0, false
	}
	if perM2 {
		if a.SizeM2 <= 0 {
			return 0, false
		}
		return math.Round(a.Price/a.SizeM2*100) / 100, true
	}
	return a.Price, true
}

// Segment names the like-for-like comparison group: private sellers are
// compared with private sellers, shops with shops.
func Segment(a Ad) string {
	if a.Company {
		return "pro"
	}
	return "private"
}

// RoundPrice rounds a suggested price to what a person would type:
// 5 euro steps under 200, 10 under 1000, 50 above.
func RoundPrice(p float64) float64 {
	step := 50.0
	switch {
	case p < 200:
		step = 5
	case p < 1000:
		step = 10
	}
	return math.Round(p/step) * step
}

// variantWords are model qualifiers that change the price class of an item
// ("iPhone 15" vs "iPhone 15 Pro"). Keyword search returns them all.
var variantWords = []string{"pro", "plus", "max", "mini", "ultra", "lite", "fe"}

// VariantsToDrop returns the qualifiers absent from the query: titles that
// carry them are a different model than the one asked for.
func VariantsToDrop(queryTokens []string) []string {
	out := make([]string, 0, len(variantWords))
	for _, v := range variantWords {
		if !slices.Contains(queryTokens, v) {
			out = append(out, v)
		}
	}
	return out
}

// damagedRe spots titles of broken, crashed or parts-only items: they are a
// different market from working units and would fake deep discounts.
var damagedRe = regexp.MustCompile(`(?i)(da riparare|\brott[aoie]\b|incidentat|\bricambi\b|non funzionant|\bguast[aoie]\b|difettos|per pezzi|\bbloccat[aoie]\b|for parts|broken)`)

// Damaged reports whether a title describes a broken or parts-only item.
func Damaged(title string) bool { return damagedRe.MatchString(title) }

var plusVariantRe = regexp.MustCompile(`[\p{L}\p{N}]\+`)

// PlusVariant reports a "+" trim marker ("GT+", "S21+") in a title.
func PlusVariant(s string) bool { return plusVariantRe.MatchString(s) }

// accessoryWords name products sold for a model rather than the model.
var accessoryWords = map[string]bool{
	"cover": true, "custodia": true, "pellicola": true, "staffa": true, "gabbia": true,
	"cage": true, "smallrig": true, "adattatore": true, "caricatore": true, "supporto": true,
	"cinghia": true, "tracolla": true, "cavo": true, "grip": true,
}

// forWords introduce the model an accessory fits: "Cover per iPhone 15".
var forWords = map[string]bool{"per": true, "for": true, "compatibile": true, "compatible": true}

// bundleWords before an accessory word mean it comes with the item:
// "Canon R6 con gabbia", "iPhone 15 incluso cover".
var bundleWords = map[string]bool{"con": true, "incluso": true, "inclusa": true, "inclusi": true, "with": true, "plus": true}

// Accessory reports whether a title sells an accessory for the queried
// model rather than the model itself: a "per <model>" phrase, or an
// accessory word that is not introduced as part of a bundle.
func Accessory(title string, queryTokens []string) bool {
	if len(queryTokens) == 0 {
		return false
	}
	title = strings.Replace(title, " + ", " con ", 1)
	ws := words(title)
	bundled := false
	for i, w := range ws {
		switch {
		case bundleWords[w]:
			bundled = true
		case forWords[w] && !bundled:
			for _, later := range ws[i+1:] {
				if slices.Contains(queryTokens, later) {
					return true
				}
			}
		case accessoryWords[w] && !bundled:
			return true
		}
	}
	return false
}

// accessoryCategories hold parts and accessories for vehicles, never the
// vehicle itself (5 Accessori Auto, 36 Accessori Moto).
var accessoryCategories = map[string]bool{"5": true, "36": true}

// AccessoryCategory reports whether a category id only lists accessories.
func AccessoryCategory(id string) bool { return accessoryCategories[id] }
