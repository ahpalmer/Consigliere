// Package pipeline tracks what the owner is actively pursuing: a short list moving from active (working on it) to
// waiting (submitted, no answer yet) to completed (with an outcome), with a checklist per pursuit and nudges.
package pipeline

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/store"
)

type Stage string

const (
	StageActive    Stage = "active"
	StageWaiting   Stage = "waiting"
	StageCompleted Stage = "completed"
)

// Outcomes a pursuit can complete with.
var Outcomes = []string{"offer", "rejected", "ghosted", "withdrawn", "dropped"}

// Step is one checklist item. Steps unlock by phase: preparation first, follow-up once submitted, interview steps
// once an interview is scheduled.
type Step struct{ Key, Label string }

var Steps = []Step{
	{"resume", "Tailor resume"},
	{"people", "Find people"},
	{"outreach", "Send outreach"},
	{"submit", "Submit application"},
	{"followup", "Follow up"},
	{"prep", "Prep for interview"},
	{"interview", "Interview"},
	{"thanks", "Send thank-you note"},
}

func stepLabel(key string) string {
	for _, s := range Steps {
		if s.Key == key {
			return s.Label
		}
	}
	return key
}

var ErrNotFound = errors.New("not found")

type Pursuit struct {
	ID                                 int64
	ItemID                             *int64
	Company, Title, URL                string
	Stage                              Stage
	Outcome, StageReached, Notes       string
	ActivatedAt                        time.Time
	AppliedAt, InterviewAt, FollowUpAt *time.Time
	GhostCheckAt, CompletedAt          *time.Time
	Done                               map[string]time.Time
	Fit                                *int   // from the linked item, if any
	Tier                               string // from the linked item, if any
}

func (p *Pursuit) Ref() string { return "P" + strconv.FormatInt(p.ID, 10) }

// VisibleSteps are the checklist steps that apply in the pursuit's current phase.
func (p *Pursuit) VisibleSteps() []Step {
	n := 4
	if _, submitted := p.Done["submit"]; submitted || p.AppliedAt != nil {
		n = 5
	}
	if p.InterviewAt != nil {
		n = len(Steps)
	}
	return Steps[:n]
}

// NextStep is the first visible step not done yet, or nil when the visible checklist is complete.
func (p *Pursuit) NextStep() *Step {
	for _, s := range p.VisibleSteps() {
		if _, done := p.Done[s.Key]; !done {
			return &s
		}
	}
	return nil
}

// Pipeline stores pursuits in the shared SQLite database.
type Pipeline struct {
	DB    *sql.DB
	Clock clock.Clock
	// Owner's time zone, for parsing and showing dates.
	Location     *time.Location
	SoftCap      int
	FollowUpDays int
	GhostDays    int
}

const pursuitColumns = `p."Id", p."ItemId", p."Company", p."Title", p."Url", p."Stage", p."Outcome", p."StageReached", p."Notes",
	p."ActivatedAt", p."AppliedAt", p."InterviewAt", p."FollowUpAt", p."GhostCheckAt", p."CompletedAt", r."FitScore", r."Tier"`

const pursuitFrom = ` FROM "Pursuits" p LEFT JOIN "RadarItems" r ON r."Id" = p."ItemId"`

func (pl *Pipeline) query(ctx context.Context, where string, args ...any) ([]*Pursuit, error) {
	rows, err := pl.DB.QueryContext(ctx, `SELECT `+pursuitColumns+pursuitFrom+` WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	var out []*Pursuit
	for rows.Next() {
		var (
			p                                                   Pursuit
			itemID, fit                                         sql.NullInt64
			outcome, reached, tier                              sql.NullString
			activated                                           string
			applied, interview, followUp, ghostCheck, completed sql.NullString
		)
		if err := rows.Scan(&p.ID, &itemID, &p.Company, &p.Title, &p.URL, &p.Stage, &outcome, &reached, &p.Notes,
			&activated, &applied, &interview, &followUp, &ghostCheck, &completed, &fit, &tier); err != nil {
			rows.Close()
			return nil, err
		}
		if itemID.Valid {
			p.ItemID = &itemID.Int64
		}
		if fit.Valid {
			f := int(fit.Int64)
			p.Fit = &f
		}
		p.Outcome, p.StageReached, p.Tier = outcome.String, reached.String, tier.String
		if p.ActivatedAt, err = store.ParseTime(activated); err != nil {
			rows.Close()
			return nil, err
		}
		for _, f := range []struct {
			dst **time.Time
			src sql.NullString
		}{{&p.AppliedAt, applied}, {&p.InterviewAt, interview}, {&p.FollowUpAt, followUp}, {&p.GhostCheckAt, ghostCheck}, {&p.CompletedAt, completed}} {
			if *f.dst, err = store.ParseTimePtr(f.src); err != nil {
				rows.Close()
				return nil, err
			}
		}
		p.Done = map[string]time.Time{}
		out = append(out, &p)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(out) == 0 {
		return out, err
	}

	byID := map[int64]*Pursuit{}
	ids := make([]int64, len(out))
	for i, p := range out {
		byID[p.ID], ids[i] = p, p.ID
	}
	steps, err := pl.DB.QueryContext(ctx, `SELECT "PursuitId", "Step", "DoneAt" FROM "PursuitSteps" WHERE "PursuitId" IN (`+store.Placeholders(len(ids))+`)`, store.Args(ids)...)
	if err != nil {
		return nil, err
	}
	defer steps.Close()
	for steps.Next() {
		var id int64
		var step, at string
		if err := steps.Scan(&id, &step, &at); err != nil {
			return nil, err
		}
		t, err := store.ParseTime(at)
		if err != nil {
			return nil, err
		}
		byID[id].Done[step] = t
	}
	return out, steps.Err()
}

func (pl *Pipeline) Get(ctx context.Context, id int64) (*Pursuit, error) {
	found, err := pl.query(ctx, `p."Id" = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, ErrNotFound
	}
	return found[0], nil
}

// Resolve finds a pursuit from what the owner typed: "P3" is pursuit 3; a plain number is a radar item id
// (its open pursuit, else its most recent one).
func (pl *Pipeline) Resolve(ctx context.Context, ref string) (*Pursuit, error) {
	ref = strings.TrimPrefix(strings.TrimSpace(ref), "#")
	if rest, ok := strings.CutPrefix(strings.ToUpper(ref), "P"); ok {
		id, err := strconv.ParseInt(rest, 10, 64)
		if err != nil {
			return nil, ErrNotFound
		}
		return pl.Get(ctx, id)
	}
	itemID, err := strconv.ParseInt(ref, 10, 64)
	if err != nil {
		return nil, ErrNotFound
	}
	found, err := pl.query(ctx, `p."ItemId" = ? ORDER BY (p."Stage" = 'completed'), p."Id" DESC LIMIT 1`, itemID)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, ErrNotFound
	}
	return found[0], nil
}

// List returns pursuits in a stage: active by activation, waiting by application date, completed newest first.
func (pl *Pipeline) List(ctx context.Context, stage Stage) ([]*Pursuit, error) {
	order := `p."ActivatedAt", p."Id"`
	switch stage {
	case StageWaiting:
		order = `p."AppliedAt", p."Id"`
	case StageCompleted:
		order = `p."CompletedAt" DESC, p."Id" DESC`
	}
	return pl.query(ctx, `p."Stage" = ? ORDER BY `+order, string(stage))
}

func (pl *Pipeline) CountActive(ctx context.Context) (int, error) {
	var n int
	err := pl.DB.QueryRowContext(ctx, `SELECT count(*) FROM "Pursuits" WHERE "Stage" = 'active'`).Scan(&n)
	return n, err
}

// ActivateItem starts pursuing a radar item. If the item already has a pursuit, that one is returned with
// created=false (whatever its stage).
func (pl *Pipeline) ActivateItem(ctx context.Context, itemID int64) (*Pursuit, bool, error) {
	if existing, err := pl.Resolve(ctx, strconv.FormatInt(itemID, 10)); err == nil {
		return existing, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}
	var company, title, url string
	err := pl.DB.QueryRowContext(ctx, `SELECT "Company", "Title", "Url" FROM "RadarItems" WHERE "Id" = ?`, itemID).Scan(&company, &title, &url)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrNotFound
	}
	if err != nil {
		return nil, false, err
	}
	p, err := pl.insert(ctx, &itemID, company, title, url)
	return p, err == nil, err
}

// ActivateManual starts pursuing something seen outside the radar.
func (pl *Pipeline) ActivateManual(ctx context.Context, url, company, title string) (*Pursuit, error) {
	return pl.insert(ctx, nil, company, title, url)
}

func (pl *Pipeline) insert(ctx context.Context, itemID *int64, company, title, url string) (*Pursuit, error) {
	now := store.FormatTime(pl.Clock.Now())
	var item any
	if itemID != nil {
		item = *itemID
	}
	var id int64
	err := pl.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO "Pursuits" ("ItemId", "Company", "Title", "Url", "Stage", "ActivatedAt", "UpdatedAt")
			VALUES (?, ?, ?, ?, 'active', ?, ?)`, item, company, title, url, now, now)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		return event(ctx, tx, id, now, "activated", "")
	})
	if err != nil {
		return nil, err
	}
	return pl.Get(ctx, id)
}

// Applied moves a pursuit to waiting, ticks "submit", and schedules the follow-up and ghost checks.
func (pl *Pipeline) Applied(ctx context.Context, id int64) (*Pursuit, error) {
	now := pl.Clock.Now()
	stamp := store.FormatTime(now)
	err := pl.update(ctx, id, func(tx *sql.Tx, p *Pursuit) error {
		if p.Stage == StageCompleted {
			return fmt.Errorf("%s is completed; /reopen %s first", p.Ref(), p.Ref())
		}
		applied := now
		if p.AppliedAt != nil {
			applied = *p.AppliedAt // re-tapping "applied" keeps the original date
		}
		if _, err := tx.ExecContext(ctx, `UPDATE "Pursuits" SET "Stage" = 'waiting', "AppliedAt" = ?, "FollowUpAt" = ?, "GhostCheckAt" = ?,
			"UpdatedAt" = ? WHERE "Id" = ?`, store.FormatTime(applied), store.FormatTime(applied.AddDate(0, 0, pl.FollowUpDays)),
			store.FormatTime(applied.AddDate(0, 0, pl.GhostDays)), stamp, id); err != nil {
			return err
		}
		if err := tick(ctx, tx, id, "submit", stamp); err != nil {
			return err
		}
		return event(ctx, tx, id, stamp, "applied", "")
	})
	return pl.after(ctx, id, err)
}

// Interview records an interview time and moves the pursuit back to active (there's work to do again).
func (pl *Pipeline) Interview(ctx context.Context, id int64, at time.Time, note string) (*Pursuit, error) {
	stamp := store.FormatTime(pl.Clock.Now())
	err := pl.update(ctx, id, func(tx *sql.Tx, p *Pursuit) error {
		if p.Stage == StageCompleted {
			return fmt.Errorf("%s is completed; /reopen %s first", p.Ref(), p.Ref())
		}
		if _, err := tx.ExecContext(ctx, `UPDATE "Pursuits" SET "Stage" = 'active', "InterviewAt" = ?, "FollowUpAt" = NULL, "GhostCheckAt" = NULL,
			"UpdatedAt" = ? WHERE "Id" = ?`, store.FormatTime(at), stamp, id); err != nil {
			return err
		}
		// An interview means the application went through and following up is moot.
		for _, step := range []string{"submit", "followup"} {
			if err := tick(ctx, tx, id, step, stamp); err != nil {
				return err
			}
		}
		// A new interview round needs fresh prep and a fresh thank-you.
		if _, err := tx.ExecContext(ctx, `DELETE FROM "PursuitSteps" WHERE "PursuitId" = ? AND "Step" IN ('prep', 'interview', 'thanks')`, id); err != nil {
			return err
		}
		return event(ctx, tx, id, stamp, "interview", strings.TrimSpace(at.In(pl.Location).Format(timeFormat)+" "+note))
	})
	return pl.after(ctx, id, err)
}

// Complete closes a pursuit with an outcome and records how far it got.
func (pl *Pipeline) Complete(ctx context.Context, id int64, outcome, note string) (*Pursuit, error) {
	outcome = strings.ToLower(strings.TrimSpace(outcome))
	if !slices.Contains(Outcomes, outcome) {
		return nil, fmt.Errorf("outcome must be one of: %s", strings.Join(Outcomes, ", "))
	}
	stamp := store.FormatTime(pl.Clock.Now())
	err := pl.update(ctx, id, func(tx *sql.Tx, p *Pursuit) error {
		reached := "active"
		switch {
		case outcome == "offer":
			reached = "offer"
		case p.InterviewAt != nil:
			reached = "interview"
		case p.AppliedAt != nil:
			reached = "applied"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE "Pursuits" SET "Stage" = 'completed', "Outcome" = ?, "StageReached" = ?, "CompletedAt" = ?,
			"FollowUpAt" = NULL, "GhostCheckAt" = NULL, "UpdatedAt" = ? WHERE "Id" = ?`, outcome, reached, stamp, stamp, id); err != nil {
			return err
		}
		if note != "" {
			if err := appendNote(ctx, tx, p, note, pl.Clock.Now().In(pl.Location)); err != nil {
				return err
			}
		}
		return event(ctx, tx, id, stamp, "completed", strings.TrimSpace(outcome+" "+note))
	})
	return pl.after(ctx, id, err)
}

// Reopen puts a completed pursuit back: waiting if it was submitted and never reached an interview, else active.
func (pl *Pipeline) Reopen(ctx context.Context, id int64) (*Pursuit, error) {
	stamp := store.FormatTime(pl.Clock.Now())
	err := pl.update(ctx, id, func(tx *sql.Tx, p *Pursuit) error {
		if p.Stage != StageCompleted {
			return fmt.Errorf("%s isn't completed", p.Ref())
		}
		stage := StageActive
		if p.AppliedAt != nil && p.InterviewAt == nil {
			stage = StageWaiting
		}
		if _, err := tx.ExecContext(ctx, `UPDATE "Pursuits" SET "Stage" = ?, "Outcome" = NULL, "StageReached" = NULL, "CompletedAt" = NULL,
			"UpdatedAt" = ? WHERE "Id" = ?`, string(stage), stamp, id); err != nil {
			return err
		}
		return event(ctx, tx, id, stamp, "reopened", "")
	})
	return pl.after(ctx, id, err)
}

// Tick marks a checklist step done (idempotent).
func (pl *Pipeline) Tick(ctx context.Context, id int64, step string) (*Pursuit, error) {
	if !slices.ContainsFunc(Steps, func(s Step) bool { return s.Key == step }) {
		return nil, fmt.Errorf("unknown step %q", step)
	}
	stamp := store.FormatTime(pl.Clock.Now())
	err := pl.update(ctx, id, func(tx *sql.Tx, _ *Pursuit) error {
		if err := tick(ctx, tx, id, step, stamp); err != nil {
			return err
		}
		return event(ctx, tx, id, stamp, "step", step)
	})
	return pl.after(ctx, id, err)
}

// Untick marks a checklist step not done, for a mis-tap.
func (pl *Pipeline) Untick(ctx context.Context, id int64, step string) (*Pursuit, error) {
	if !slices.ContainsFunc(Steps, func(s Step) bool { return s.Key == step }) {
		return nil, fmt.Errorf("unknown step %q", step)
	}
	stamp := store.FormatTime(pl.Clock.Now())
	err := pl.update(ctx, id, func(tx *sql.Tx, _ *Pursuit) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM "PursuitSteps" WHERE "PursuitId" = ? AND "Step" = ?`, id, step)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		return event(ctx, tx, id, stamp, "step_undone", step)
	})
	return pl.after(ctx, id, err)
}

// Event is one entry in a pursuit's history.
type Event struct {
	At           time.Time
	Kind, Detail string
}

// Events returns a pursuit's history, oldest first.
func (pl *Pipeline) Events(ctx context.Context, id int64) ([]Event, error) {
	rows, err := pl.DB.QueryContext(ctx, `SELECT "At", "Kind", coalesce("Detail", '') FROM "PursuitEvents" WHERE "PursuitId" = ? ORDER BY "At", "Id"`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var at string
		if err := rows.Scan(&at, &e.Kind, &e.Detail); err != nil {
			return nil, err
		}
		if e.At, err = store.ParseTime(at); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ForItems maps radar item ids to their pursuit (the open one, else the most recent).
func (pl *Pipeline) ForItems(ctx context.Context, itemIDs []int64) (map[int64]*Pursuit, error) {
	out := map[int64]*Pursuit{}
	if len(itemIDs) == 0 {
		return out, nil
	}
	found, err := pl.query(ctx, `p."ItemId" IN (`+store.Placeholders(len(itemIDs))+`) ORDER BY (p."Stage" <> 'completed'), p."Id"`, store.Args(itemIDs)...)
	if err != nil {
		return nil, err
	}
	for _, p := range found { // later rows win: open beats completed, newer beats older
		out[*p.ItemID] = p
	}
	return out, nil
}

// FollowedUp ticks "followup" and schedules the second reminder (2 × FollowUpDays after applying), if it's
// still ahead; after that, reminders stop.
func (pl *Pipeline) FollowedUp(ctx context.Context, id int64) (*Pursuit, error) {
	now := pl.Clock.Now()
	stamp := store.FormatTime(now)
	err := pl.update(ctx, id, func(tx *sql.Tx, p *Pursuit) error {
		var next any
		if p.AppliedAt != nil {
			if second := p.AppliedAt.AddDate(0, 0, 2*pl.FollowUpDays); second.After(now) {
				next = store.FormatTime(second)
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE "Pursuits" SET "FollowUpAt" = ?, "UpdatedAt" = ? WHERE "Id" = ?`, next, stamp, id); err != nil {
			return err
		}
		if err := tick(ctx, tx, id, "followup", stamp); err != nil {
			return err
		}
		return event(ctx, tx, id, stamp, "followed_up", "")
	})
	return pl.after(ctx, id, err)
}

// KeepWaiting answers a ghost check with "not yet": ask again in two weeks.
func (pl *Pipeline) KeepWaiting(ctx context.Context, id int64) (*Pursuit, error) {
	now := pl.Clock.Now()
	stamp := store.FormatTime(now)
	err := pl.update(ctx, id, func(tx *sql.Tx, _ *Pursuit) error {
		if _, err := tx.ExecContext(ctx, `UPDATE "Pursuits" SET "GhostCheckAt" = ?, "UpdatedAt" = ? WHERE "Id" = ?`,
			store.FormatTime(now.AddDate(0, 0, 14)), stamp, id); err != nil {
			return err
		}
		return event(ctx, tx, id, stamp, "keep_waiting", "")
	})
	return pl.after(ctx, id, err)
}

func (pl *Pipeline) AddNote(ctx context.Context, id int64, note string) (*Pursuit, error) {
	stamp := store.FormatTime(pl.Clock.Now())
	err := pl.update(ctx, id, func(tx *sql.Tx, p *Pursuit) error {
		if err := appendNote(ctx, tx, p, note, pl.Clock.Now().In(pl.Location)); err != nil {
			return err
		}
		return event(ctx, tx, id, stamp, "note", note)
	})
	return pl.after(ctx, id, err)
}

// GhostChecksDue returns waiting pursuits whose ghost check has come up, and pushes each check a week out so an
// unanswered question isn't repeated every day.
func (pl *Pipeline) GhostChecksDue(ctx context.Context) ([]*Pursuit, error) {
	now := pl.Clock.Now()
	due, err := pl.query(ctx, `p."Stage" = 'waiting' AND p."GhostCheckAt" IS NOT NULL AND p."GhostCheckAt" <= ? ORDER BY p."AppliedAt"`, store.FormatTime(now))
	if err != nil || len(due) == 0 {
		return due, err
	}
	ids := make([]int64, len(due))
	for i, p := range due {
		ids[i] = p.ID
	}
	_, err = pl.DB.ExecContext(ctx, `UPDATE "Pursuits" SET "GhostCheckAt" = ? WHERE "Id" IN (`+store.Placeholders(len(ids))+`)`,
		append([]any{store.FormatTime(now.AddDate(0, 0, 7))}, store.Args(ids)...)...)
	return due, err
}

func (pl *Pipeline) update(ctx context.Context, id int64, f func(tx *sql.Tx, p *Pursuit) error) error {
	p, err := pl.Get(ctx, id)
	if err != nil {
		return err
	}
	return pl.tx(ctx, func(tx *sql.Tx) error { return f(tx, p) })
}

func (pl *Pipeline) after(ctx context.Context, id int64, err error) (*Pursuit, error) {
	if err != nil {
		return nil, err
	}
	return pl.Get(ctx, id)
}

func (pl *Pipeline) tx(ctx context.Context, f func(tx *sql.Tx) error) error {
	tx, err := pl.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := f(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func tick(ctx context.Context, tx *sql.Tx, id int64, step, stamp string) error {
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO "PursuitSteps" ("PursuitId", "Step", "DoneAt") VALUES (?, ?, ?)`, id, step, stamp)
	return err
}

func event(ctx context.Context, tx *sql.Tx, id int64, stamp, kind, detail string) error {
	var d any
	if detail != "" {
		d = detail
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO "PursuitEvents" ("PursuitId", "At", "Kind", "Detail") VALUES (?, ?, ?, ?)`, id, stamp, kind, d)
	return err
}

func appendNote(ctx context.Context, tx *sql.Tx, p *Pursuit, note string, now time.Time) error {
	notes := strings.TrimSpace(p.Notes + "\n" + now.Format("Jan 2") + ": " + strings.TrimSpace(note))
	_, err := tx.ExecContext(ctx, `UPDATE "Pursuits" SET "Notes" = ? WHERE "Id" = ?`, notes, p.ID)
	return err
}
