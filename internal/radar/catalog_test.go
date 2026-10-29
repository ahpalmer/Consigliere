package radar

import (
	"slices"
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
