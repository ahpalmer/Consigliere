package radar

import "testing"

var testPrefs = Preferences{
	Titles:           KeywordSet{Include: []string{"platform engineer", "sre", "software engineer"}, Exclude: []string{"intern", "principal"}},
	ExcludeLocations: []string{"canada", "uk", "london"},
	DealBreakers:     []string{"security clearance"},
	Tiers: []Tier{
		{Name: "local", Locations: []string{"raleigh", "durham"}},
		{Name: "remote", Remote: true},
		{Name: "relocation", MinComp: 250_000},
	},
}

func fp(v float64) *float64 { return &v }

func item(title string, locations []string, compMax *float64, description string) FetchedItem {
	if title == "" {
		title = "Senior Platform Engineer"
	}
	if locations == nil {
		locations = []string{"Raleigh, NC"}
	}
	return FetchedItem{ExternalID: "1", Company: "Acme", Title: title, Locations: locations, URL: "https://example.test/1", Description: description, CompMax: compMax}
}

func TestRejectsOnTitle(t *testing.T) {
	for title, reason := range map[string]string{
		"Marketing Manager":           "title: no include keyword",
		"Software Engineer Intern":    "title: excluded 'intern'",
		"Principal Platform Engineer": "title: excluded 'principal'",
	} {
		if got := Evaluate(item(title, nil, nil, ""), testPrefs); got.Reason != reason {
			t.Errorf("%s: %+v", title, got)
		}
	}
}

func TestLocalTierWinsOverRemote(t *testing.T) {
	if got := Evaluate(item("", []string{"Remote (US)", "Durham, NC"}, nil, ""), testPrefs); got.Tier != "local" {
		t.Fatalf("%+v", got)
	}
}

func TestRemoteTierNeedsARemoteLocationAfterExclusions(t *testing.T) {
	if got := Evaluate(item("", []string{"Remote (US)", "Remote (Canada)"}, nil, ""), testPrefs); got.Tier != "remote" {
		t.Fatalf("%+v", got)
	}
	got := Evaluate(item("", []string{"Remote (Canada)", "London, UK"}, nil, ""), testPrefs)
	if got.Passed || got.Reason != "location: all excluded" {
		t.Fatalf("%+v", got)
	}
}

func TestRelocationFloorAndUnknownComp(t *testing.T) {
	low := Evaluate(item("", []string{"Seattle, WA"}, fp(200_000), ""), testPrefs)
	if low.Passed || low.Tier != "relocation" || low.Reason != "comp: 200,000 below relocation floor 250,000" {
		t.Fatalf("%+v", low)
	}
	if !Evaluate(item("", []string{"Seattle, WA"}, fp(300_000), ""), testPrefs).Passed {
		t.Fatal("high comp should pass")
	}
	unknown := Evaluate(item("", []string{"Seattle, WA"}, nil, ""), testPrefs)
	if !unknown.Passed || unknown.Reason != "comp unknown" {
		t.Fatalf("%+v", unknown)
	}
}

func TestUnknownCompCanHaveItsOwnTier(t *testing.T) {
	prefs := Preferences{Tiers: []Tier{{Name: "relocation", MinComp: 250_000, UnknownCompTier: "relocation-no-salary"}}}
	if got := Evaluate(item("", []string{"Seattle, WA"}, nil, ""), prefs); got.Tier != "relocation-no-salary" {
		t.Fatalf("%+v", got)
	}
	if got := Evaluate(item("", []string{"Seattle, WA"}, fp(300_000), ""), prefs); got.Tier != "relocation" {
		t.Fatalf("%+v", got)
	}
}

func TestCompFloorDoesNotApplyToLocalTier(t *testing.T) {
	if !Evaluate(item("", nil, fp(120_000), ""), testPrefs).Passed {
		t.Fatal("local should ignore the relocation floor")
	}
}

func TestDealBreakerInDescriptionRejects(t *testing.T) {
	if got := Evaluate(item("", nil, nil, "Active security clearance required."), testPrefs); got.Reason != "deal-breaker 'security clearance'" {
		t.Fatalf("%+v", got)
	}
}

func TestEmptyPreferencesPassEverything(t *testing.T) {
	if !Evaluate(item("Anything", nil, nil, ""), Preferences{}).Passed {
		t.Fatal("should pass")
	}
}
