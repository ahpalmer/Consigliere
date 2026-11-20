package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ahpalmer/consigliere/internal/comp"
)

func TestOffers(t *testing.T) {
	f := newFixture(t, "")
	var activated struct{ Pursuit pursuitJSON }
	f.do("POST", "/api/pursuits", `{"itemId":1}`, &activated)
	base := "/api/pursuits/" + itoa(activated.Pursuit.ID) + "/offers"

	if code := f.do("POST", base, `{"text":"  "}`, nil); code != http.StatusBadRequest {
		t.Errorf("empty offer: %d", code)
	}
	if code := f.do("POST", base, `{"text":"no numbers here"}`, nil); code != http.StatusUnprocessableEntity {
		t.Errorf("not an offer: %d", code)
	}
	if code := f.do("POST", "/api/pursuits/999/offers", `{"text":"Base $200K"}`, nil); code != http.StatusNotFound {
		t.Errorf("unknown pursuit: %d", code)
	}

	// The sample pack's current package is compared with the extracted offer.
	var o offerJSON
	if code := f.do("POST", base, `{"text":"Base $200K, 15% bonus, 4% match."}`, &o); code != http.StatusCreated {
		t.Fatalf("add: %d", code)
	}
	if o.Company != "Acme" || o.Comparison == nil || o.Comparison.Start != "2027-01-04" || o.Error != "" {
		t.Fatalf("offer = %+v", o)
	}
	if got := o.Comparison.Incoming.Lines[0]; got.Label != "Base pay" || got.Amounts[0] != 200_000 {
		t.Errorf("first incoming line = %+v", got)
	}
	// The sample's 401(k) contribution ($23,000) is far over 4% of base, so the new match is capped at $8,000.
	for _, l := range o.Comparison.Incoming.Lines {
		if l.Label == "Retirement match" && l.Amounts[0] != 8_000 {
			t.Errorf("incoming match = %d", l.Amounts[0])
		}
	}

	// Correcting the YAML recomputes; YAML that doesn't check out is refused with the reason.
	var bad map[string]string
	if code := f.do("PUT", "/api/offers/"+itoa(o.ID), `{"package":"base: {amount: -1}\n"}`, &bad); code != http.StatusBadRequest ||
		!strings.Contains(bad["error"], "base.amount") {
		t.Errorf("bad YAML: %d %v", code, bad)
	}
	fixed := strings.Replace(o.Package, "amount: 200000", "amount: 210000", 1)
	raw, _ := json.Marshal(map[string]string{"package": fixed})
	body := string(raw)
	if code := f.do("PUT", "/api/offers/"+itoa(o.ID), body, &o); code != http.StatusOK || o.Comparison.Incoming.Lines[0].Amounts[0] != 210_000 {
		t.Errorf("update: %d %+v", code, o.Comparison.Incoming.Lines[0])
	}

	var list struct {
		Offers  []offerJSON
		Current bool
	}
	f.do("GET", base, "", &list)
	if len(list.Offers) != 1 || !list.Current {
		t.Errorf("list = %+v", list)
	}

	// Reading an offer is a model call, so pausing stops it.
	f.state.Pause()
	if code := f.do("POST", base, `{"text":"Base $200K"}`, nil); code != http.StatusConflict {
		t.Errorf("paused: %d", code)
	}
	f.state.Resume()

	if code := f.do("DELETE", "/api/offers/"+itoa(o.ID), "", nil); code != http.StatusOK {
		t.Errorf("delete: %d", code)
	}
	if code := f.do("DELETE", "/api/offers/"+itoa(o.ID), "", nil); code != http.StatusNotFound {
		t.Errorf("delete twice: %d", code)
	}
}

// Postings with listed pay are held against what the owner makes now (the sample pack's current package), and
// Options can filter to the ones that could beat it.
func TestPostingPay(t *testing.T) {
	f := newFixture(t, "")
	// Platform Engineer lists 300-350K, SRE 100-120K, Sales Engineer nothing.
	f.db.Exec(`UPDATE "RadarItems" SET "CompMin" = 300000, "CompMax" = 350000 WHERE "Title" = 'Platform Engineer'`)
	f.db.Exec(`UPDATE "RadarItems" SET "CompMin" = 100000, "CompMax" = 120000 WHERE "Title" = 'SRE'`)

	var page struct {
		Options []optionJSON
		PayNow  *comp.Yardstick
	}
	f.do("GET", "/api/options?minScore=0", "", &page)
	if page.PayNow == nil || page.PayNow.Base != 172_000 || page.PayNow.Total <= page.PayNow.Base {
		t.Fatalf("payNow = %+v", page.PayNow)
	}
	verdicts := map[string]string{}
	for _, o := range page.Options {
		if o.Pay != nil {
			verdicts[o.Title] = o.Pay.Verdict
		}
	}
	if verdicts["Platform Engineer"] != comp.PayAbove || verdicts["SRE"] != comp.PayBelow || len(verdicts) != 2 {
		t.Errorf("verdicts = %v", verdicts)
	}

	for filter, want := range map[string]int{"listed": 2, "reach": 1, "beats": 1} {
		f.do("GET", "/api/options?minScore=0&pay="+filter, "", &page)
		if len(page.Options) != want {
			t.Errorf("pay=%s: %d options, want %d", filter, len(page.Options), want)
		}
	}
	if code := f.do("GET", "/api/options?pay=lots", "", nil); code != http.StatusBadRequest {
		t.Errorf("unknown pay filter: %d", code)
	}

	var item struct{ Item optionJSON }
	f.do("GET", "/api/items/2", "", &item)
	if item.Item.Pay == nil || item.Item.Pay.VsBase != [2]int64{100_000 - 172_000, 120_000 - 172_000} {
		t.Errorf("item pay = %+v", item.Item.Pay)
	}
}
