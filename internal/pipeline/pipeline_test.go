package pipeline

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ahpalmer/consigliere/internal/agent"
	"github.com/ahpalmer/consigliere/internal/channel"
	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/store"
)

type sent struct {
	text    string
	buttons []channel.Button
}

type fakeChannel struct {
	mu   sync.Mutex
	sent []sent
}

func (f *fakeChannel) Name() string { return "fake" }
func (f *fakeChannel) Notify(_ context.Context, text string, buttons ...channel.Button) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sent{text, buttons})
	return nil
}

func (f *fakeChannel) last(t *testing.T) sent {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		t.Fatal("nothing sent")
	}
	return f.sent[len(f.sent)-1]
}

type fixture struct {
	ctx     context.Context
	db      *sql.DB
	clock   *clock.Fake
	pl      *Pipeline
	cmd     *Commands
	channel *fakeChannel
}

var newYork, _ = time.LoadLocation("America/New_York")

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	clk := clock.NewFake(time.Date(2026, 10, 28, 13, 0, 0, 0, time.UTC)) // Wed 9:00 EDT
	pl := &Pipeline{DB: db, Clock: clk, Location: newYork, SoftCap: 2, FollowUpDays: 7, GhostDays: 30}
	ch := &fakeChannel{}
	f := &fixture{ctx: ctx, db: db, clock: clk, pl: pl, channel: ch,
		cmd: &Commands{Pipeline: pl, Channel: ch, Tasks: agent.NewTasks(ctx), State: agent.NewState(clk.Now())}}
	for i, title := range []string{"Platform Engineer", "SRE", "Build Engineer"} {
		_, err := db.Exec(`INSERT INTO "RadarItems" ("Source", "Board", "ExternalId", "Company", "Title", "Locations", "Url", "Description",
			"FirstSeenAt", "LastSeenAt", "ContentHash", "Fingerprint", "Passed", "FitScore", "Tier")
			VALUES ('greenhouse', 'acme', ?, 'Acme', ?, 'Remote', ?, 'Build fleets.', ?, ?, 'h', 'f', 1, 88, 'remote')`,
			i, title, "https://example.com/"+title, store.FormatTime(clk.Now()), store.FormatTime(clk.Now()))
		if err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *fixture) run(t *testing.T, text string) string {
	t.Helper()
	command, args, _ := strings.Cut(text, " ")
	reply, err := f.cmd.Handle(f.ctx, command, args, 1)
	if err != nil {
		t.Fatalf("%s: %v", text, err)
	}
	return reply
}

func TestLifecycleFromActivationToCompletion(t *testing.T) {
	f := newFixture(t)
	ctx, pl := f.ctx, f.pl

	p, created, err := pl.ActivateItem(ctx, 1)
	if err != nil || !created || p.Ref() != "P1" || p.Company != "Acme" || p.Stage != StageActive || *p.Fit != 88 {
		t.Fatalf("%+v %v %v", p, created, err)
	}
	if again, created, _ := pl.ActivateItem(ctx, 1); created || again.ID != p.ID {
		t.Fatal("activating twice created a second pursuit")
	}
	if p.NextStep().Key != "resume" || len(p.VisibleSteps()) != 4 {
		t.Fatalf("%+v", p.VisibleSteps())
	}

	p, _ = pl.Tick(ctx, p.ID, "resume")
	if p.NextStep().Key != "people" {
		t.Fatal(p.NextStep())
	}

	p, err = pl.Applied(ctx, p.ID)
	if err != nil || p.Stage != StageWaiting || p.AppliedAt == nil {
		t.Fatalf("%+v %v", p, err)
	}
	if _, done := p.Done["submit"]; !done || len(p.VisibleSteps()) != 5 {
		t.Fatal("applying should tick submit and unlock follow-up")
	}
	if !p.FollowUpAt.Equal(p.AppliedAt.AddDate(0, 0, 7)) || !p.GhostCheckAt.Equal(p.AppliedAt.AddDate(0, 0, 30)) {
		t.Fatalf("follow-up %v ghost %v", p.FollowUpAt, p.GhostCheckAt)
	}

	// Day 7: the briefing asks for a follow-up; following up schedules the second nudge at day 14.
	f.clock.Advance(7 * 24 * time.Hour)
	waiting, _ := pl.List(ctx, StageWaiting)
	if agenda := pl.Agenda(nil, waiting); !strings.Contains(agenda, "📬 Follow up: P1") {
		t.Fatalf("agenda: %q", agenda)
	}
	p, _ = pl.FollowedUp(ctx, p.ID)
	if !p.FollowUpAt.Equal(p.AppliedAt.AddDate(0, 0, 14)) {
		t.Fatal(p.FollowUpAt)
	}
	f.clock.Advance(7 * 24 * time.Hour)
	if p, _ = pl.FollowedUp(ctx, p.ID); p.FollowUpAt != nil {
		t.Fatal("reminders should stop after the second follow-up")
	}

	// An interview brings it back to active with the interview steps unlocked.
	at := time.Date(2026, 11, 20, 14, 0, 0, 0, newYork)
	p, err = pl.Interview(ctx, p.ID, at, "phone screen")
	if err != nil || p.Stage != StageActive || !p.InterviewAt.Equal(at) || p.GhostCheckAt != nil || len(p.VisibleSteps()) != len(Steps) {
		t.Fatalf("%+v %v", p, err)
	}
	if p.NextStep().Key != "people" {
		t.Fatal("unfinished earlier steps still come first:", p.NextStep())
	}

	p, err = pl.Complete(ctx, p.ID, "Rejected", "team reorg")
	if err != nil || p.Stage != StageCompleted || p.Outcome != "rejected" || p.StageReached != "interview" || !strings.Contains(p.Notes, "team reorg") {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := pl.Applied(ctx, p.ID); err == nil {
		t.Fatal("applied on a completed pursuit")
	}
	completed, _ := pl.List(ctx, StageCompleted)
	if text := pl.CompletedList(completed); !strings.Contains(text, "1 rejected") || !strings.Contains(text, "1 applied → 1 interviewed → 0 offers") {
		t.Fatal(text)
	}

	p, err = pl.Reopen(ctx, p.ID)
	if err != nil || p.Stage != StageActive || p.Outcome != "" {
		t.Fatalf("%+v %v", p, err)
	}

	var events int
	f.db.QueryRow(`SELECT count(*) FROM "PursuitEvents" WHERE "PursuitId" = ?`, p.ID).Scan(&events)
	if events != 8 { // activated, step, applied, followed_up ×2, interview, completed, reopened
		t.Fatalf("events = %d", events)
	}
}

func TestCompleteValidatesOutcomeAndRecordsStageReached(t *testing.T) {
	f := newFixture(t)
	p, _, _ := f.pl.ActivateItem(f.ctx, 2)
	if _, err := f.pl.Complete(f.ctx, p.ID, "vanished", ""); err == nil {
		t.Fatal("accepted an unknown outcome")
	}
	p, _ = f.pl.Complete(f.ctx, p.ID, "dropped", "")
	if p.StageReached != "active" {
		t.Fatal(p.StageReached)
	}
	p2, _ := f.pl.ActivateManual(f.ctx, "https://jobs.example.com/1", "Globex", "SWE")
	f.pl.Applied(f.ctx, p2.ID)
	p2, _ = f.pl.Complete(f.ctx, p2.ID, "offer", "")
	if p2.StageReached != "offer" || p2.ItemID != nil {
		t.Fatalf("%+v", p2)
	}
	if reopened, _ := f.pl.Reopen(f.ctx, p2.ID); reopened.Stage != StageWaiting {
		t.Fatal("a submitted pursuit without an interview reopens into waiting")
	}
}

func TestResolveAcceptsPursuitAndItemIDs(t *testing.T) {
	f := newFixture(t)
	p, _, _ := f.pl.ActivateItem(f.ctx, 3)
	for _, ref := range []string{"P1", "p1", "3", "#3"} {
		got, err := f.pl.Resolve(f.ctx, ref)
		if err != nil || got.ID != p.ID {
			t.Fatalf("%s: %v %v", ref, got, err)
		}
	}
	for _, ref := range []string{"P9", "2", "abc", ""} {
		if _, err := f.pl.Resolve(f.ctx, ref); err != ErrNotFound {
			t.Fatalf("%s: %v", ref, err)
		}
	}
}

func TestGhostChecksAreAskedOnceAWeek(t *testing.T) {
	f := newFixture(t)
	p, _, _ := f.pl.ActivateItem(f.ctx, 1)
	f.pl.Applied(f.ctx, p.ID)
	f.clock.Advance(29 * 24 * time.Hour)
	if due, _ := f.pl.GhostChecksDue(f.ctx); len(due) != 0 {
		t.Fatal("too early")
	}
	f.clock.Advance(24 * time.Hour)
	if due, _ := f.pl.GhostChecksDue(f.ctx); len(due) != 1 {
		t.Fatal("ghost check not due at 30 days")
	}
	if due, _ := f.pl.GhostChecksDue(f.ctx); len(due) != 0 {
		t.Fatal("asked twice in one day")
	}
	p, _ = f.pl.KeepWaiting(f.ctx, p.ID)
	if !p.GhostCheckAt.Equal(f.clock.Now().AddDate(0, 0, 14)) {
		t.Fatal(p.GhostCheckAt)
	}
}

func TestCommandsAndButtons(t *testing.T) {
	f := newFixture(t)

	if reply := f.run(t, "/activate 1"); reply != "" {
		t.Fatal(reply)
	}
	card := f.channel.last(t)
	if !strings.Contains(card.text, "🎯 P1 · Platform Engineer · Acme") || !strings.Contains(card.text, "⬜ Tailor resume  ← next") {
		t.Fatal(card.text)
	}
	labels := func(bs []channel.Button) string {
		var out []string
		for _, b := range bs {
			out = append(out, b.Label)
		}
		return strings.Join(out, "|")
	}
	if got := labels(card.buttons); got != "✅ Tailor resume|✍️ Kit|📨 Applied|🗂 Complete" {
		t.Fatal(got)
	}

	// Ticking via the button updates the card in place.
	toast, err := f.cmd.HandleCallback(f.ctx, card.buttons[0].Data)
	if err != nil || toast != "✅ Tailor resume" {
		t.Fatal(toast, err)
	}
	text, buttons, ok, err := f.cmd.Refresh(f.ctx, card.buttons[0].Data)
	if err != nil || !ok || !strings.Contains(text, "✅ Tailor resume") || buttons[0].Label != "✅ Find people" {
		t.Fatal(text, err)
	}

	// Soft cap warns but doesn't block.
	f.run(t, "/activate 2")
	if reply := f.run(t, "/activate 3"); !strings.Contains(reply, "3 active (soft cap 2)") {
		t.Fatal(reply)
	}
	if reply := f.run(t, "/activate 1"); reply != "Already in the pipeline as P1." {
		t.Fatal(reply)
	}
	if reply := f.run(t, "/activate https://jobs.example.com/x Globex - Staff SRE"); !strings.Contains(reply, "4 active") || !strings.Contains(f.channel.last(t).text, "P4 · Staff SRE · Globex") {
		t.Fatal(reply, f.channel.last(t).text)
	}
	if reply := f.run(t, "/activate https://jobs.example.com/x Globex Staff SRE"); !strings.HasPrefix(reply, "Usage") {
		t.Fatal(reply)
	}

	if reply := f.run(t, "/applied P1"); !strings.Contains(reply, "P1 is in Waiting") || !strings.Contains(reply, "Acme's recruiting emails") {
		t.Fatal(reply)
	}
	if reply := f.run(t, "/waiting"); !strings.Contains(reply, "P1 · Platform Engineer · Acme") || !strings.Contains(reply, "applied today") {
		t.Fatal(reply)
	}
	if reply := f.run(t, "/done P2 people"); reply != "" || !strings.Contains(f.channel.last(t).text, "✅ Find people") {
		t.Fatal(reply)
	}
	if reply := f.run(t, "/done P2 dance"); !strings.Contains(reply, "unknown step") {
		t.Fatal(reply)
	}
	if reply := f.run(t, "/note P2 recruiter is Dana"); reply != "📝 Noted on P2." {
		t.Fatal(reply)
	}
	if reply := f.run(t, "/interview P1 fri 2pm onsite loop"); reply == "" || !strings.Contains(f.channel.last(t).text, "🎤 Interview: Fri Oct 30, 2:00 PM") {
		t.Fatal(reply, f.channel.last(t).text)
	}
	if reply := f.run(t, "/applied P9"); !strings.Contains(reply, "No pursuit P9") {
		t.Fatal(reply)
	}

	// Complete with no outcome asks via buttons; tapping one completes it.
	if reply := f.run(t, "/complete P2"); reply != "" {
		t.Fatal(reply)
	}
	ask := f.channel.last(t)
	if !strings.Contains(ask.text, "How did P2") || len(ask.buttons) != len(Outcomes) {
		t.Fatalf("%+v", ask)
	}
	toast, _ = f.cmd.HandleCallback(f.ctx, ask.buttons[1].Data) // rejected
	text, _, _, _ = f.cmd.Refresh(f.ctx, ask.buttons[1].Data)
	if toast != "🗂 Completed" || !strings.Contains(text, "rejected before applying") {
		t.Fatal(toast, text)
	}
	if reply := f.run(t, "/completed"); !strings.Contains(reply, "1 rejected") {
		t.Fatal(reply)
	}

	// "🎯 Make active" on an alert for something already in the pipeline.
	if toast, _ := f.cmd.HandleCallback(f.ctx, ActivateButton(1).Data); toast != "Already P1" {
		t.Fatal(toast)
	}

	// The briefing lists the interview and the remaining active next steps.
	active, _ := f.pl.List(f.ctx, StageActive)
	waiting, _ := f.pl.List(f.ctx, StageWaiting)
	agenda := f.pl.Agenda(active, waiting)
	if !strings.Contains(agenda, "🎤 Fri Oct 30, 2:00 PM: P1") || !strings.Contains(agenda, "prep not done") || !strings.Contains(agenda, "🎯 P3 · Acme: Tailor resume") {
		t.Fatal(agenda)
	}
}

func TestParseWhen(t *testing.T) {
	now := time.Date(2026, 10, 28, 13, 0, 0, 0, time.UTC) // Wednesday 9:00 in New York
	cases := []struct {
		in, want, rest string
	}{
		{"2026-11-03 14:00 panel", "2026-11-03 14:00", "panel"},
		{"11/3 2pm", "2026-11-03 14:00", ""},
		{"11/3 2:30 pm with Dana", "2026-11-03 14:30", "with Dana"},
		{"1/5", "2027-01-05 09:00", ""}, // no year: the next Jan 5
		{"tomorrow 9:30am", "2026-10-29 09:30", ""},
		{"fri 1pm", "2026-10-30 13:00", ""},
		{"wednesday 10am", "2026-11-04 10:00", ""}, // same weekday means next week
		{"today 2 recruiter", "2026-10-28 09:00", "2 recruiter"},
	}
	for _, c := range cases {
		got, rest, err := ParseWhen(c.in, now, newYork)
		if err != nil || got.Format("2006-01-02 15:04") != c.want || rest != c.rest || got.Location() != newYork {
			t.Errorf("%q → %v %q %v; want %s %q", c.in, got, rest, err, c.want, c.rest)
		}
	}
	if _, _, err := ParseWhen("someday", now, newYork); err == nil {
		t.Error("parsed nonsense")
	}
}
