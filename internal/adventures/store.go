// Package adventures is the Adventures section (M13): an idea bank, adventures planned from it, ideas generated from
// constraints and past ratings, and Telegram reminders before and a rating after. A bot has the section when its data
// pack has adventures/settings.yaml.
package adventures

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/datapack"
	"github.com/ahpalmer/consigliere/internal/store"
	"gopkg.in/yaml.v3"
)

var ErrNotFound = errors.New("not found")

// InvalidError is input the owner can fix.
type InvalidError struct{ msg string }

func (e *InvalidError) Error() string { return e.msg }

func invalid(format string, args ...any) error { return &InvalidError{fmt.Sprintf(format, args...)} }

// SettingsPath is the file in the data pack that turns the section on.
const SettingsPath = "adventures/settings.yaml"

// Settings are adventures/settings.yaml.
type Settings struct {
	// Nudge when nothing's planned and the last adventure was this many days ago. 0: 14.
	CadenceDays int `yaml:"cadence_days"`
	// Where you start from, for generated ideas ("Raleigh, NC").
	Home string `yaml:"home"`
	// Who you go with, if anyone, for generated ideas.
	With string `yaml:"with"`
	// What you like and what to avoid, in your own words, for generated ideas.
	Likes string `yaml:"likes"`
}

func (s Settings) Cadence() int {
	if s.CadenceDays <= 0 {
		return 14
	}
	return s.CadenceDays
}

// LoadSettings reads the settings; ok=false when the pack has none (the section is off).
func LoadSettings(pack *datapack.Pack) (Settings, bool, error) {
	var s Settings
	if pack == nil {
		return s, false, nil
	}
	text, found, err := pack.ReadText(SettingsPath)
	if err != nil || !found {
		return s, false, err
	}
	if err := yaml.Unmarshal([]byte(text), &s); err != nil {
		return s, false, fmt.Errorf("%s: %w", SettingsPath, err)
	}
	return s, true, nil
}

// Idea is one entry in the idea bank.
type Idea struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Place       string    `json:"place"`
	Cost        string    `json:"cost"`
	Duration    string    `json:"duration"`
	Tags        []string  `json:"tags"`
	Source      string    `json:"source"`
	Favorite    bool      `json:"favorite"`
	CreatedAt   time.Time `json:"createdAt"`
	// How it went the times it was done: count and average stars (0 when never rated).
	Done      int     `json:"done"`
	AvgRating float64 `json:"avgRating"`
}

// Adventure is one planned or past adventure.
type Adventure struct {
	ID          int64      `json:"id"`
	IdeaID      *int64     `json:"ideaId"`
	Title       string     `json:"title"`
	Place       string     `json:"place"`
	Notes       string     `json:"notes"`
	Date        string     `json:"date"`
	Time        string     `json:"time"`
	PrepNote    string     `json:"prepNote"`
	PrepDue     string     `json:"prepDue"`
	Status      string     `json:"status"`
	Rating      *int       `json:"rating"`
	Review      string     `json:"review"`
	CreatedAt   time.Time  `json:"createdAt"`
	CompletedAt *time.Time `json:"completedAt"`
}

const (
	StatusPlanned = "planned"
	StatusDone    = "done"
	StatusSkipped = "skipped"
)

var costs = []string{"", "$", "$$", "$$$", "$$$$"}

// Store keeps ideas and adventures. Days are the owner's (Location).
type Store struct {
	DB       *sql.DB
	Clock    clock.Clock
	Location *time.Location
}

// Today is the owner's date.
func (s *Store) Today() string { return s.Clock.Now().In(s.Location).Format(time.DateOnly) }

func (s *Store) now() string { return store.FormatTime(s.Clock.Now()) }

// --- ideas ---

func (i *Idea) clean() error {
	i.Title, i.Description, i.Place, i.Duration = strings.TrimSpace(i.Title), strings.TrimSpace(i.Description), strings.TrimSpace(i.Place), strings.TrimSpace(i.Duration)
	i.Cost = strings.TrimSpace(i.Cost)
	if i.Title == "" {
		return invalid("give the idea a title")
	}
	if !slices.Contains(costs, i.Cost) {
		return invalid("cost is $ to $$$$, or blank")
	}
	var tags []string
	for _, t := range i.Tags {
		if t = strings.ToLower(strings.TrimSpace(t)); t != "" && !slices.Contains(tags, t) {
			tags = append(tags, t)
		}
	}
	i.Tags = tags
	if i.Source != "generated" {
		i.Source = "manual"
	}
	return nil
}

const ideaColumns = `i."Id", i."Title", i."Description", i."Place", i."Cost", i."Duration", i."Tags", i."Source", i."Favorite", i."CreatedAt",
	(SELECT count(*) FROM "Adventures" a WHERE a."IdeaId" = i."Id" AND a."Status" = 'done'),
	coalesce((SELECT avg(a."Rating") FROM "Adventures" a WHERE a."IdeaId" = i."Id" AND a."Rating" IS NOT NULL), 0)`

func scanIdea(r interface{ Scan(...any) error }) (*Idea, error) {
	var i Idea
	var tags, created string
	if err := r.Scan(&i.ID, &i.Title, &i.Description, &i.Place, &i.Cost, &i.Duration, &tags, &i.Source, &i.Favorite, &created, &i.Done, &i.AvgRating); err != nil {
		return nil, err
	}
	i.Tags = []string{}
	if tags != "" {
		i.Tags = strings.Split(tags, ",")
	}
	var err error
	i.CreatedAt, err = store.ParseTime(created)
	return &i, err
}

func (s *Store) AddIdea(ctx context.Context, i Idea) (*Idea, error) {
	if err := i.clean(); err != nil {
		return nil, err
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO "AdventureIdeas" ("Title", "Description", "Place", "Cost", "Duration", "Tags", "Source", "Favorite", "CreatedAt")
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, i.Title, i.Description, i.Place, i.Cost, i.Duration, strings.Join(i.Tags, ","), i.Source, i.Favorite, s.now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.Idea(ctx, id)
}

func (s *Store) Idea(ctx context.Context, id int64) (*Idea, error) {
	i, err := scanIdea(s.DB.QueryRowContext(ctx, `SELECT `+ideaColumns+` FROM "AdventureIdeas" i WHERE i."Id" = ? AND i."ArchivedAt" IS NULL`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return i, err
}

// UpdateIdea replaces an idea's fields (not its source or when it was added).
func (s *Store) UpdateIdea(ctx context.Context, id int64, i Idea) (*Idea, error) {
	if err := i.clean(); err != nil {
		return nil, err
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE "AdventureIdeas" SET "Title" = ?, "Description" = ?, "Place" = ?, "Cost" = ?, "Duration" = ?, "Tags" = ?,
		"Favorite" = ? WHERE "Id" = ? AND "ArchivedAt" IS NULL`, i.Title, i.Description, i.Place, i.Cost, i.Duration, strings.Join(i.Tags, ","), i.Favorite, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.Idea(ctx, id)
}

func (s *Store) SetFavorite(ctx context.Context, id int64, favorite bool) (*Idea, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE "AdventureIdeas" SET "Favorite" = ? WHERE "Id" = ? AND "ArchivedAt" IS NULL`, favorite, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.Idea(ctx, id)
}

// ArchiveIdea takes an idea out of the bank; adventures planned from it keep their own copy of its details.
func (s *Store) ArchiveIdea(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE "AdventureIdeas" SET "ArchivedAt" = ? WHERE "Id" = ? AND "ArchivedAt" IS NULL`, s.now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Ideas lists the bank: favorites first, then the best rated, then the newest. Search matches title, description,
// place and tags.
func (s *Store) Ideas(ctx context.Context, search string, favoritesOnly bool) ([]*Idea, error) {
	where, args := `i."ArchivedAt" IS NULL`, []any{}
	if favoritesOnly {
		where += ` AND i."Favorite" = 1`
	}
	if q := strings.TrimSpace(search); q != "" {
		where += ` AND (i."Title" LIKE ? OR i."Description" LIKE ? OR i."Place" LIKE ? OR i."Tags" LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like, like)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+ideaColumns+` FROM "AdventureIdeas" i WHERE `+where+` ORDER BY i."Favorite" DESC, 12 DESC, i."Id" DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Idea{}
	for rows.Next() {
		i, err := scanIdea(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// --- adventures ---

func (a *Adventure) clean() error {
	a.Title, a.Place, a.Notes = strings.TrimSpace(a.Title), strings.TrimSpace(a.Place), strings.TrimSpace(a.Notes)
	a.Date, a.Time = strings.TrimSpace(a.Date), strings.TrimSpace(a.Time)
	a.PrepNote, a.PrepDue = strings.TrimSpace(a.PrepNote), strings.TrimSpace(a.PrepDue)
	if a.Title == "" {
		return invalid("give the adventure a title")
	}
	if _, err := time.Parse(time.DateOnly, a.Date); err != nil {
		return invalid("give the day as 2026-12-05")
	}
	if a.Time != "" {
		if _, err := time.Parse("15:04", a.Time); err != nil {
			return invalid("give the time as 19:30, or leave it blank")
		}
	}
	if a.PrepDue != "" {
		if _, err := time.Parse(time.DateOnly, a.PrepDue); err != nil {
			return invalid("give the prep day as 2026-12-01, or leave it blank")
		}
		if a.PrepDue > a.Date {
			return invalid("prep is due after the adventure")
		}
		if a.PrepNote == "" {
			return invalid("say what to prep, or clear its day")
		}
	}
	return nil
}

const adventureColumns = `"Id", "IdeaId", "Title", "Place", "Notes", "Date", "Time", "PrepNote", "PrepDue", "Status", "Rating", "Review", "CreatedAt", "CompletedAt"`

func scanAdventure(r interface{ Scan(...any) error }) (*Adventure, error) {
	var a Adventure
	var idea, rating sql.NullInt64
	var created string
	var completed sql.NullString
	if err := r.Scan(&a.ID, &idea, &a.Title, &a.Place, &a.Notes, &a.Date, &a.Time, &a.PrepNote, &a.PrepDue, &a.Status, &rating, &a.Review, &created, &completed); err != nil {
		return nil, err
	}
	if idea.Valid {
		a.IdeaID = &idea.Int64
	}
	if rating.Valid {
		n := int(rating.Int64)
		a.Rating = &n
	}
	var err error
	if a.CreatedAt, err = store.ParseTime(created); err != nil {
		return nil, err
	}
	a.CompletedAt, err = store.ParseTimePtr(completed)
	return &a, err
}

// Plan adds an adventure. From an idea, blank fields are filled from it.
func (s *Store) Plan(ctx context.Context, a Adventure) (*Adventure, error) {
	if a.IdeaID != nil {
		idea, err := s.Idea(ctx, *a.IdeaID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(a.Title) == "" {
			a.Title = idea.Title
		}
		if strings.TrimSpace(a.Place) == "" {
			a.Place = idea.Place
		}
		if strings.TrimSpace(a.Notes) == "" {
			a.Notes = idea.Description
		}
	}
	if err := a.clean(); err != nil {
		return nil, err
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO "Adventures" ("IdeaId", "Title", "Place", "Notes", "Date", "Time", "PrepNote", "PrepDue", "CreatedAt")
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, a.IdeaID, a.Title, a.Place, a.Notes, a.Date, a.Time, a.PrepNote, a.PrepDue, s.now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.Adventure(ctx, id)
}

func (s *Store) Adventure(ctx context.Context, id int64) (*Adventure, error) {
	a, err := scanAdventure(s.DB.QueryRowContext(ctx, `SELECT `+adventureColumns+` FROM "Adventures" WHERE "Id" = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

// Update changes a planned adventure's details. A new day or prep day sends its reminders again.
func (s *Store) Update(ctx context.Context, id int64, a Adventure) (*Adventure, error) {
	if err := a.clean(); err != nil {
		return nil, err
	}
	old, err := s.Adventure(ctx, id)
	if err != nil {
		return nil, err
	}
	if old.Status != StatusPlanned {
		return nil, invalid("only a planned adventure can be changed")
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE "Adventures" SET "Title" = ?, "Place" = ?, "Notes" = ?, "Date" = ?, "Time" = ?, "PrepNote" = ?, "PrepDue" = ?,
		"DayBeforeRemindedOn" = CASE WHEN "Date" = ? THEN "DayBeforeRemindedOn" END,
		"DayOfRemindedOn" = CASE WHEN "Date" = ? THEN "DayOfRemindedOn" END,
		"RatingAskedOn" = CASE WHEN "Date" = ? THEN "RatingAskedOn" END,
		"PrepRemindedOn" = CASE WHEN "PrepDue" = ? THEN "PrepRemindedOn" END
		WHERE "Id" = ?`, a.Title, a.Place, a.Notes, a.Date, a.Time, a.PrepNote, a.PrepDue, a.Date, a.Date, a.Date, a.PrepDue, id)
	if err != nil {
		return nil, err
	}
	return s.Adventure(ctx, id)
}

// Rate records how it went (1-5 stars) and marks it done; rating again replaces the rating.
func (s *Store) Rate(ctx context.Context, id int64, rating int, review string) (*Adventure, error) {
	if rating < 1 || rating > 5 {
		return nil, invalid("rate it 1 to 5")
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE "Adventures" SET "Status" = 'done', "Rating" = ?, "Review" = CASE WHEN ? <> '' THEN ? ELSE "Review" END,
		"CompletedAt" = coalesce("CompletedAt", ?) WHERE "Id" = ? AND "Status" <> 'skipped'`, rating, strings.TrimSpace(review), strings.TrimSpace(review), s.now(), id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.Adventure(ctx, id); err != nil {
			return nil, err
		}
		return nil, invalid("it was skipped; plan it again instead")
	}
	return s.Adventure(ctx, id)
}

// Skip marks a planned adventure as not happening; its reminders stop.
func (s *Store) Skip(ctx context.Context, id int64) (*Adventure, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE "Adventures" SET "Status" = 'skipped', "CompletedAt" = ? WHERE "Id" = ? AND "Status" = 'planned'`, s.now(), id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.Adventure(ctx, id); err != nil {
			return nil, err
		}
		return nil, invalid("only a planned adventure can be skipped")
	}
	return s.Adventure(ctx, id)
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM "Adventures" WHERE "Id" = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Upcoming are planned adventures, soonest first, including past ones still waiting for a rating.
func (s *Store) Upcoming(ctx context.Context) ([]*Adventure, error) {
	return s.list(ctx, `"Status" = 'planned' ORDER BY "Date", "Time", "Id"`)
}

// History is everything done or skipped, newest first.
func (s *Store) History(ctx context.Context) ([]*Adventure, error) {
	return s.list(ctx, `"Status" <> 'planned' ORDER BY "Date" DESC, "Id" DESC`)
}

func (s *Store) list(ctx context.Context, where string, args ...any) ([]*Adventure, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+adventureColumns+` FROM "Adventures" WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Adventure{}
	for rows.Next() {
		a, err := scanAdventure(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
