package subito

import (
	"fmt"
	"regexp"
	"strings"
)

// Signal is one scam heuristic evaluated on an ad, with the evidence that
// triggered it. Fired=false signals are kept so callers see what was checked.
type Signal struct {
	Name     string `json:"name"`
	Fired    bool   `json:"fired"`
	Evidence string `json:"evidence,omitempty"`
	Checked  bool   `json:"checked"`
	Note     string `json:"note,omitempty"`
}

// RiskInput carries what the risk check knows beyond the ad itself.
type RiskInput struct {
	Ad Ad
	// MarketMedian is the like-for-like median price; 0 when unknown.
	MarketMedian float64
	// Comparables is how many ads the median was computed over.
	Comparables int
	// SellerSimilar counts other ads by the same advertiser, in the same
	// category, priced at least half this ad's price. -1 when unknown.
	SellerSimilar int
}

// BelowMarketPct is the gap below the median that counts as suspicious when
// the text gives no reason (subito-research threshold).
const BelowMarketPct = -25.0

// MinComparables is the smallest sample a median is trusted on.
const MinComparables = 3

var (
	// Phone numbers need non-digit boundaries so IMEIs and serials do not match.
	offPlatformRe = regexp.MustCompile(`(?i)(whats\s?app|wa\.me|telegram|[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}|(?:^|[^\d])(?:\+39[\s.]?)?3\d{2}[\s.-]?\d{3}[\s.-]?\d{3,4}(?:[^\d]|$)|scriv\w* (?:in privato|su (?:whats\s?app|telegram)|alla mia (?:e-?)?mail)|contatt\w* (?:via|per|su) (?:e-?)?mail)`)
	depositRe     = regexp.MustCompile(`(?i)(anticip\w+|bonifico anticipato|ricarica (?:postepay|paypal)|western union|moneygram|amici e parenti|friends (?:and|&) family)`)
	// A deposit is normal for property; elsewhere it is a scam marker.
	caparraRe  = regexp.MustCompile(`(?i)(caparra|acconto)`)
	shipOnlyRe = regexp.MustCompile(`(?i)(solo spedizion[ei]|no ritiro|niente ritiro|non (?:effettuo|faccio) (?:il )?ritiro|ritiro non possibile|spedisco soltanto|invio solo)`)
	// A reason in the text makes a low price less suspicious.
	reasonRe = regexp.MustCompile(`(?i)(urgente|trasferimento|trasloco|difett|rott[aoie]\b|graffi|non funzion|da riparare|per ricambi|svendo|ammaccat)`)
	// negationTailRe matches text that ends in a negation, optionally plus one
	// softening word: "nessun difetto", "senza piccoli graffi", "no anticipi".
	// Spaces only: a comma or newline ends the negated clause, so
	// "non trattabile, bonifico anticipato" still fires.
	negationTailRe = regexp.MustCompile(`(?i)\b(?:nessun[oa]?|senza|zero|privo di|priva di|no|non)[ \t]+(?:(?:piccol[oiea]|alcun[oa]?|evident[ei]|particolar[ei]|grossi|gravi)[ \t]+)?$`)
)

// firstAffirmative returns the first match of re in text that is not
// negated by the words just before it, trimmed of boundary characters.
func firstAffirmative(re *regexp.Regexp, text string) string {
	for _, loc := range re.FindAllStringIndex(text, -1) {
		from := loc[0] - 30
		if from < 0 {
			from = 0
		}
		if negationTailRe.MatchString(text[from:loc[0]]) {
			continue
		}
		return strings.Trim(text[loc[0]:loc[1]], " \t\n.,;:()-")
	}
	return ""
}

// Categories where buyers normally inspect and collect in person: vehicles,
// property and bulky furniture/appliances.
var pickupCategories = map[string]bool{
	"2": true, "3": true, "4": true, "22": true, "34": true, // vehicles, boats, campers
	"7": true, "8": true, "29": true, "30": true, "31": true, "32": true, "33": true, "43": true, // property
	"14": true, "37": true, // furniture, appliances
}

var propertyCategories = map[string]bool{"7": true, "8": true, "29": true, "30": true, "31": true, "32": true, "33": true, "43": true}

// Risk evaluates the mechanical scam signals of the subito-research skill.
// The "catalog photos" signal needs vision and is reported as unchecked.
func Risk(in RiskInput) []Signal {
	a := in.Ad
	text := a.Subject + "\n" + a.Body
	signals := make([]Signal, 0, 6)

	below := Signal{Name: "price_far_below_market"}
	switch {
	case !Priced(a):
		below.Note = "ad has no real asking price"
	case in.MarketMedian <= 0 || in.Comparables < MinComparables:
		below.Note = fmt.Sprintf("only %d comparable ads; median not trusted", in.Comparables)
	default:
		below.Checked = true
		gap := GapPct(a.Price, in.MarketMedian)
		if gap <= BelowMarketPct {
			if m := firstAffirmative(reasonRe, text); m != "" {
				below.Note = fmt.Sprintf("%.0f%% below the median of %.0f, but the text gives a reason (%q)", -gap, in.MarketMedian, m)
			} else {
				below.Fired = true
				below.Evidence = fmt.Sprintf("%.0f € is %.0f%% below the median of %.0f € over %d comparable ads, with no reason in the text", a.Price, -gap, in.MarketMedian, in.Comparables)
			}
		}
	}
	signals = append(signals, below)

	seller := Signal{Name: "seller_many_similar_ads"}
	switch {
	case in.SellerSimilar < 0:
		seller.Note = "seller's other ads not known locally"
	case a.Company:
		seller.Checked = true
		seller.Note = "verified shop: several similar ads are expected"
	default:
		seller.Checked = true
		if in.SellerSimilar >= 2 {
			seller.Fired = true
			seller.Evidence = fmt.Sprintf("private seller %s has %d other ads of similar value in %s seen locally", a.UserID, in.SellerSimilar, a.Category)
		}
	}
	signals = append(signals, seller)

	signals = append(signals, regexSignal("contact_off_platform", text, offPlatformRe))
	payment := []*regexp.Regexp{depositRe}
	if !propertyCategories[a.CategoryID] {
		payment = append(payment, caparraRe)
	}
	signals = append(signals, regexSignal("deposit_or_untraceable_payment", text, payment...))

	ship := Signal{Name: "shipping_only_for_pickup_item", Checked: true}
	if !pickupCategories[a.CategoryID] {
		ship.Note = "category is normally shipped"
	} else if m := shipOnlyRe.FindString(text); m != "" {
		ship.Fired = true
		ship.Evidence = fmt.Sprintf("%s is usually collected in person, but the text says %q", a.Category, m)
	}
	signals = append(signals, ship)

	signals = append(signals, Signal{Name: "catalog_photos", Note: "needs a look at the photos; not checked automatically"})
	return signals
}

func regexSignal(name, text string, res ...*regexp.Regexp) Signal {
	s := Signal{Name: name, Checked: true}
	for _, re := range res {
		if m := firstAffirmative(re, text); m != "" {
			s.Fired = true
			s.Evidence = fmt.Sprintf("text contains %q", m)
			return s
		}
	}
	return s
}

// RiskVerdict follows the skill rule: two or more fired signals exclude the
// ad, one calls for caution.
func RiskVerdict(signals []Signal) (string, int) {
	n := 0
	for _, s := range signals {
		if s.Fired {
			n++
		}
	}
	switch {
	case n >= 2:
		return "exclude", n
	case n == 1:
		return "caution", n
	}
	return "ok", n
}
