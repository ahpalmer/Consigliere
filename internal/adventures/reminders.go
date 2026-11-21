package adventures

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ahpalmer/consigliere/internal/channel"
	"github.com/ahpalmer/consigliere/internal/datapack"
	"github.com/ahpalmer/consigliere/internal/store"
)

// Reminders is the daily job: prep that's due, the day before, the day of, a rating the day after, and a nudge when
// nothing's planned and the last adventure was a while ago. Each reminder goes once. Nothing runs without settings.
type Reminders struct {
	Store   *Store
	Pack    *datapack.Pack
	Channel channel.Channel
	Cron    string
}

func (r *Reminders) Name() string     { return "adventures" }
func (r *Reminders) Schedule() string { return r.Cron }

const nudgeKey = "adventures_nudged_on"

func (r *Reminders) Run(ctx context.Context) error {
	settings, ok, err := LoadSettings(r.Pack)
	if err != nil || !ok {
		return err
	}
	s := r.Store
	today := s.Today()
	tomorrow := day(today, 1)

	steps := []struct {
		where, column string
		args          []any
		text          func(a *Adventure) string
		buttons       func(a *Adventure) []channel.Button
	}{
		{`"PrepDue" <> '' AND "PrepDue" <= ? AND "Date" >= ?`, "PrepRemindedOn", []any{today, today},
			func(a *Adventure) string {
				return fmt.Sprintf("📝 Prep for %s (%s): %s", a.Title, When(a, today), a.PrepNote)
			}, nil},
		{`"Date" = ?`, "DayBeforeRemindedOn", []any{tomorrow}, func(a *Adventure) string { return "🗓 Tomorrow: " + Describe(a, today) }, nil},
		{`"Date" = ?`, "DayOfRemindedOn", []any{today}, func(a *Adventure) string { return "✨ Today: " + Describe(a, today) }, nil},
		{`"Date" < ?`, "RatingAskedOn", []any{today}, func(a *Adventure) string { return fmt.Sprintf("How was %s?", a.Title) }, RatingButtons},
	}
	for _, step := range steps {
		due, err := s.list(ctx, `"Status" = 'planned' AND "`+step.column+`" IS NULL AND `+step.where+` ORDER BY "Date", "Time"`, step.args...)
		if err != nil {
			return err
		}
		for _, a := range due {
			var buttons []channel.Button
			if step.buttons != nil {
				buttons = step.buttons(a)
			}
			if err := r.Channel.Notify(ctx, step.text(a), buttons...); err != nil {
				return err
			}
			if _, err := s.DB.ExecContext(ctx, `UPDATE "Adventures" SET "`+step.column+`" = ? WHERE "Id" = ?`, today, a.ID); err != nil {
				return err
			}
		}
	}
	return r.nudge(ctx, settings, today)
}

// nudge suggests planning something when nothing's planned and the last adventure (done, or past and waiting for its
// rating) or the last nudge was at least the cadence ago.
func (r *Reminders) nudge(ctx context.Context, settings Settings, today string) error {
	s := r.Store
	var planned int
	var last string
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM "Adventures" WHERE "Status" = 'planned' AND "Date" >= ?`, today).Scan(&planned); err != nil {
		return err
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT coalesce(max("Date"), '') FROM "Adventures" WHERE "Status" <> 'skipped' AND "Date" <= ?`, today).Scan(&last); err != nil {
		return err
	}
	nudged, _, err := store.GetState(ctx, s.DB, nudgeKey)
	if err != nil {
		return err
	}
	cadence := settings.Cadence()
	if planned > 0 || (last != "" && daysBetween(last, today) < cadence) || (nudged != "" && daysBetween(nudged, today) < cadence) {
		return nil
	}
	text := "🧭 Nothing's planned. Time for an adventure?"
	if last != "" {
		text = fmt.Sprintf("🧭 It's been %d days since your last adventure, and nothing's planned.", daysBetween(last, today))
	}
	ideas, err := s.Ideas(ctx, "", false)
	if err != nil {
		return err
	}
	if len(ideas) > 0 {
		var picks []string
		for _, i := range ideas[:min(len(ideas), 3)] {
			picks = append(picks, "• "+i.Title)
		}
		text += " From your ideas:\n" + strings.Join(picks, "\n")
	} else {
		text += " Add some ideas, or generate a few, on the dashboard's Adventures page."
	}
	if err := r.Channel.Notify(ctx, text); err != nil {
		return err
	}
	return store.SetState(ctx, s.DB, nudgeKey, today)
}

// --- rating buttons ---

const callbackPrefix = "adv:"

// RatingButtons are ⭐1-5 and "didn't happen" under the day-after question.
func RatingButtons(a *Adventure) []channel.Button {
	var out []channel.Button
	for n := 1; n <= 5; n++ {
		out = append(out, channel.Button{Label: strconv.Itoa(n) + "⭐", Data: fmt.Sprintf("%sr:%d:%d", callbackPrefix, a.ID, n)})
	}
	return append(out, channel.Button{Label: "Didn't happen", Data: fmt.Sprintf("%sx:%d", callbackPrefix, a.ID), Row: 1})
}

// Callbacks handles the rating buttons.
type Callbacks struct{ Store *Store }

func (c *Callbacks) Prefix() string { return callbackPrefix }

func (c *Callbacks) HandleCallback(ctx context.Context, data string) (string, error) {
	parts := strings.Split(strings.TrimPrefix(data, callbackPrefix), ":")
	if len(parts) < 2 {
		return "Unrecognized button.", nil
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "Unrecognized button.", nil
	}
	var a *Adventure
	switch {
	case parts[0] == "r" && len(parts) == 3:
		n, _ := strconv.Atoi(parts[2])
		a, err = c.Store.Rate(ctx, id, n, "")
	case parts[0] == "x":
		a, err = c.Store.Skip(ctx, id)
	default:
		return "Unrecognized button.", nil
	}
	var bad *InvalidError
	switch {
	case errors.Is(err, ErrNotFound):
		return "That adventure is gone.", nil
	case errors.As(err, &bad):
		return bad.Error(), nil
	case err != nil:
		return "", err
	case a.Status == StatusSkipped:
		return "Marked as didn't happen.", nil
	}
	return fmt.Sprintf("Rated %s %d⭐. Add a note on the dashboard if you like.", a.Title, *a.Rating), nil
}

// --- formatting ---

// When is a short "today", "tomorrow at 7:30pm" or "Sat Dec 5" for an adventure.
func When(a *Adventure, today string) string {
	var out string
	switch a.Date {
	case today:
		out = "today"
	case day(today, 1):
		out = "tomorrow"
	default:
		d, _ := time.Parse(time.DateOnly, a.Date)
		out = d.Format("Mon Jan 2")
	}
	if a.Time != "" {
		t, _ := time.Parse("15:04", a.Time)
		out += " at " + strings.ToLower(strings.Replace(t.Format("3:04PM"), ":00", "", 1))
	}
	return out
}

// Describe is one adventure for a message: title, when, where, notes.
func Describe(a *Adventure, today string) string {
	out := a.Title + ", " + When(a, today)
	if a.Place != "" {
		out += ", " + a.Place
	}
	if a.Notes != "" {
		out += "\n" + a.Notes
	}
	return out
}

func day(date string, add int) string {
	d, _ := time.Parse(time.DateOnly, date)
	return d.AddDate(0, 0, add).Format(time.DateOnly)
}

func daysBetween(from, to string) int {
	a, _ := time.Parse(time.DateOnly, from)
	b, _ := time.Parse(time.DateOnly, to)
	return int(b.Sub(a).Hours() / 24)
}
