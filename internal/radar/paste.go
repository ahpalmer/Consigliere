package radar

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/ahpalmer/consigliere/internal/store"
)

// MaxPastedChars caps a pasted posting; the scorer reads only the first maxDescriptionChars of it anyway.
const MaxPastedChars = 50_000

// ErrPaused: the kill switch is on, so nothing that spends is started.
var ErrPaused = errors.New("paused; resume first")

// Paste puts the full posting the owner pasted in place of what the source gave (an alert email carries only a
// snippet), takes the pay range from it when one is listed, and re-scores the item at once. The source reporting the
// item again never overwrites the pasted text.
func (r *ScoringRunner) Paste(ctx context.Context, id int64, text string) error {
	text = strings.TrimSpace(text)
	if r.State.Paused() {
		return ErrPaused
	}
	sc, err := r.Scorer.BuildContext()
	if err != nil {
		return err
	}
	var title string
	err = r.DB.QueryRowContext(ctx, `SELECT "Title" FROM "RadarItems" WHERE "Id" = ?`, id).Scan(&title)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	sets := `"Description" = ?, "ContentHash" = ?, "PastedAt" = ?`
	args := []any{text, Hash(title, text), store.FormatTime(r.Clock.Now())}
	// A range in the posting beats the alert's (often missing) one; with none, the alert's stays.
	if lo, hi, currency := payPtrs(ExtractPayRange(text)); lo != nil {
		sets += `, "CompMin" = ?, "CompMax" = ?, "CompCurrency" = ?`
		args = append(args, *lo, *hi, currency)
	}
	if _, err := r.DB.ExecContext(ctx, `UPDATE "RadarItems" SET `+sets+` WHERE "Id" = ?`, append(args, id)...); err != nil {
		return err
	}
	_, err = r.scoreOne(ctx, id, sc, sc.Key())
	return err
}
