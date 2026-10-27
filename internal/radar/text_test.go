package radar

import "testing"

func TestExtractsAnnualPayRanges(t *testing.T) {
	cases := []struct {
		text     string
		min, max float64
	}{
		{"Annual Salary: $405,000 — $485,000 USD", 405_000, 485_000},
		{"Range: $211.4K – $290.6K • Offers Equity", 211_400, 290_600},
		{"pays $180k to $220k plus equity", 180_000, 220_000},
		{"$150,000 USD - $190,000 USD", 150_000, 190_000},
		{"the reasonably expected base pay range is between $132,000 and $198,450 for the level", 132_000, 198_450},
		{"The base pay range for this role is $196,000.00 - $294,000.00. Actual salary", 196_000, 294_000},
		{"Salary Range $250,000.00 - $263,000 USD", 250_000, 263_000},
		{"$160K/yr - $200K/yr · Actively recruiting", 160_000, 200_000},
	}
	for _, c := range cases {
		got := ExtractPayRange(c.text)
		if got == nil || got.Min != c.min || got.Max != c.max {
			t.Errorf("%q: got %+v", c.text, got)
		}
	}
}

func TestIgnoresNonAnnualOrMissingRanges(t *testing.T) {
	for _, text := range []string{"$45 - $60 per hour", "We raised $100M - $200M", "no numbers here",
		"Over $1B raised (including our $500M Series F)", "a $5,000 stipend and $1,000 learning budget"} {
		if got := ExtractPayRange(text); got != nil {
			t.Errorf("%q: got %+v", text, got)
		}
	}
}

func TestKeywordMatchingIsWholeWord(t *testing.T) {
	cases := []struct {
		text, kw string
		want     bool
	}{
		{"Milwaukee, WI", "uk", false},
		{"London, UK", "uk", true},
		{"Senior SRE", "sre", true},
		{"Measured results", "sre", false},
		{"Site Reliability Engineer", "site reliability", true},
		{"Café Engineer", "café", true},
		{"sre-sre", "sre", true},
	}
	for _, c := range cases {
		if got := ContainsKeyword(c.text, c.kw); got != c.want {
			t.Errorf("%q / %q: got %v", c.text, c.kw, got)
		}
	}
}

func TestHTMLToTextKeepsLineStructure(t *testing.T) {
	got := HTMLToText("&lt;p&gt;Hello&amp;nbsp;world&lt;/p&gt;&lt;ul&gt;&lt;li&gt;One&lt;/li&gt;&lt;li&gt;Two&lt;/li&gt;&lt;/ul&gt;")
	if got != "Hello world\n\nOne\n\nTwo" {
		t.Fatalf("%q", got)
	}
}

func TestHashMatchesTheDotNetImplementation(t *testing.T) {
	// SHA-256("a\x1fb"), first 16 bytes, lower hex. Verified against hashes the .NET version stored.
	if got := Hash("a", "b"); got != "f04cdced9736a69da6103f08a4daaf8c" {
		t.Fatalf("%s", got)
	}
}
