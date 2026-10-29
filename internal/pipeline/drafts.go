package pipeline

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ahpalmer/consigliere/internal/agent"
	"github.com/ahpalmer/consigliere/internal/kit"
	"github.com/ahpalmer/consigliere/internal/store"
)

const (
	DraftPending = "pending"
	DraftReady   = "ready"
	DraftFailed  = "failed"
)

var ErrPaused = errors.New("paused; resume first")

// Draft is something the model wrote for the owner to review: an apply kit, a prep pack or a thank-you note.
type Draft struct {
	ID                  int64
	PursuitID, ItemID   *int64
	Kind, Title, Status string
	Text, Error         string
	FileName            string // set when a file (the tailored resume) comes with it
	CreatedAt           time.Time
	FinishedAt          *time.Time
}

// Drafts writes drafts in the background and keeps them, so they can be read again from any channel.
type Drafts struct {
	Pipeline *Pipeline
	Kit      *kit.Kit
	Tasks    *agent.Tasks
	State    *agent.State
}

// Done is told when a draft finishes (ready or failed), e.g. to send it to chat. file is the attachment, if any.
type Done func(ctx context.Context, d Draft, file []byte) error

// StartKit builds an apply kit for p, or for a radar item when p is nil.
func (d *Drafts) StartKit(ctx context.Context, p *Pursuit, itemID *int64, done Done) (Draft, error) {
	var (
		job kit.Job
		err error
	)
	switch {
	case p != nil:
		job, err = d.Pipeline.JobFor(ctx, p)
		itemID = p.ItemID
	case itemID != nil:
		job, err = d.Pipeline.ItemJob(ctx, *itemID)
	default:
		return Draft{}, errors.New("a kit needs a pursuit or an item")
	}
	if err != nil {
		return Draft{}, err
	}
	return d.start(ctx, "kit", "Apply kit: "+job.Title+" · "+job.Company, p, itemID, done, func(ctx context.Context) (string, string, []byte, error) {
		result, err := d.Kit.Build(ctx, job)
		if err != nil {
			return "", "", nil, err
		}
		return result.Text, result.FileName, result.File, nil
	})
}

// StartPrep writes an interview prep pack, using the pursuit's notes and interview time plus extra.
func (d *Drafts) StartPrep(ctx context.Context, p *Pursuit, extra string, done Done) (Draft, error) {
	job, err := d.Pipeline.JobFor(ctx, p)
	if err != nil {
		return Draft{}, err
	}
	notes := strings.TrimSpace(p.Notes + "\n" + extra)
	if p.InterviewAt != nil {
		notes = "Interview: " + p.InterviewAt.In(d.Pipeline.Location).Format(timeFormat) + "\n" + notes
	}
	return d.start(ctx, "prep", "Prep pack: "+p.Title+" · "+p.Company, p, p.ItemID, done, func(ctx context.Context) (string, string, []byte, error) {
		text, err := d.Kit.Prep(ctx, job, notes)
		return text, "", nil, err
	})
}

// StartThanks drafts a thank-you note; details says who the owner met and what they talked about.
func (d *Drafts) StartThanks(ctx context.Context, p *Pursuit, details string, done Done) (Draft, error) {
	if strings.TrimSpace(details) == "" {
		return Draft{}, errors.New("say who you met and what you talked about")
	}
	job, err := d.Pipeline.JobFor(ctx, p)
	if err != nil {
		return Draft{}, err
	}
	return d.start(ctx, "thanks", "Thank-you: "+p.Title+" · "+p.Company, p, p.ItemID, done, func(ctx context.Context) (string, string, []byte, error) {
		text, err := d.Kit.Thanks(ctx, job, details)
		return text, "", nil, err
	})
}

// start records a pending draft, then writes it in the background under the app's lifetime.
func (d *Drafts) start(ctx context.Context, kind, title string, p *Pursuit, itemID *int64, done Done,
	write func(ctx context.Context) (text, fileName string, file []byte, err error)) (Draft, error) {
	if d.State != nil && d.State.Paused() {
		return Draft{}, ErrPaused
	}
	var pursuitID any
	if p != nil {
		pursuitID = p.ID
	}
	var item any
	if itemID != nil {
		item = *itemID
	}
	res, err := d.Pipeline.DB.ExecContext(ctx, `INSERT INTO "Drafts" ("PursuitId", "ItemId", "Kind", "Title", "Status", "CreatedAt")
		VALUES (?, ?, ?, ?, 'pending', ?)`, pursuitID, item, kind, title, store.FormatTime(d.Pipeline.Clock.Now()))
	if err != nil {
		return Draft{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Draft{}, err
	}
	draft, err := d.Get(ctx, id)
	if err != nil {
		return Draft{}, err
	}

	d.Tasks.Go(kind, func(ctx context.Context) error {
		text, fileName, file, werr := write(ctx)
		if ctx.Err() != nil {
			return nil // shutting down; Recover marks it failed on the next start
		}
		finished := store.FormatTime(d.Pipeline.Clock.Now())
		var err error
		if werr != nil {
			_, err = d.Pipeline.DB.ExecContext(ctx, `UPDATE "Drafts" SET "Status" = 'failed', "Error" = ?, "FinishedAt" = ? WHERE "Id" = ?`,
				werr.Error(), finished, id)
		} else {
			var name any
			if file != nil {
				name = fileName
			}
			_, err = d.Pipeline.DB.ExecContext(ctx, `UPDATE "Drafts" SET "Status" = 'ready', "Text" = ?, "FileName" = ?, "File" = ?, "FinishedAt" = ?
				WHERE "Id" = ?`, text, name, file, finished, id)
		}
		if err != nil {
			return err
		}
		if done == nil {
			return nil
		}
		finishedDraft, err := d.Get(ctx, id)
		if err != nil {
			return err
		}
		return done(ctx, finishedDraft, file)
	})
	return draft, nil
}

const draftColumns = `"Id", "PursuitId", "ItemId", "Kind", "Title", "Status", "Text", "Error", "FileName", "CreatedAt", "FinishedAt"`

func (d *Drafts) query(ctx context.Context, where string, args ...any) ([]Draft, error) {
	rows, err := d.Pipeline.DB.QueryContext(ctx, `SELECT `+draftColumns+` FROM "Drafts" WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Draft
	for rows.Next() {
		var (
			dr                Draft
			pursuitID, itemID sql.NullInt64
			errText, fileName sql.NullString
			created           string
			finished          sql.NullString
		)
		if err := rows.Scan(&dr.ID, &pursuitID, &itemID, &dr.Kind, &dr.Title, &dr.Status, &dr.Text, &errText, &fileName, &created, &finished); err != nil {
			return nil, err
		}
		if pursuitID.Valid {
			dr.PursuitID = &pursuitID.Int64
		}
		if itemID.Valid {
			dr.ItemID = &itemID.Int64
		}
		dr.Error, dr.FileName = errText.String, fileName.String
		if dr.CreatedAt, err = store.ParseTime(created); err != nil {
			return nil, err
		}
		if dr.FinishedAt, err = store.ParseTimePtr(finished); err != nil {
			return nil, err
		}
		out = append(out, dr)
	}
	return out, rows.Err()
}

func (d *Drafts) Get(ctx context.Context, id int64) (Draft, error) {
	found, err := d.query(ctx, `"Id" = ?`, id)
	if err != nil {
		return Draft{}, err
	}
	if len(found) == 0 {
		return Draft{}, ErrNotFound
	}
	return found[0], nil
}

// ForPursuit lists a pursuit's drafts (and those written for its item before it was activated), newest first.
func (d *Drafts) ForPursuit(ctx context.Context, p *Pursuit) ([]Draft, error) {
	if p.ItemID != nil {
		return d.query(ctx, `"PursuitId" = ? OR ("PursuitId" IS NULL AND "ItemId" = ?) ORDER BY "Id" DESC`, p.ID, *p.ItemID)
	}
	return d.query(ctx, `"PursuitId" = ? ORDER BY "Id" DESC`, p.ID)
}

// File returns a draft's attachment.
func (d *Drafts) File(ctx context.Context, id int64) (string, []byte, error) {
	var name sql.NullString
	var file []byte
	err := d.Pipeline.DB.QueryRowContext(ctx, `SELECT "FileName", "File" FROM "Drafts" WHERE "Id" = ?`, id).Scan(&name, &file)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && file == nil) {
		return "", nil, ErrNotFound
	}
	return name.String, file, err
}

// Recover fails drafts left pending by a previous run that stopped mid-write.
func (d *Drafts) Recover(ctx context.Context) error {
	_, err := d.Pipeline.DB.ExecContext(ctx, `UPDATE "Drafts" SET "Status" = 'failed', "Error" = 'interrupted by a restart', "FinishedAt" = ?
		WHERE "Status" = 'pending'`, store.FormatTime(d.Pipeline.Clock.Now()))
	return err
}

// JobFor is the posting behind a pursuit: its radar item when it has one, else what was entered by hand.
func (pl *Pipeline) JobFor(ctx context.Context, p *Pursuit) (kit.Job, error) {
	if p.ItemID != nil {
		job, err := pl.ItemJob(ctx, *p.ItemID)
		if err == nil {
			return job, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return kit.Job{}, err
		}
	}
	return kit.Job{Company: p.Company, Title: p.Title, URL: p.URL}, nil
}

func (pl *Pipeline) ItemJob(ctx context.Context, id int64) (kit.Job, error) {
	var j kit.Job
	err := pl.DB.QueryRowContext(ctx, `SELECT "Company", "Title", "Url", "Locations", "Description" FROM "RadarItems" WHERE "Id" = ?`, id).
		Scan(&j.Company, &j.Title, &j.URL, &j.Locations, &j.Description)
	if errors.Is(err, sql.ErrNoRows) {
		return j, fmt.Errorf("item #%d: %w", id, ErrNotFound)
	}
	return j, err
}
