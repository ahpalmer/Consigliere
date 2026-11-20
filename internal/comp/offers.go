package comp

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/store"
)

var ErrNotFound = errors.New("not found")

// Offer is an incoming package pasted in for a pursuit: the text as pasted and the package read from it, as YAML.
type Offer struct {
	ID, PursuitID        int64
	Text, Package        string
	CreatedAt, UpdatedAt time.Time
}

// Offers keeps pasted offers in the shared database.
type Offers struct {
	DB    *sql.DB
	Clock clock.Clock
}

const offerColumns = `"Id", "PursuitId", "Text", "Package", "CreatedAt", "UpdatedAt"`

func (o *Offers) Add(ctx context.Context, pursuitID int64, text string, p *Package) (*Offer, error) {
	now := store.FormatTime(o.Clock.Now())
	res, err := o.DB.ExecContext(ctx, `INSERT INTO "Offers" ("PursuitId", "Text", "Package", "CreatedAt", "UpdatedAt") VALUES (?, ?, ?, ?, ?)`,
		pursuitID, text, p.YAML(), now, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return o.Get(ctx, id)
}

func (o *Offers) Get(ctx context.Context, id int64) (*Offer, error) {
	list, err := o.query(ctx, `"Id" = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return list[0], nil
}

// List is a pursuit's offers, newest first.
func (o *Offers) List(ctx context.Context, pursuitID int64) ([]*Offer, error) {
	return o.query(ctx, `"PursuitId" = ? ORDER BY "Id" DESC`, pursuitID)
}

// Update replaces an offer's package with the owner's corrected YAML, refusing YAML that doesn't parse or check out.
func (o *Offers) Update(ctx context.Context, id int64, yamlText string) (*Offer, error) {
	if _, err := Parse(yamlText); err != nil {
		return nil, &InvalidError{err}
	}
	res, err := o.DB.ExecContext(ctx, `UPDATE "Offers" SET "Package" = ?, "UpdatedAt" = ? WHERE "Id" = ?`,
		yamlText, store.FormatTime(o.Clock.Now()), id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return o.Get(ctx, id)
}

func (o *Offers) Delete(ctx context.Context, id int64) error {
	res, err := o.DB.ExecContext(ctx, `DELETE FROM "Offers" WHERE "Id" = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (o *Offers) query(ctx context.Context, where string, args ...any) ([]*Offer, error) {
	rows, err := o.DB.QueryContext(ctx, `SELECT `+offerColumns+` FROM "Offers" WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Offer
	for rows.Next() {
		var (
			of               Offer
			created, updated string
		)
		if err := rows.Scan(&of.ID, &of.PursuitID, &of.Text, &of.Package, &created, &updated); err != nil {
			return nil, err
		}
		if of.CreatedAt, err = store.ParseTime(created); err != nil {
			return nil, err
		}
		if of.UpdatedAt, err = store.ParseTime(updated); err != nil {
			return nil, err
		}
		out = append(out, &of)
	}
	return out, rows.Err()
}

// InvalidError is a package the owner wrote that doesn't parse or check out; its message says what to fix.
type InvalidError struct{ Err error }

func (e *InvalidError) Error() string { return e.Err.Error() }
func (e *InvalidError) Unwrap() error { return e.Err }
