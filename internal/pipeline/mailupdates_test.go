package pipeline

import (
	"strings"
	"testing"
	"time"

	"github.com/ahpalmer/consigliere/internal/datapack"
	"github.com/ahpalmer/consigliere/internal/mail"
	"github.com/ahpalmer/consigliere/internal/radar"
)

func (f *fixture) mailUpdates() *MailUpdates { return &MailUpdates{Pipeline: f.pl, Channel: f.channel} }

func (f *fixture) activate(t *testing.T, itemID int64) *Pursuit {
	t.Helper()
	p, _, err := f.pl.ActivateItem(f.ctx, itemID)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (f *fixture) handle(t *testing.T, u mail.Update) string {
	t.Helper()
	note, err := f.mailUpdates().HandleUpdate(f.ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	return note
}

func TestReceivedEmailMovesThePursuitToWaitingAndTracksUnknownJobs(t *testing.T) {
	f := newFixture(t)
	p := f.activate(t, 1) // Acme · Platform Engineer

	if note := f.handle(t, mail.Update{Kind: mail.UpdateReceived, Company: "Acme, Inc.", Role: "Platform Engineer", Subject: "Thanks for applying"}); note != "received → P1" {
		t.Fatalf("note %q", note)
	}
	got, _ := f.pl.Get(f.ctx, p.ID)
	if got.Stage != StageWaiting || got.AppliedAt == nil || got.FollowUpAt == nil {
		t.Fatalf("pursuit %+v", got)
	}
	events, _ := f.pl.Events(f.ctx, p.ID)
	if last := events[len(events)-1]; last.Kind != "applied" || !strings.Contains(events[len(events)-2].Detail, "received: ") {
		t.Fatalf("events %+v", events)
	}

	// Applied somewhere Consigliere never saw: the confirmation starts tracking it.
	f.handle(t, mail.Update{Kind: mail.UpdateReceived, Company: "Globex", Role: "SRE", Links: []string{"https://globex.example/jobs/1"}})
	waiting, _ := f.pl.List(f.ctx, StageWaiting)
	if len(waiting) != 2 || waiting[1].Company != "Globex" || waiting[1].URL != "https://globex.example/jobs/1" {
		t.Fatalf("waiting %+v", waiting)
	}
	if len(f.channel.sent) != 0 {
		t.Fatalf("received confirmations shouldn't alert: %+v", f.channel.sent)
	}
}

func TestInterviewEmailsReactivateAndAlert(t *testing.T) {
	f := newFixture(t)
	p := f.activate(t, 1)
	f.pl.Applied(f.ctx, p.ID)

	// No time stated yet: back to active, with a nudge to record the time.
	f.handle(t, mail.Update{Kind: mail.UpdateInterviewRequest, Company: "Acme", Summary: "Pick a time for a phone screen.", From: "Jo <jo@acme.com>"})
	got, _ := f.pl.Get(f.ctx, p.ID)
	if got.Stage != StageActive || got.FollowUpAt != nil || !strings.Contains(f.channel.last(t).text, "/interview P1") {
		t.Fatalf("pursuit %+v, sent %q", got, f.channel.last(t).text)
	}

	// A confirmed time sets the interview (in the owner's zone when the email gives none) and offers prep.
	f.handle(t, mail.Update{Kind: mail.UpdateScheduling, Company: "Acme", Role: "Platform Engineer", When: "2026-11-05T14:00"})
	got, _ = f.pl.Get(f.ctx, p.ID)
	want := time.Date(2026, 11, 5, 14, 0, 0, 0, newYork)
	if got.InterviewAt == nil || !got.InterviewAt.Equal(want) {
		t.Fatalf("interview %v", got.InterviewAt)
	}
	last := f.channel.last(t)
	if !strings.Contains(last.text, "Thu Nov 5, 2:00 PM") || len(last.buttons) != 1 || last.buttons[0].Data != "pp:prep:1" {
		t.Fatalf("sent %+v", last)
	}
}

func TestMatchesBySenderDomainAndClosestTitle(t *testing.T) {
	f := newFixture(t)
	f.activate(t, 1) // Platform Engineer
	sre := f.activate(t, 2)
	got, err := f.pl.Match(f.ctx, "", "Site Reliability Engineer (SRE)", "Talent <talent@careers.acme.com>")
	if err != nil || got == nil || got.ID != sre.ID {
		t.Fatalf("match %+v %v", got, err)
	}
	if got, _ := f.pl.Match(f.ctx, "", "SRE", "x@notacme.com"); got != nil {
		t.Fatalf("matched a lookalike domain: %+v", got)
	}
}

func TestOffersAskAndRejectionsWaitForTheBriefing(t *testing.T) {
	f := newFixture(t)
	p := f.activate(t, 1)
	f.handle(t, mail.Update{Kind: mail.UpdateOffer, Company: "Acme", Summary: "Offer letter attached."})
	if last := f.channel.last(t); !strings.Contains(last.text, "Offer") || last.buttons[0].Data != "pp:out:1:offer" {
		t.Fatalf("offer %+v", last)
	}

	before := len(f.channel.sent)
	f.handle(t, mail.Update{Kind: mail.UpdateRejection, Company: "Acme", Summary: "We went with another candidate."})
	got, _ := f.pl.Get(f.ctx, p.ID)
	if got.Stage == StageCompleted || len(f.channel.sent) != before {
		t.Fatal("a rejection must not close anything or alert by itself")
	}

	pack, err := datapack.Open("../../samples/data-pack")
	if err != nil {
		t.Fatal(err)
	}
	ranking := radar.RankingFactory{Config: radar.ConfigLoader{Pack: pack}}
	b := &Briefing{Pipeline: f.pl, Digest: &radar.Digest{DB: f.db, Ranking: ranking, Clock: f.clock, MaxItems: 5, MinScore: 50}, Channel: f.channel}
	if err := b.Run(f.ctx); err != nil {
		t.Fatal(err)
	}
	var proposal *sent
	for i := range f.channel.sent {
		if strings.HasPrefix(f.channel.sent[i].text, "📭 P1") {
			proposal = &f.channel.sent[i]
		}
	}
	if proposal == nil || proposal.buttons[0].Data != "pp:out:1:rejected" {
		t.Fatalf("no rejection proposal in %+v", f.channel.sent)
	}
}

func TestBackfillOnlyRecordsHistory(t *testing.T) {
	f := newFixture(t)
	p := f.activate(t, 1)
	f.pl.Applied(f.ctx, p.ID)
	f.handle(t, mail.Update{Kind: mail.UpdateInterviewRequest, Company: "Acme", Backfill: true})
	f.handle(t, mail.Update{Kind: mail.UpdateInterviewRequest, Company: "Initech", Backfill: true}) // untracked, old: ignored
	got, _ := f.pl.Get(f.ctx, p.ID)
	if got.Stage != StageWaiting || len(f.channel.sent) != 0 {
		t.Fatalf("stage %s, sent %+v", got.Stage, f.channel.sent)
	}
	if all, _ := f.pl.query(f.ctx, `1 = 1`); len(all) != 1 {
		t.Fatalf("pursuits %d", len(all))
	}
}

func TestRecruiterOutreachAlertsWithoutCreatingAnything(t *testing.T) {
	f := newFixture(t)
	f.handle(t, mail.Update{Kind: mail.UpdateRecruiterOutreach, Company: "Hooli", Role: "Staff SRE", Links: []string{"https://hooli.example/jobs/9"}})
	if text := f.channel.last(t).text; !strings.Contains(text, "/activate https://hooli.example/jobs/9 Hooli - Staff SRE") {
		t.Fatalf("sent %q", text)
	}
	if all, _ := f.pl.query(f.ctx, `1 = 1`); len(all) != 0 {
		t.Fatal("outreach created a pursuit")
	}
}
