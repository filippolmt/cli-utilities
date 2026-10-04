package subito

import (
	"encoding/json"
	"math"
	"slices"
	"testing"
)

func adJSON(listID, subject, body, price, user string, company bool, category string) json.RawMessage {
	m := map[string]any{
		"urn":        "id:ad:00000000-0000-0000-0000-000000000000:list:" + listID,
		"subject":    subject,
		"body":       body,
		"type":       map[string]any{"key": "s"},
		"category":   map[string]any{"key": category, "value": "Cat" + category},
		"dates":      map[string]any{"display": "2026-10-04 21:04:01"},
		"images":     []any{map[string]any{"uri": "imgid:x"}},
		"advertiser": map[string]any{"user_id": user, "company": company},
		"geo": map[string]any{
			"region": map[string]any{"value": "Lombardia"},
			"city":   map[string]any{"value": "Milano"},
			"town":   map[string]any{"value": "Town" + listID},
		},
		"urls": map[string]any{"default": "https://www.subito.it/x/slug-" + listID + ".htm"},
		"features": []any{
			map[string]any{"uri": "/price", "values": []any{map[string]any{"key": price, "value": price + " €"}}},
			map[string]any{"uri": "/item_shippable", "values": []any{map[string]any{"key": "1"}}},
		},
	}
	b, _ := json.Marshal(m)
	return b
}

func mustAd(t *testing.T, raw json.RawMessage) Ad {
	t.Helper()
	a, err := ParseAd(raw)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestParseAd(t *testing.T) {
	a := mustAd(t, adJSON("663298311", " Bici corsa ", "testo", "1.500", "42", false, "41"))
	if a.ListID != "663298311" || a.Subject != "Bici corsa" || a.Price != 1500 || !a.HasPrice || !a.Shippable || a.Town != "Town663298311" || a.ImageCount != 1 {
		t.Fatalf("unexpected ad: %+v", a)
	}
	if _, err := ParseAd(json.RawMessage(`{"subject":"x"}`)); err == nil {
		t.Fatal("ad without urn must fail")
	}
}

func TestParseSearchSkipsBadAds(t *testing.T) {
	raw := json.RawMessage(`{"count_all": 7, "ads": [` + string(adJSON("1000001", "a", "", "10", "1", false, "41")) + `, {"subject":"no urn"}]}`)
	p, err := ParseSearch(raw)
	if err != nil || p.Total != 7 || len(p.Ads) != 1 {
		t.Fatalf("got %+v %v", p, err)
	}
}

func TestParseNumber(t *testing.T) {
	cases := map[string]float64{"180": 180, "0,99": 0.99, "1.500": 1500, "12.000,50": 12000.5, "3.5": 3.5, " 250 € ": 250}
	for in, want := range cases {
		got, ok := ParseNumber(in)
		if !ok || math.Abs(got-want) > 1e-9 {
			t.Errorf("ParseNumber(%q) = %v,%v want %v", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "abc", "-5"} {
		if _, ok := ParseNumber(bad); ok {
			t.Errorf("ParseNumber(%q) should fail", bad)
		}
	}
}

func TestParseRef(t *testing.T) {
	cases := map[string]string{
		"https://www.subito.it/biciclette/bici-da-corsa-milano-663258568.htm?x=1": "663258568",
		"www.subito.it/auto/fiat-panda-roma-612345678.htm":                        "612345678",
		"https://WWW.Subito.it/auto/fiat-panda-roma-612345678.htm#top":            "612345678",
		"id:ad:f2233452-c527-4139-b3c2-43b343147fa0:list:663258568":               "663258568",
		"663258568": "663258568",
	}
	for in, want := range cases {
		r, err := ParseRef(in)
		if err != nil || r.ListID != want {
			t.Errorf("ParseRef(%q) = %+v, %v", in, r, err)
		}
	}
	if r, _ := ParseRef("https://www.subito.it/biciclette/bici-milano-663258568.htm?x=1"); r.URL != "https://www.subito.it/biciclette/bici-milano-663258568.htm" {
		t.Errorf("query not stripped: %q", r.URL)
	}
	for _, bad := range []string{"", "hello", "https://example.com/x-1.htm", "12",
		"http://169.254.169.254/latest?x=subito.it/a-123456.htm", "https://notsubito.it/x-123456.htm",
		"https://subito.it.evil.com/x-123456.htm", "ftp://www.subito.it/x-123456.htm", "https://www.subito.it/no-id"} {
		if _, err := ParseRef(bad); err == nil {
			t.Errorf("ParseRef(%q) should fail", bad)
		}
	}
}

func TestMatchesAll(t *testing.T) {
	q := Tokens("Yamaha Tracer 9 GT")
	yes := []string{"YAMAHA TRACER 9 GT 2021", "Yamaha tracer 9 gt+ full optional"}
	no := []string{"Yamaha Tracer 7", "Yamaha MT-09", "Tracer 9 GT ricambi originali"}
	for _, s := range yes {
		if !MatchesAll(s, q) {
			t.Errorf("%q should match", s)
		}
	}
	for _, s := range no {
		if MatchesAll(s, q) {
			t.Errorf("%q should not match", s)
		}
	}
	if !MatchesAll("Yamaha Tracer9 GT", Tokens("tracer9 gt")) {
		t.Error("compact token should match")
	}
	if MatchesAll("Bici corsa", Tokens("bici da corsa carbonio")) {
		t.Error("missing token must not match")
	}
	if !MatchesAll("Bici corsa Bianchi", Tokens("bici da corsa")) {
		t.Error("stopword must be ignored")
	}
}

func TestDedupe(t *testing.T) {
	ads := []Ad{
		mustAd(t, adJSON("1000001", "iPhone 15 128GB nero", "", "600", "7", true, "12")),
		mustAd(t, adJSON("1000002", "IPHONE 15 128gb  NERO!", "", "600", "7", true, "12")),
		mustAd(t, adJSON("1000003", "iPhone 15 128GB nero", "", "600", "8", false, "12")),
		mustAd(t, adJSON("1000001", "iPhone 15 128GB nero", "", "600", "7", true, "12")),
	}
	g := Dedupe(ads)
	if len(g) != 2 {
		t.Fatalf("want 2 groups, got %d: %+v", len(g), g)
	}
	if len(g[0].Duplicates) != 1 || g[0].Duplicates[0] != "1000002" || len(g[0].Towns) != 2 {
		t.Fatalf("bad merge: %+v", g[0])
	}
}

func TestPercentileAndVerdict(t *testing.T) {
	v := []float64{400, 100, 300, 200}
	if Median(v) != 250 || Percentile(v, 0) != 100 || Percentile(v, 100) != 400 {
		t.Fatalf("percentiles wrong: %v %v %v", Median(v), Percentile(v, 0), Percentile(v, 100))
	}
	if !math.IsNaN(Median(nil)) {
		t.Fatal("empty median must be NaN")
	}
	if GapPct(75, 100) != -25 || Verdict(-25) != "below" || Verdict(5) != "in_line" || Verdict(12) != "above" {
		t.Fatal("gap/verdict wrong")
	}
	if RoundPrice(123) != 125 || RoundPrice(456) != 460 || RoundPrice(1234) != 1250 {
		t.Fatal("rounding wrong")
	}
}

func TestMetricSkipsPlaceholderPrices(t *testing.T) {
	a := mustAd(t, adJSON("1000001", "x", "", "1", "7", false, "12"))
	if _, ok := Metric(a, false); ok {
		t.Fatal("1 euro placeholder must not count as a price")
	}
	b := mustAd(t, adJSON("1000002", "x", "", "200000", "7", false, "7"))
	b.SizeM2 = 80
	if m, ok := Metric(b, true); !ok || m != 2500 {
		t.Fatalf("per m2 = %v %v", m, ok)
	}
}

func TestRisk(t *testing.T) {
	scam := mustAd(t, adJSON("1000001", "Fiat Panda 2020", "Contattami su WhatsApp, richiesta caparra. Solo spedizione.", "2000", "9", false, "2"))
	sig := Risk(RiskInput{Ad: scam, MarketMedian: 8000, Comparables: 10, SellerSimilar: 3})
	verdict, n := RiskVerdict(sig)
	if verdict != "exclude" || n != 5 {
		t.Fatalf("want exclude with 5 signals, got %s %d: %+v", verdict, n, sig)
	}
	clean := mustAd(t, adJSON("1000002", "Fiat Panda 2020", "Visibile a Milano, tagliandi in regola.", "7900", "10", false, "2"))
	if v, n := RiskVerdict(Risk(RiskInput{Ad: clean, MarketMedian: 8000, Comparables: 10, SellerSimilar: 0})); v != "ok" || n != 0 {
		t.Fatalf("clean ad flagged: %s %d", v, n)
	}
	reason := mustAd(t, adJSON("1000003", "Fiat Panda 2020", "Vendo urgente per trasferimento.", "4000", "11", false, "2"))
	if v, _ := RiskVerdict(Risk(RiskInput{Ad: reason, MarketMedian: 8000, Comparables: 10, SellerSimilar: -1})); v != "ok" {
		t.Fatalf("stated reason should not fire below-market: %s", v)
	}
	few := Risk(RiskInput{Ad: scam, MarketMedian: 8000, Comparables: 1, SellerSimilar: -1})
	if few[0].Checked || few[1].Checked {
		t.Fatal("thin evidence must leave price and seller unchecked")
	}
	shippable := mustAd(t, adJSON("1000004", "iPhone", "Solo spedizione", "300", "12", false, "12"))
	for _, s := range Risk(RiskInput{Ad: shippable, SellerSimilar: -1}) {
		if s.Name == "shipping_only_for_pickup_item" && s.Fired {
			t.Fatal("shipping-only is fine for phones")
		}
	}
}

func TestParseAdPage(t *testing.T) {
	page := []byte(`<html><script type="application/ld+json">{"@type":"BreadcrumbList"}</script>` +
		`<script type="application/ld+json">{"@context":"https://schema.org","@type":"Product","name":"Bici d&apos;epoca","description":"Telaio acciaio","image":["https://images.sbito.it/a","https://images.sbito.it/b"],"offers":{"@type":"Offer","url":"https://www.subito.it/biciclette/bici-milano-663258568.htm","priceCurrency":"EUR","price":180,"itemCondition":"https://schema.org/UsedCondition"}}</script></html>`)
	d, err := ParseAdPage(page)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "Bici d'epoca" || d.Price != 180 || d.Currency != "EUR" || len(d.Images) != 2 || d.Condition != "UsedCondition" || d.URL == "" {
		t.Fatalf("unexpected detail: %+v", d)
	}
	if _, err := ParseAdPage([]byte("<html>Access Denied</html>")); err == nil {
		t.Fatal("challenge page must fail")
	}
}

func TestVariantsToDrop(t *testing.T) {
	drop := VariantsToDrop(Tokens("iphone 15 pro"))
	if slices.Contains(drop, "pro") || !slices.Contains(drop, "max") {
		t.Fatalf("got %v", drop)
	}
	if !ContainsAny("iPhone 15 Pro Max 256", drop) || ContainsAny("iPhone 15 Pro 128", drop) {
		t.Fatal("variant filtering wrong")
	}
}

func TestDamagedAndPlus(t *testing.T) {
	for _, s := range []string{"iPhone 15 128 GB - Da riparare", "Tracer 9 GT+ incidentata", "Schermo rotto", "solo ricambi", "iPhone bloccato iCloud"} {
		if !Damaged(s) {
			t.Errorf("%q should be damaged", s)
		}
	}
	for _, s := range []string{"iPhone 15 128 nero perfetto", "Bici con ricambio catena nuova", "Rotterdam"} {
		if Damaged(s) {
			t.Errorf("%q should not be damaged", s)
		}
	}
	if !PlusVariant("Yamaha Tracer 9 GT+ 2023") || PlusVariant("Tracer 9 GT 2021") || PlusVariant("iPhone 15 + cover") {
		t.Fatal("plus variant detection wrong")
	}
}

func TestRiskRegexEdges(t *testing.T) {
	fire := func(cat, body, name string) bool {
		a := Ad{ListID: "1", Subject: "x", Body: body, CategoryID: cat, Price: 100, HasPrice: true}
		for _, s := range Risk(RiskInput{Ad: a, MarketMedian: 400, Comparables: 10, SellerSimilar: -1}) {
			if s.Name == name {
				return s.Fired
			}
		}
		t.Fatalf("signal %s missing", name)
		return false
	}
	cases := []struct {
		cat, body, signal string
		want              bool
	}{
		{"12", "IMEI 356938035643809, seriale 3501234567890", "contact_off_platform", false},
		{"12", "chiamami al 347 123 4567", "contact_off_platform", true},
		{"12", "scrivetemi su subito", "contact_off_platform", false},
		{"12", "scrivimi su whatsapp", "contact_off_platform", true},
		{"12", "no whatsapp, solo chat", "contact_off_platform", false},
		{"12", "no anticipi, solo a mano", "deposit_or_untraceable_payment", false},
		{"12", "richiedo anticipo del 50%", "deposit_or_untraceable_payment", true},
		{"12", "serve una caparra", "deposit_or_untraceable_payment", true},
		{"7", "caparra 2 mensilita", "deposit_or_untraceable_payment", false},
		{"12", "perfetto, nessun difetto, senza graffi", "price_far_below_market", true},
		{"12", "ha qualche graffio e un difetto", "price_far_below_market", false},
		{"12", "non trattabile, bonifico anticipato", "deposit_or_untraceable_payment", true},
		{"12", "no perditempo, whatsapp", "contact_off_platform", true},
		{"12", "iPhone 13 no box\nscrivi su whatsapp", "contact_off_platform", true},
		{"12", "zero graffi, trasloco", "price_far_below_market", false},
		{"12", "senza piccoli graffi", "price_far_below_market", true},
	}
	for _, c := range cases {
		if got := fire(c.cat, c.body, c.signal); got != c.want {
			t.Errorf("%s on %q = %v, want %v", c.signal, c.body, got, c.want)
		}
	}
}

func TestAccessory(t *testing.T) {
	q := Tokens("canon r6")
	for _, s := range []string{"Staffa L per canon R6 II/R5", "SMALLRIG Gabbia per canon R6 II", "Cover per Canon R6", "Batteria compatibile Canon R6",
		"Canon R6 Smallrig Cage", "Canon BG-R10 - Battery Grip per Canon EOS R5 / R6", "Canon battery grip r5/r6"} {
		if !Accessory(s, q) {
			t.Errorf("%q should be an accessory", s)
		}
	}
	for _, s := range []string{"Canon R6 corpo", "Canon EOS R6 con cover e borsa", "Fotocamera Canon R6 mark II", "Canon R6 + gabbia smallrig", "Canon R6 perfetta"} {
		if Accessory(s, q) {
			t.Errorf("%q should not be an accessory", s)
		}
	}
}
