package radar

import (
	"strings"
	"testing"
	"time"
)

func TestSecondScanRecordsNothingNew(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.source.items["acme"] = []FetchedItem{fake("1", "Platform Engineer"), fake("2", "Recruiter")}

	first, second := h.scan(), h.scan()

	if first.New() != 2 || first.Passed() != 1 || second.New() != 0 || len(h.rows()) != 2 {
		t.Fatalf("first %+v second %+v rows %d", first, second, len(h.rows()))
	}
}

func TestRescanNeverChangesFirstSeenOrPostedDates(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	when := time.Date(2026, 10, 1, 9, 30, 0, 0, time.FixedZone("EDT", -4*3600))
	h.source.items["acme"] = []FetchedItem{fake("1", "Engineer", posted(when))}
	h.scan()
	firstSeen := h.rows()[0].FirstSeenAt.String

	h.clock.Advance(72 * time.Hour)
	edited := fake("1", "Engineer", posted(when))
	edited.Description = "Edited"
	h.source.items["acme"] = []FetchedItem{edited}
	h.scan()

	r := h.rows()[0]
	if r.Description != "Edited" {
		t.Fatalf("description not refreshed: %q", r.Description)
	}
	if r.PostedAt.String != "2026-10-01 13:30:00" {
		t.Fatalf("posted date changed or not UTC: %q", r.PostedAt.String)
	}
	if r.FirstSeenAt.String != firstSeen {
		t.Fatalf("first seen changed: %q -> %q", firstSeen, r.FirstSeenAt.String)
	}
	if r.LastSeenAt.String != "2026-10-29 12:00:00" {
		t.Fatalf("last seen: %q", r.LastSeenAt.String)
	}
}

func TestChangedPostingIsRefreshedAndRefilteredUntilDigested(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.source.items["acme"] = []FetchedItem{fake("1", "Engineer"), fake("2", "Engineer II")}
	h.scan()
	h.db.Exec(`UPDATE "RadarItems" SET "DigestedAt" = '2026-10-26 12:00:00' WHERE "ExternalId" = '1'`)

	pay := 150_000.0
	a, b := fake("1", "Recruiter"), fake("2", "Recruiter")
	a.CompMax, b.CompMax = &pay, &pay
	h.source.items["acme"] = []FetchedItem{a, b}
	if s := h.scan(); s.New() != 0 {
		t.Fatalf("new %d", s.New())
	}

	rows := h.rows()
	for _, r := range rows {
		if r.CompMax.Float64 != 150_000 {
			t.Fatalf("comp not refreshed: %+v", r)
		}
	}
	if !rows[0].Passed || rows[1].Passed {
		t.Fatalf("digested item must keep its result; undigested one is re-filtered: %v %v", rows[0].Passed, rows[1].Passed)
	}
}

func TestPreferenceEditsApplyToExistingUnsentItems(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.source.items["acme"] = []FetchedItem{fake("1", "Engineer", location("Austin, TX"))}
	h.scan()
	if h.rows()[0].Tier.String != "anywhere" {
		t.Fatal("expected anywhere tier")
	}
	h.writePreferences(strings.Replace(defaultPreferences, "locations: [raleigh]", "locations: [raleigh, austin]", 1))
	h.scan()
	if h.rows()[0].Tier.String != "local" {
		t.Fatalf("tier %q", h.rows()[0].Tier.String)
	}
}

func TestSamePostingOnTwoBoardsIsMarkedDuplicate(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.source.items["acme"] = []FetchedItem{fake("1", "SRE Engineer", company("Acme"))}
	h.source.items["globex"] = []FetchedItem{fake("x9", "sre  engineer", company("ACME"))}
	h.scan()
	rows := h.rows()
	if rows[0].DuplicateOf.Valid || rows[1].DuplicateOf.Int64 != rows[0].ID {
		t.Fatalf("%+v", rows)
	}
}

func TestUnsupportedSourceReportsErrorWithoutFailingScan(t *testing.T) {
	h := newHarness(t, harnessOptions{watchlist: defaultWatchlist + "  - { name: Initech, ats: workday, board: initech }\n"})
	s := h.scan()
	found := false
	for _, r := range s.Sources {
		if r.Name == "Initech" && strings.Contains(r.Error, "unsupported") {
			found = true
		}
	}
	var runs int
	h.db.QueryRow(`SELECT count(*) FROM "SourceRuns"`).Scan(&runs)
	if !found || runs != 3 {
		t.Fatalf("found=%v runs=%d", found, runs)
	}
}

const aggregatorWatchlist = `
sources:
  - { name: Acme board, ats: fake, board: acme }
  - { name: Alerts, ats: fakeagg, board: inbox }
`

func TestAggregatorCopyDefersToExistingPrimary(t *testing.T) {
	h := newHarness(t, harnessOptions{watchlist: aggregatorWatchlist})
	h.source.items["acme"] = []FetchedItem{fake("1", "Platform Engineer")}
	h.scan()
	h.aggregator.items["inbox"] = []FetchedItem{fake("linkedin:9", "Platform  engineer", company("ACME"), location("Raleigh, NC (Hybrid)"))}
	h.scan()
	rows := h.rows()
	if rows[1].Source != "fakeagg" || rows[1].DuplicateOf.Int64 != rows[0].ID {
		t.Fatalf("%+v", rows)
	}
}

func TestPrimaryArrivingLaterTakesOverFromAggregatorCopy(t *testing.T) {
	h := newHarness(t, harnessOptions{watchlist: aggregatorWatchlist})
	h.aggregator.items["inbox"] = []FetchedItem{fake("linkedin:9", "Platform Engineer", location("Raleigh, NC (Hybrid)"))}
	h.scan()
	h.source.items["acme"] = []FetchedItem{fake("1", "Platform Engineer")}
	h.scan()
	rows := h.rows()
	agg, primary := rows[0], rows[1]
	if primary.DuplicateOf.Valid || agg.DuplicateOf.Int64 != primary.ID {
		t.Fatalf("%+v", rows)
	}
}

func TestScoresAreNotRecomputedUntilTheKeyChanges(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.source.items["acme"] = []FetchedItem{fake("1", "Engineer")}
	h.scan()
	h.score()
	h.score()
	if len(h.scorer.calls) != 1 || h.rows()[0].FitScore.Int64 != 60 || h.rows()[0].Category.String != "software" {
		t.Fatalf("calls %v rows %+v", h.scorer.calls, h.rows())
	}
	h.scorer.version = "v2" // e.g. the profile was edited
	h.score()
	if len(h.scorer.calls) != 2 {
		t.Fatalf("calls %v", h.scorer.calls)
	}
}

func TestIdenticalContentReusesAScoreWithinABatch(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.source.items["acme"] = []FetchedItem{fake("1", "Engineer", location("Raleigh, NC"))}
	h.source.items["globex"] = []FetchedItem{fake("1", "Engineer", location("Austin, TX"))}
	h.scan()
	s := h.score()
	if len(h.scorer.calls) != 1 || s.Scored != 1 || s.Cached != 1 {
		t.Fatalf("calls %v summary %+v", h.scorer.calls, s)
	}
}

func TestStaleItemsAreNeverScoredOrDigested(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	now := h.clock.Now()
	h.source.items["acme"] = []FetchedItem{fake("1", "Fresh Engineer", posted(now.AddDate(0, 0, -3))), fake("2", "Old Engineer", posted(now.AddDate(0, 0, -120)))}
	h.scan()
	h.score()
	if len(h.scorer.calls) != 1 || h.scorer.calls[0] != "Fresh Engineer" {
		t.Fatalf("calls %v", h.scorer.calls)
	}
	text := h.nextDigest().Text
	if !strings.Contains(text, "Fresh Engineer") || strings.Contains(text, "Old Engineer") || !strings.Contains(text, "posted 3d ago") {
		t.Fatal(text)
	}
}

func TestDeclinedItemsAreNotRetriedOrDigested(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.scorer.byTitle["Weird Engineer"] = nil
	h.source.items["acme"] = []FetchedItem{fake("1", "Weird Engineer")}
	h.scan()
	h.score()
	h.score()
	if len(h.scorer.calls) != 1 || h.nextDigest().Text != "" {
		t.Fatalf("calls %v", h.scorer.calls)
	}
}

func TestDigestOrdersByTierCategoryAndPriorityAdjustedFit(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.scorer.set("Robot Engineer", 95, "robotics")
	h.scorer.set("Good Engineer", 70, "software")
	h.scorer.set("Great Engineer", 80, "software")
	h.scorer.set("Remote Engineer", 99, "software")
	h.source.items["acme"] = []FetchedItem{fake("1", "Robot Engineer"), fake("2", "Good Engineer"), fake("4", "Remote Engineer", location("Austin, TX"))}
	// Globex is low priority: 80 - 15 = 65, so it ranks below Acme's 70.
	h.source.items["globex"] = []FetchedItem{fake("3", "Great Engineer", company("Globex"))}
	h.scan()
	h.score()

	text := h.nextDigest().Text
	last := -1
	for _, title := range []string{"Good Engineer", "Great Engineer", "Robot Engineer", "Remote Engineer"} {
		i := strings.Index(text, title)
		if i <= last {
			t.Fatalf("%s out of order:\n%s", title, text)
		}
		last = i
	}
	if !strings.Contains(text, "— local · software (2) —") || !strings.Contains(text, "— local · robotics (1) —") {
		t.Fatal(text)
	}
}

func TestDigestSkipsLowScoresPagesAndResets(t *testing.T) {
	h := newHarness(t, harnessOptions{digestMax: 2})
	h.scorer.set("Meh Engineer", 20, "software")
	h.source.items["acme"] = []FetchedItem{fake("1", "A Engineer"), fake("2", "B Engineer"), fake("3", "C Engineer"), fake("4", "Meh Engineer")}
	h.scan()
	h.score()

	p1, p2, p3 := h.nextDigest(), h.nextDigest(), h.nextDigest()
	if p1.Shown != 2 || p1.Remaining != 1 || p2.Shown != 1 || p2.Remaining != 0 || p3.Text != "" {
		t.Fatalf("%+v %+v %+v", p1, p2, p3)
	}
	if strings.Contains(p1.Text+p2.Text, "Meh Engineer") {
		t.Fatal("low score shown")
	}
	h.digest.Reset(h.ctx)
	if again := h.nextDigest(); again.Text != p1.Text {
		t.Fatalf("reset should start from the top:\n%s\nvs\n%s", again.Text, p1.Text)
	}
}

func TestStrongLocalMatchAlertsOnceAndSkipsTheDigest(t *testing.T) {
	h := newHarness(t, harnessOptions{alertMin: 75})
	h.scorer.set("Dream Engineer", 90, "software")
	h.scorer.set("Far Engineer", 95, "software")
	h.source.items["acme"] = []FetchedItem{fake("1", "Dream Engineer"), fake("2", "Far Engineer", location("Austin, TX"))}
	h.scan()
	h.score()
	h.score()

	if len(h.channel.sent) != 1 || !strings.Contains(h.channel.sent[0].text, "Dream Engineer") {
		t.Fatalf("%+v", h.channel.sent)
	}
	buttons := h.channel.sent[0].buttons
	if len(buttons) != 2 || buttons[0].Label != "👍 Good" || buttons[1].Label != "👎 Bad" {
		t.Fatalf("%+v", buttons)
	}
	text := h.nextDigest().Text
	if strings.Contains(text, "Dream Engineer") || !strings.Contains(text, "Far Engineer") {
		t.Fatal(text)
	}
	if toast, err := h.feedback.HandleCallback(h.ctx, buttons[0].Data); err != nil || toast != "Noted 👍" {
		t.Fatalf("%q %v", toast, err)
	}
	if h.rows()[0].Feedback.Int64 != 1 {
		t.Fatal("feedback not stored")
	}
}

func TestHighPriorityCompaniesLeadTheDigestAndAlertInAnyTier(t *testing.T) {
	prefs := defaultPreferences + "company_priority:\n  high: [Sierra, \"GitHub\"]\n  low: [Acme]\n"
	h := newHarness(t, harnessOptions{preferences: prefs, alertMin: 75})
	h.scorer.set("Local Engineer", 70, "software")
	h.scorer.set("Agent Engineer", 65, "software")   // 65 + 15 = 80: alerts even though "anywhere" isn't instant
	h.scorer.set("Defense Engineer", 70, "software") // Sierra Nevada must not match "Sierra"
	h.source.items["acme"] = []FetchedItem{
		fake("1", "Local Engineer", company("Initech")),
		fake("2", "Agent Engineer", company("Sierra"), location("Austin, TX")),
		fake("3", "Defense Engineer", company("Sierra Nevada Corporation"), location("Austin, TX")),
	}
	h.scan()
	h.score()

	if len(h.channel.sent) != 1 || !strings.HasPrefix(h.channel.sent[0].text, "★ Agent Engineer") {
		t.Fatalf("%+v", h.channel.sent)
	}
	h.digest.Reset(h.ctx)
	text := h.nextDigest().Text
	if !strings.Contains(text, "— ★ priority · anywhere · software (1) —") ||
		strings.Index(text, "Agent Engineer") > strings.Index(text, "Local Engineer") {
		t.Fatal(text)
	}
}

func TestCompanyNamesNormalizeForExactMatching(t *testing.T) {
	for in, want := range map[string]string{"GitHub, Inc.": "github", "Google LLC": "google", "Sierra Nevada Corporation": "sierra nevada", "Co": "co"} {
		if got := NormalizeCompany(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestPausedAgentStopsScoring(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.state.Pause()
	h.source.items["acme"] = []FetchedItem{fake("1", "Engineer")}
	h.scan()
	h.score()
	if len(h.scorer.calls) != 0 {
		t.Fatalf("calls %v", h.scorer.calls)
	}
}
