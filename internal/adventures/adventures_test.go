package adventures

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ahpalmer/consigliere/internal/channel"
	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/datapack"
	"github.com/ahpalmer/consigliere/internal/llm"
	"github.com/ahpalmer/consigliere/internal/store"
)

type fixture struct {
	ctx   context.Context
	store *Store
	clock *clock.Fake
	pack  *datapack.Pack
	sent  *sentMessages
	job   *Reminders
}

type sentMessages struct {
	mu   sync.Mutex
	msgs []string
	btns [][]channel.Button
}

func (s *sentMessages) Name() string { return "test" }
func (s *sentMessages) Notify(_ context.Context, text string, buttons ...channel.Button) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, text)
	s.btns = append(s.btns, buttons)
	return nil
}

func (s *sentMessages) take() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.msgs
	s.msgs = nil
	return out
}

// newFixture starts at 9am New York time on Thu 2026-12-03, with settings in the pack.
func newFixture(t *testing.T, settings string) *fixture {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	if settings != "" {
		os.MkdirAll(filepath.Join(dir, "adventures"), 0o755)
		os.WriteFile(filepath.Join(dir, SettingsPath), []byte(settings), 0o644)
	}
	pack, err := datapack.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ny, _ := time.LoadLocation("America/New_York")
	clk := clock.NewFake(time.Date(2026, 12, 3, 9, 0, 0, 0, ny))
	s := &Store{DB: db, Clock: clk, Location: ny}
	sent := &sentMessages{}
	return &fixture{ctx: ctx, store: s, clock: clk, pack: pack, sent: sent, job: &Reminders{Store: s, Pack: pack, Channel: sent}}
}

func (f *fixture) run(t *testing.T) []string {
	t.Helper()
	if err := f.job.Run(f.ctx); err != nil {
		t.Fatal(err)
	}
	return f.sent.take()
}

func TestIdeasAndPlanning(t *testing.T) {
	f := newFixture(t, "home: Raleigh, NC\n")
	ctx := f.ctx
	if _, err := f.store.AddIdea(ctx, Idea{Title: " "}); err == nil {
		t.Fatal("an idea needs a title")
	}
	if _, err := f.store.AddIdea(ctx, Idea{Title: "Picnic", Cost: "cheap"}); err == nil {
		t.Fatal("cost is $ to $$$$")
	}
	hike, err := f.store.AddIdea(ctx, Idea{Title: "Hike Umstead", Description: "Company Mill trail.", Place: "Umstead State Park", Cost: "$",
		Tags: []string{"Outdoors", " outdoors", "active"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(hike.Tags, ",") != "outdoors,active" || hike.Source != "manual" {
		t.Fatalf("%+v", hike)
	}
	f.store.AddIdea(ctx, Idea{Title: "Pottery class", Cost: "$$"})
	f.store.SetFavorite(ctx, hike.ID, true)

	// Planned from an idea, the blanks come from it.
	a, err := f.store.Plan(ctx, Adventure{IdeaID: &hike.ID, Date: "2026-12-05", Time: "10:00"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Title != "Hike Umstead" || a.Place != "Umstead State Park" || a.Notes != "Company Mill trail." || a.Status != StatusPlanned {
		t.Fatalf("%+v", a)
	}
	for _, bad := range []Adventure{
		{Title: "x", Date: "Dec 5"},
		{Title: "x", Date: "2026-12-05", Time: "7pm"},
		{Title: "x", Date: "2026-12-05", PrepDue: "2026-12-06", PrepNote: "Book"},
		{Title: "x", Date: "2026-12-05", PrepDue: "2026-12-01"},
	} {
		var inv *InvalidError
		if _, err := f.store.Plan(ctx, bad); !errors.As(err, &inv) {
			t.Fatalf("%+v: %v", bad, err)
		}
	}

	if _, err := f.store.Rate(ctx, a.ID, 6, ""); err == nil {
		t.Fatal("ratings are 1-5")
	}
	if a, err = f.store.Rate(ctx, a.ID, 5, "Perfect weather"); err != nil || a.Status != StatusDone || *a.Rating != 5 || a.CompletedAt == nil {
		t.Fatalf("%+v %v", a, err)
	}
	ideas, _ := f.store.Ideas(ctx, "", false)
	if ideas[0].ID != hike.ID || ideas[0].Done != 1 || ideas[0].AvgRating != 5 {
		t.Fatalf("favorite, rated idea first: %+v", ideas[0])
	}
	if found, _ := f.store.Ideas(ctx, "pottery", false); len(found) != 1 {
		t.Fatalf("search: %d", len(found))
	}
	if favs, _ := f.store.Ideas(ctx, "", true); len(favs) != 1 {
		t.Fatalf("favorites: %d", len(favs))
	}

	// Archiving an idea keeps the adventure planned from it.
	if err := f.store.ArchiveIdea(ctx, hike.ID); err != nil {
		t.Fatal(err)
	}
	if history, _ := f.store.History(ctx); len(history) != 1 || history[0].Title != "Hike Umstead" {
		t.Fatalf("history: %+v", history)
	}

	skip, _ := f.store.Plan(ctx, Adventure{Title: "Concert", Date: "2026-12-10"})
	if skip, err = f.store.Skip(ctx, skip.ID); err != nil || skip.Status != StatusSkipped {
		t.Fatalf("%+v %v", skip, err)
	}
	if _, err := f.store.Rate(ctx, skip.ID, 4, ""); err == nil {
		t.Fatal("a skipped adventure can't be rated")
	}
}

func TestRemindersGoOnceEachOnTheirDay(t *testing.T) {
	f := newFixture(t, "cadence_days: 30\n")
	ctx := f.ctx
	// Thu Dec 3: dinner Sat Dec 5 at 7:30pm, book by Fri.
	a, err := f.store.Plan(ctx, Adventure{Title: "Dinner at Poole's", Place: "downtown", Date: "2026-12-05", Time: "19:30",
		PrepNote: "Book a table", PrepDue: "2026-12-04"})
	if err != nil {
		t.Fatal(err)
	}
	if msgs := f.run(t); len(msgs) != 0 {
		t.Fatalf("Thursday: %q", msgs)
	}

	f.clock.Advance(24 * time.Hour) // Fri: prep due, and the day before
	msgs := f.run(t)
	if len(msgs) != 2 || msgs[0] != "📝 Prep for Dinner at Poole's (tomorrow at 7:30pm): Book a table" || !strings.HasPrefix(msgs[1], "🗓 Tomorrow: Dinner at Poole's, tomorrow at 7:30pm, downtown") {
		t.Fatalf("Friday: %q", msgs)
	}
	if again := f.run(t); len(again) != 0 {
		t.Fatalf("sent twice: %q", again)
	}

	f.clock.Advance(24 * time.Hour) // Sat: the day of
	if msgs := f.run(t); len(msgs) != 1 || !strings.HasPrefix(msgs[0], "✨ Today: Dinner at Poole's, today at 7:30pm") {
		t.Fatalf("Saturday: %q", msgs)
	}

	f.clock.Advance(24 * time.Hour) // Sun: how was it?
	msgs = f.run(t)
	if len(msgs) != 1 || msgs[0] != "How was Dinner at Poole's?" || len(f.sent.btns[len(f.sent.btns)-1]) != 6 {
		t.Fatalf("Sunday: %q", msgs)
	}
	cb := &Callbacks{Store: f.store}
	toast, err := cb.HandleCallback(ctx, f.sent.btns[len(f.sent.btns)-1][3].Data)
	if err != nil || !strings.Contains(toast, "4⭐") {
		t.Fatalf("%q %v", toast, err)
	}
	if a, _ = f.store.Adventure(ctx, a.ID); a.Status != StatusDone || *a.Rating != 4 {
		t.Fatalf("%+v", a)
	}
}

func TestMovingAnAdventureSendsItsRemindersAgain(t *testing.T) {
	f := newFixture(t, "cadence_days: 30\n")
	a, _ := f.store.Plan(f.ctx, Adventure{Title: "Museum", Date: "2026-12-04"})
	if msgs := f.run(t); len(msgs) != 1 {
		t.Fatalf("%q", msgs)
	}
	moved := *a
	moved.Date = "2026-12-11"
	if _, err := f.store.Update(f.ctx, a.ID, moved); err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(7 * 24 * time.Hour) // Thu Dec 10
	if msgs := f.run(t); len(msgs) != 1 || !strings.HasPrefix(msgs[0], "🗓 Tomorrow: Museum") {
		t.Fatalf("%q", msgs)
	}
}

func TestNudgeWhenNothingIsPlanned(t *testing.T) {
	f := newFixture(t, "cadence_days: 14\n")
	ctx := f.ctx
	f.store.AddIdea(ctx, Idea{Title: "Kayak the Neuse"})
	msgs := f.run(t)
	if len(msgs) != 1 || !strings.Contains(msgs[0], "Nothing's planned") || !strings.Contains(msgs[0], "• Kayak the Neuse") {
		t.Fatalf("first nudge: %q", msgs)
	}
	f.clock.Advance(24 * time.Hour)
	if msgs := f.run(t); len(msgs) != 0 {
		t.Fatalf("nudged again the next day: %q", msgs)
	}

	// A recent adventure holds the nudge off until the cadence passes.
	a, _ := f.store.Plan(ctx, Adventure{Title: "Bowling", Date: "2026-12-04"})
	f.store.Rate(ctx, a.ID, 3, "")
	f.clock.Advance(14 * 24 * time.Hour) // Dec 18: 14 days since Dec 4, and since the last nudge
	msgs = f.run(t)
	if len(msgs) != 1 || !strings.Contains(msgs[0], "It's been 14 days since your last adventure") {
		t.Fatalf("%q", msgs)
	}

	// Something planned: no nudge.
	f.store.Plan(ctx, Adventure{Title: "Lights at the zoo", Date: "2027-02-10"})
	f.clock.Advance(30 * 24 * time.Hour)
	for _, m := range f.run(t) {
		if strings.Contains(m, "🧭") {
			t.Fatalf("nudged with something planned: %q", m)
		}
	}
}

func TestNoSettingsNoReminders(t *testing.T) {
	f := newFixture(t, "")
	f.store.Plan(f.ctx, Adventure{Title: "Museum", Date: "2026-12-04"})
	if msgs := f.run(t); len(msgs) != 0 {
		t.Fatalf("%q", msgs)
	}
	if _, ok, _ := LoadSettings(f.pack); ok {
		t.Fatal("the section should be off")
	}
}

type fakeLLM struct{ user string }

func (f *fakeLLM) Structured(_ context.Context, req llm.StructuredRequest) (string, error) {
	f.user = req.User
	return `{"ideas":[{"title":"Night hike","description":"Headlamps at the lake.","place":"Lake Johnson","cost":"$","duration":"an evening","tags":["Outdoors"]},
		{"title":"","description":"no title","place":"","cost":"$","duration":"","tags":[]},
		{"title":"Ramen crawl","description":"Three bowls.","place":"Durham","cost":"$$","duration":"an evening","tags":["food"]}]}`, nil
}

func TestGeneratorUsesRatingsAndCleansIdeas(t *testing.T) {
	f := newFixture(t, "home: Raleigh, NC\nlikes: outdoors, no crowds\n")
	ctx := f.ctx
	a, _ := f.store.Plan(ctx, Adventure{Title: "State fair", Date: "2026-10-20"})
	f.store.Rate(ctx, a.ID, 1, "Too crowded")
	fav, _ := f.store.AddIdea(ctx, Idea{Title: "Hike Umstead", Favorite: true})
	_ = fav

	llmFake := &fakeLLM{}
	settings, _, _ := LoadSettings(f.pack)
	ideas, err := Generator{LLM: llmFake, Owner: "Sam", Store: f.store}.Generate(ctx, settings, "rainy Saturday, under $50")
	if err != nil {
		t.Fatal(err)
	}
	if len(ideas) != 2 || ideas[0].Source != "generated" || ideas[0].Tags[0] != "outdoors" {
		t.Fatalf("%+v", ideas)
	}
	for _, want := range []string{"Home: Raleigh, NC", "rainy Saturday, under $50", "State fair (1/5): Too crowded", "Favorite ideas: Hike Umstead"} {
		if !strings.Contains(llmFake.user, want) {
			t.Fatalf("missing %q in:\n%s", want, llmFake.user)
		}
	}
}
