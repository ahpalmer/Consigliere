package radar

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// testdata/*.json are trimmed copies of real public board responses.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestGreenhouseDecodesEscapedHTMLAndExtractsPay(t *testing.T) {
	items, err := ParseGreenhouse(fixture(t, "greenhouse.json"), WatchedSource{Name: "Fallback Co", Board: "anthropic"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items", len(items))
	}
	swe := items[0]
	if swe.ExternalID != "5098025008" || swe.Company != "Anthropic" {
		t.Fatalf("%+v", swe)
	}
	if !slices.Equal(swe.Locations, []string{"San Francisco, CA", "New York City, NY"}) {
		t.Fatalf("locations %v", swe.Locations)
	}
	if swe.CompMin == nil || *swe.CompMin != 405_000 || *swe.CompMax != 485_000 {
		t.Fatalf("pay %v %v", swe.CompMin, swe.CompMax)
	}
	if strings.Contains(swe.Description, "<") || strings.Contains(swe.Description, "&lt;") {
		t.Fatal("description still has markup")
	}
	if swe.PostedAt == nil || !strings.HasPrefix(swe.URL, "https://") {
		t.Fatalf("%+v", swe)
	}
	if items[1].CompMax != nil {
		t.Fatal("second item should have no pay")
	}
}

func TestLeverUsesWatchlistNameAndFlagsRemote(t *testing.T) {
	items, err := ParseLever(fixture(t, "lever.json"), WatchedSource{Name: "Palantir", Board: "palantir"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Company != "Palantir" || items[1].Company != "Palantir" {
		t.Fatalf("%+v", items)
	}
	if !slices.Contains(items[0].Locations, "Remote") || slices.Contains(items[1].Locations, "Remote") {
		t.Fatalf("remote flags: %v / %v", items[0].Locations, items[1].Locations)
	}
	if !strings.Contains(items[1].Description, "What We Require") {
		t.Fatal("list sections should be folded into the description")
	}
}

func TestAshbyReadsStructuredSalaryAndSecondaryLocations(t *testing.T) {
	items, err := ParseAshby(fixture(t, "ashby.json"), WatchedSource{Name: "Ramp", Board: "ramp"})
	if err != nil {
		t.Fatal(err)
	}
	s := items[0]
	if s.Title != "Security Engineer, Cloud" || *s.CompMin != 211_400 || *s.CompMax != 290_600 || s.CompCurrency != "USD" {
		t.Fatalf("%+v", s)
	}
	if s.Locations[0] != "New York, NY (HQ)" || !slices.Contains(s.Locations, "Remote (US)") {
		t.Fatalf("%v", s.Locations)
	}
}
