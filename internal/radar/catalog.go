package radar

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/store"
)

var ErrNotFound = errors.New("not found")

// Catalog is read-only browsing of scored items, for the dashboard. Unlike the digest it never marks anything sent.
type Catalog struct {
	DB       *sql.DB
	Ranking  RankingFactory
	Config   ConfigLoader
	Clock    clock.Clock
	MinScore int // default floor, the digest's
}

// Option is a ranked item with what the ranking knows about it.
type Option struct {
	ItemView
	RankScore int
	Priority  Priority
	Sent      bool // already shown in an alert or digest
}

type OptionsQuery struct {
	Search         string // matched against title, company and locations
	Tier, Category string
	MinScore       *int // nil: the catalog's default
	Limit          int
}

type OptionsPage struct {
	Options []Option
	Total   int // matches before Limit
	// For filter pickers, in preferences order.
	Tiers, Categories []string
	Unscored          int // passed the filter, still waiting for a score
}

func (c *Catalog) Options(ctx context.Context, q OptionsQuery) (OptionsPage, error) {
	ranking, err := c.Ranking.Create()
	if err != nil {
		return OptionsPage{}, err
	}
	stale, args := staleClause(ranking.StaleBefore(c.Clock.Now()))
	base := `"Passed" = 1 AND "DuplicateOfId" IS NULL` + stale
	page := OptionsPage{}
	if err := c.DB.QueryRowContext(ctx, `SELECT count(*) FROM "RadarItems" WHERE `+base+` AND "FitScore" IS NULL AND "ScoredAt" IS NULL`, args...).
		Scan(&page.Unscored); err != nil {
		return OptionsPage{}, err
	}

	where := base + ` AND "FitScore" IS NOT NULL`
	if s := strings.TrimSpace(q.Search); s != "" {
		like := "%" + strings.ToLower(s) + "%"
		where += ` AND (lower("Title") LIKE ? OR lower("Company") LIKE ? OR lower("Locations") LIKE ?)`
		args = append(args, like, like, like)
	}
	if q.Tier != "" {
		where += ` AND lower(coalesce("Tier", '')) = ?`
		args = append(args, strings.ToLower(q.Tier))
	}
	if q.Category != "" {
		where += ` AND lower(coalesce("Category", '')) = ?`
		args = append(args, strings.ToLower(q.Category))
	}
	rows, err := c.DB.QueryContext(ctx, `SELECT `+itemViewColumns+`, "DigestedAt" IS NOT NULL FROM "RadarItems" WHERE `+where, args...)
	if err != nil {
		return OptionsPage{}, err
	}
	var items []ItemView
	sent := map[int64]bool{}
	for rows.Next() {
		var wasSent bool
		v, err := scanItemView(scanTail{rows, []any{&wasSent}})
		if err != nil {
			rows.Close()
			return OptionsPage{}, err
		}
		sent[v.ID] = wasSent
		items = append(items, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return OptionsPage{}, err
	}

	floor := c.MinScore
	if q.MinScore != nil {
		floor = *q.MinScore
	}
	var kept []ItemView
	for _, i := range items {
		if ranking.RankScore(i) >= floor {
			kept = append(kept, i)
		}
	}
	ranking.Sort(kept)
	page.Total = len(kept)
	if q.Limit > 0 && len(kept) > q.Limit {
		kept = kept[:q.Limit]
	}
	for _, i := range kept {
		page.Options = append(page.Options, Option{ItemView: i, RankScore: ranking.RankScore(i), Priority: ranking.PriorityOf(i), Sent: sent[i.ID]})
	}
	for _, t := range ranking.Prefs.Tiers {
		page.Tiers = append(page.Tiers, t.Name)
	}
	for _, cat := range ranking.Prefs.Categories {
		page.Categories = append(page.Categories, cat.Name)
	}
	return page, nil
}

// scanTail scans the item columns plus extra trailing columns.
type scanTail struct {
	r     rowScanner
	extra []any
}

func (s scanTail) Scan(dest ...any) error { return s.r.Scan(append(dest, s.extra...)...) }

// ItemDetail is one item with its posting text.
type ItemDetail struct {
	Option
	Description string
}

func (c *Catalog) Item(ctx context.Context, id int64) (ItemDetail, error) {
	var d ItemDetail
	var wasSent bool
	v, err := scanItemView(scanTail{c.DB.QueryRowContext(ctx, `SELECT `+itemViewColumns+`, "DigestedAt" IS NOT NULL, "Description"
		FROM "RadarItems" WHERE "Id" = ?`, id), []any{&wasSent, &d.Description}})
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	ranking, err := c.Ranking.Create()
	if err != nil {
		return d, err
	}
	d.Option = Option{ItemView: v, RankScore: ranking.RankScore(v), Priority: ranking.PriorityOf(v), Sent: wasSent}
	return d, nil
}

// SourceStatus is a watched source and its most recent run.
type SourceStatus struct {
	Name, Ats, Board, Priority string
	LastRun                    *time.Time
	Fetched, New               int
	Error                      string
}

func (c *Catalog) Sources(ctx context.Context) ([]SourceStatus, error) {
	watchlist, err := c.Config.Watchlist()
	if err != nil {
		return nil, err
	}
	rows, err := c.DB.QueryContext(ctx, `SELECT r."Source", r."Board", r."StartedAt", r."Fetched", r."New", coalesce(r."Error", '') FROM "SourceRuns" r
		WHERE r."Id" = (SELECT max(x."Id") FROM "SourceRuns" x WHERE x."Source" = r."Source" AND x."Board" = r."Board")`)
	if err != nil {
		return nil, err
	}
	type run struct {
		at           time.Time
		fetched, new int
		err          string
	}
	runs := map[string]run{}
	for rows.Next() {
		var source, board, started string
		var r run
		if err := rows.Scan(&source, &board, &started, &r.fetched, &r.new, &r.err); err != nil {
			rows.Close()
			return nil, err
		}
		if r.at, err = store.ParseTime(started); err != nil {
			rows.Close()
			return nil, err
		}
		runs[strings.ToLower(source)+"/"+board] = r
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]SourceStatus, 0, len(watchlist.Sources)+1)
	add := func(name, ats, board, priority string) {
		s := SourceStatus{Name: name, Ats: ats, Board: board, Priority: priority}
		if r, ok := runs[strings.ToLower(ats)+"/"+board]; ok {
			at := r.at
			s.LastRun, s.Fetched, s.New, s.Error = &at, r.fetched, r.new, r.err
		}
		out = append(out, s)
	}
	hasMail := false
	for _, s := range watchlist.Sources {
		add(s.Name, s.Ats, s.Board, s.Priority)
		hasMail = hasMail || strings.EqualFold(s.Ats, "email")
	}
	// The mailbox is scanned whether or not the watchlist lists it.
	if !hasMail {
		add("Alert emails", "email", "gmail", "")
	}
	return out, nil
}
