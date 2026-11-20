package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
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
