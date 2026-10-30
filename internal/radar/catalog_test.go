package radar

import (
	"slices"
	"strings"
	"testing"
)

func TestSourcesListsTheMailboxOnce(t *testing.T) {
	for _, tc := range []struct {
		name, watchlist string
		want            []string
	}{
		{"not in the watchlist", defaultWatchlist, []string{"Acme", "Globex", "Alert emails"}},
		{"in the watchlist", defaultWatchlist + "  - { name: Inbox, ats: email, board: gmail }\n", []string{"Acme", "Globex", "Inbox"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, harnessOptions{watchlist: tc.watchlist})
			catalog := &Catalog{DB: h.db, Ranking: h.scoring.Ranking, Config: h.scanner.Config, Clock: h.clock}
			sources, err := catalog.Sources(h.ctx)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, s := range sources {
				got = append(got, s.Name)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("sources = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOptionsForACompanyListsAllItsScoredItemsWithoutMarkingThemSent(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.scorer.set("Robot Engineer", 95, "robotics")
	h.scorer.set("Meh Engineer", 20, "software")
	h.scorer.set("Great Engineer", 80, "software")
	h.source.items["acme"] = []FetchedItem{fake("1", "Robot Engineer"), fake("2", "Meh Engineer")}
	h.source.items["globex"] = []FetchedItem{fake("3", "Great Engineer", company("Globex"))}
	h.scan()
	h.score()
	cmds := &Commands{DB: h.db, Digest: h.digest, Clock: h.clock,
		Catalog: &Catalog{DB: h.db, Ranking: h.scoring.Ranking, Config: h.scanner.Config, Clock: h.clock, MinScore: 50}}

	text, err := cmds.Handle(h.ctx, "/options", " acme ", 0)
	if err != nil {
		t.Fatal(err)
	}
	robot, meh := strings.Index(text, "Robot Engineer"), strings.Index(text, "Meh Engineer")
	if !strings.HasPrefix(text, "Acme: 2 scored item(s)") || robot < 0 || meh < robot || strings.Contains(text, "Great Engineer") {
		t.Fatalf("want both Acme items, best first, low scores included, nothing else:\n%s", text)
	}
	if digest := h.nextDigest().Text; !strings.Contains(digest, "Robot Engineer") {
		t.Fatalf("browsing a company marked items as sent:\n%s", digest)
	}

	if text, _ := cmds.Handle(h.ctx, "/options", "initech", 0); !strings.HasPrefix(text, "No scored items") {
		t.Fatal(text)
	}
}
