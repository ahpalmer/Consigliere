package radar

import (
	"database/sql"
	"time"

	"github.com/ahpalmer/consigliere/internal/store"
)

// ItemView is a RadarItems row without its (large) description, for listing and ranking.
type ItemView struct {
	ID                              int64
	Source, Board, Company, Title   string
	Locations, URL                  string
	CompMin, CompMax                *float64
	PostedAt                        *time.Time
	FirstSeenAt                     time.Time
	Tier, Category                  string
	FitScore                        *int
	FitSummary, FitReasons, FitGaps string
	Feedback                        *int
}

const itemViewColumns = `"Id", "Source", "Board", "Company", "Title", "Locations", "Url", "CompMin", "CompMax", "PostedAt", "FirstSeenAt",
	"Tier", "Category", "FitScore", "FitSummary", "FitReasons", "FitGaps", "Feedback"`

type rowScanner interface{ Scan(dest ...any) error }

func scanItemView(r rowScanner) (ItemView, error) {
	var (
		v                                      ItemView
		compMin, compMax                       sql.NullFloat64
		posted                                 sql.NullString
		firstSeen                              string
		tier, category, summary, reasons, gaps sql.NullString
		fit, feedback                          sql.NullInt64
	)
	if err := r.Scan(&v.ID, &v.Source, &v.Board, &v.Company, &v.Title, &v.Locations, &v.URL, &compMin, &compMax, &posted, &firstSeen,
		&tier, &category, &fit, &summary, &reasons, &gaps, &feedback); err != nil {
		return v, err
	}
	var err error
	if v.PostedAt, err = store.ParseTimePtr(posted); err != nil {
		return v, err
	}
	if v.FirstSeenAt, err = store.ParseTime(firstSeen); err != nil {
		return v, err
	}
	v.CompMin, v.CompMax = floatPtr(compMin), floatPtr(compMax)
	v.FitScore, v.Feedback = intPtr(fit), intPtr(feedback)
	v.Tier, v.Category, v.FitSummary, v.FitReasons, v.FitGaps = tier.String, category.String, summary.String, reasons.String, gaps.String
	return v, nil
}

func scanItemViews(rows *sql.Rows, err error) ([]ItemView, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ItemView
	for rows.Next() {
		v, err := scanItemView(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func floatPtr(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}

func intPtr(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullFloat(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// staleClause filters out items posted before cutoff (nil cutoff: no limit). Items without a date are kept.
func staleClause(cutoff *time.Time) (string, []any) {
	if cutoff == nil {
		return "", nil
	}
	return ` AND ("PostedAt" IS NULL OR "PostedAt" >= ?)`, []any{store.FormatTime(*cutoff)}
}
