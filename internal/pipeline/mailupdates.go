package pipeline

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ahpalmer/consigliere/internal/channel"
	"github.com/ahpalmer/consigliere/internal/mail"
	"github.com/ahpalmer/consigliere/internal/radar"
	"github.com/ahpalmer/consigliere/internal/store"
)

// MailUpdates applies what emails say about the owner's applications to the pipeline:
//   - received: the pursuit moves to waiting (one is created when the email is about a job not tracked yet);
//   - interview request / scheduling / assessment: back to active, with the interview time when the email states
//     one, and an instant alert;
//   - offer: an instant alert asking the owner to confirm;
//   - rejection: recorded, and proposed as "completed: rejected" in the next morning briefing;
//   - recruiter outreach: an instant alert (nothing is created);
//   - anything else: recorded in the pursuit's history.
//
// Nothing is ever replied to, and nothing completes a pursuit without the owner's tap.
type MailUpdates struct {
	Pipeline *Pipeline
	Channel  channel.Channel
}

// Companies are the companies of open pursuits, whose own mail the email source then reads.
func (m *MailUpdates) Companies(ctx context.Context) ([]string, error) {
	open, err := m.Pipeline.query(ctx, `p."Stage" <> 'completed'`)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range open {
		out = append(out, p.Company)
	}
	return out, nil
}

func (m *MailUpdates) HandleUpdate(ctx context.Context, u mail.Update) (string, error) {
	p, err := m.Pipeline.Match(ctx, u.Company, u.Role, u.From)
	if err != nil {
		return "", err
	}
	company, role := u.Company, u.Role
	if p != nil {
		company, role = p.Company, p.Title
	}
	if role == "" {
		role = "(role not named)"
	}
	opens := slices.Contains([]string{mail.UpdateReceived, mail.UpdateInterviewRequest, mail.UpdateScheduling, mail.UpdateAssessment, mail.UpdateOffer}, u.Kind)
	if p == nil && company != "" && opens && (!u.Backfill || u.Kind == mail.UpdateReceived) {
		// An email about an application that isn't tracked yet (applied outside Consigliere): track it now.
		url := ""
		if len(u.Links) > 0 {
			url = u.Links[0]
		}
		if p, err = m.Pipeline.ActivateManual(ctx, url, company, role); err != nil {
			return "", err
		}
	}
	if p == nil {
		if u.Kind == mail.UpdateRecruiterOutreach && !u.Backfill {
			return "recruiter outreach", m.notify(ctx, outreachText(u))
		}
		return "update for an untracked job: " + u.Kind, nil
	}

	detail := strings.TrimSpace(u.Kind + ": " + u.Summary)
	if u.Subject != "" {
		detail += " (email: " + u.Subject + ")"
	}
	if err := m.Pipeline.Record(ctx, p.ID, "email", detail); err != nil {
		return "", err
	}
	note := fmt.Sprintf("%s → %s", u.Kind, p.Ref())
	if u.Backfill && u.Kind != mail.UpdateReceived {
		return note, nil // history only: an old interview request shouldn't reopen anything today
	}

	switch u.Kind {
	case mail.UpdateReceived:
		if p.Stage == StageActive {
			_, err = m.Pipeline.Applied(ctx, p.ID)
		}
		return note, err

	case mail.UpdateInterviewRequest, mail.UpdateScheduling:
		if at, ok := m.parseWhen(u.When); ok {
			if p, err = m.Pipeline.Interview(ctx, p.ID, at, "from email"); err != nil {
				return "", err
			}
			text := fmt.Sprintf("📅 Interview: %s · %s · %s, %s.", p.Ref(), p.Title, p.Company, at.In(m.Pipeline.Location).Format(timeFormat))
			return note, m.notify(ctx, withDetails(text, u), channel.Button{Label: "📚 Prep pack", Data: fmt.Sprintf("%sprep:%d", prefix, p.ID)})
		}
		if p, err = m.Pipeline.Reactivate(ctx, p.ID, u.Kind); err != nil {
			return "", err
		}
		text := fmt.Sprintf("📅 Interview request: %s · %s · %s.", p.Ref(), p.Title, p.Company)
		return note, m.notify(ctx, withDetails(text, u)+fmt.Sprintf("\nOnce it's booked: /interview %s <when>", p.Ref()))

	case mail.UpdateAssessment:
		if p, err = m.Pipeline.Reactivate(ctx, p.ID, u.Kind); err != nil {
			return "", err
		}
		text := fmt.Sprintf("📝 Assessment: %s · %s · %s.", p.Ref(), p.Title, p.Company)
		if u.Deadline != "" {
			text += " Due " + u.Deadline + "."
		}
		return note, m.notify(ctx, withDetails(text, u))

	case mail.UpdateOffer:
		text := fmt.Sprintf("🎉 Offer: %s · %s · %s. Check the email; once it's real, file it.", p.Ref(), p.Title, p.Company)
		return note, m.notify(ctx, withDetails(text, u), channel.Button{Label: "🎉 Complete as offer", Data: fmt.Sprintf("%sout:%d:offer", prefix, p.ID)})
	}
	return note, nil // rejections wait for the briefing; others are history
}

func (m *MailUpdates) notify(ctx context.Context, text string, buttons ...channel.Button) error {
	if m.Channel == nil {
		return nil
	}
	return m.Channel.Notify(ctx, text, buttons...)
}

// parseWhen reads the model's ISO 8601 time; one without a zone is the owner's local time.
func (m *MailUpdates) parseWhen(when string) (time.Time, bool) {
	when = strings.TrimSpace(when)
	if t, err := time.Parse(time.RFC3339, when); err == nil {
		return t, true
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if t, err := time.ParseInLocation(layout, when, m.Pipeline.Location); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func withDetails(text string, u mail.Update) string {
	if u.Summary != "" {
		text += "\n" + u.Summary
	}
	for _, l := range u.Links {
		text += "\n" + l
	}
	return text
}

func outreachText(u mail.Update) string {
	text := "📨 Recruiter"
	if u.Company != "" {
		text += " at " + u.Company
	}
	if u.Role != "" {
		text += " about " + u.Role
	}
	text = withDetails(text+".", u)
	if u.Company != "" {
		link := "<link>"
		if len(u.Links) > 0 {
			link = u.Links[0]
		}
		role := u.Role
		if role == "" {
			role = "<title>"
		}
		text += fmt.Sprintf("\nTo pursue it: /activate %s %s - %s", link, u.Company, role)
	}
	return text
}

// Match finds the open pursuit an email is about: the same company (by name, or the sender's domain), preferring
// the closest title, then the most recent. Nil when none matches.
func (pl *Pipeline) Match(ctx context.Context, company, role, from string) (*Pursuit, error) {
	open, err := pl.query(ctx, `p."Stage" <> 'completed' ORDER BY p."Id" DESC`)
	if err != nil {
		return nil, err
	}
	want := squash(company)
	var best *Pursuit
	bestScore := -1
	for _, p := range open {
		name := squash(p.Company)
		if name == "" || (name != want && !mail.FromCompany(from, []string{p.Company})) {
			continue
		}
		if score := overlap(p.Title, role); score > bestScore {
			best, bestScore = p, score
		}
	}
	return best, nil
}

func squash(company string) string {
	return strings.ReplaceAll(radar.NormalizeCompany(company), " ", "")
}

// overlap counts title words two titles share ("Senior Platform Engineer" vs "Platform Engineer II": 2).
func overlap(a, b string) int {
	words := map[string]bool{}
	for _, w := range strings.Fields(radar.NormalizeCompany(a)) {
		words[w] = true
	}
	n := 0
	for _, w := range strings.Fields(radar.NormalizeCompany(b)) {
		if words[w] {
			n++
			words[w] = false
		}
	}
	return n
}

// Record adds an entry to a pursuit's history without changing it.
func (pl *Pipeline) Record(ctx context.Context, id int64, kind, detail string) error {
	stamp := store.FormatTime(pl.Clock.Now())
	return pl.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE "Pursuits" SET "UpdatedAt" = ? WHERE "Id" = ?`, stamp, id); err != nil {
			return err
		}
		return event(ctx, tx, id, stamp, kind, detail)
	})
}

// Reactivate moves a pursuit back to active because the company asked for something (an interview, an
// assessment): the application clearly went through, so "submit" is ticked and waiting reminders stop.
func (pl *Pipeline) Reactivate(ctx context.Context, id int64, reason string) (*Pursuit, error) {
	stamp := store.FormatTime(pl.Clock.Now())
	err := pl.update(ctx, id, func(tx *sql.Tx, p *Pursuit) error {
		if p.Stage == StageCompleted {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE "Pursuits" SET "Stage" = 'active', "FollowUpAt" = NULL, "GhostCheckAt" = NULL, "UpdatedAt" = ?
			WHERE "Id" = ?`, stamp, id); err != nil {
			return err
		}
		if err := tick(ctx, tx, id, "submit", stamp); err != nil {
			return err
		}
		return event(ctx, tx, id, stamp, "reactivated", reason)
	})
	return pl.after(ctx, id, err)
}

// EmailRejections are open pursuits whose email said no since the given time, for the briefing to propose closing.
func (pl *Pipeline) EmailRejections(ctx context.Context, since time.Time) ([]*Pursuit, error) {
	return pl.query(ctx, `p."Stage" <> 'completed' AND p."Id" IN (SELECT "PursuitId" FROM "PursuitEvents"
		WHERE "Kind" = 'email' AND "Detail" LIKE 'rejection:%' AND "At" >= ?) ORDER BY p."Id"`, store.FormatTime(since))
}
