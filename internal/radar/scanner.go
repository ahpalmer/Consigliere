package radar

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/store"
)

type SourceResult struct {
	Name, Kind           string
	Fetched, New, Passed int
	Error                string
}

type ScanSummary struct{ Sources []SourceResult }

func (s ScanSummary) New() (n int) {
	for _, r := range s.Sources {
		n += r.New
	}
	return
}

func (s ScanSummary) Passed() (n int) {
	for _, r := range s.Sources {
		n += r.Passed
	}
	return
}

// Scanner fetches every watched source, records new items, refreshes known ones, and runs the rule filter.
type Scanner struct {
	DB      *sql.DB
	Sources []Source
	Config  ConfigLoader
	Clock   clock.Clock

	mu      sync.Mutex
	running bool
}

// Scan returns ok=false when a scan is already running.
func (s *Scanner) Scan(ctx context.Context) (ScanSummary, bool, error) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return ScanSummary{}, false, nil
	}
	s.running = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.running = false; s.mu.Unlock() }()

	watchlist, err := s.Config.Watchlist()
	if err != nil {
		return ScanSummary{}, true, err
	}
	prefs, err := s.Config.Preferences()
	if err != nil {
		return ScanSummary{}, true, err
	}
	byKind := map[string]Source{}
	var aggregators []string
	for _, src := range s.Sources {
		byKind[strings.ToLower(src.Kind())] = src
		if src.IsAggregator() {
			aggregators = append(aggregators, src.Kind())
		}
	}

	var summary ScanSummary
	for _, watched := range watchlist.Sources {
		if ctx.Err() != nil {
			return summary, true, ctx.Err()
		}
		summary.Sources = append(summary.Sources, s.scanSource(ctx, watched, byKind, aggregators, prefs))
	}
	return summary, true, nil
}

func (s *Scanner) scanSource(ctx context.Context, watched WatchedSource, byKind map[string]Source, aggregators []string, prefs Preferences) SourceResult {
	started := s.Clock.Now()
	result := SourceResult{Name: watched.Name, Kind: watched.Ats}
	if src, ok := byKind[strings.ToLower(watched.Ats)]; !ok {
		result.Error = "unsupported source '" + watched.Ats + "'"
	} else {
		result.Kind = src.Kind()
		items, err := src.Fetch(ctx, watched)
		if err == nil {
			result, err = s.persist(ctx, src, watched, items, aggregators, prefs)
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("scan failed", "source", watched.Name, "kind", watched.Ats, "board", watched.Board, "err", err)
			result = SourceResult{Name: watched.Name, Kind: src.Kind(), Error: err.Error()}
		}
	}

	_, err := s.DB.ExecContext(ctx, `INSERT INTO "SourceRuns" ("Source", "Board", "StartedAt", "FinishedAt", "Fetched", "New", "Passed", "Error")
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		result.Kind, watched.Board, store.FormatTime(started), store.FormatTime(s.Clock.Now()), result.Fetched, result.New, result.Passed, nullString(result.Error))
	if err != nil {
		slog.Warn("recording source run failed", "source", watched.Name, "err", err)
	}
	return result
}

type knownItem struct {
	id                           int64
	contentHash                  string
	compMin, compMax             *float64
	digested                     bool
	passed                       bool
	tier, filterReason, titleKey string
	hasTitleKey                  bool
}

func (s *Scanner) persist(ctx context.Context, src Source, watched WatchedSource, fetched []FetchedItem, aggregators []string, prefs Preferences) (SourceResult, error) {
	now := store.FormatTime(s.Clock.Now())
	kind := src.Kind()

	// Dedupe within the fetch.
	seen := map[string]bool{}
	var items []FetchedItem
	for _, it := range fetched {
		if !seen[it.ExternalID] {
			seen[it.ExternalID] = true
			items = append(items, it)
		}
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return SourceResult{}, err
	}
	defer tx.Rollback()

	known := map[string]knownItem{}
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ExternalID)
	}
	for chunk := range slices.Chunk(ids, 500) {
		rows, err := tx.QueryContext(ctx, `SELECT "Id", "ExternalId", "ContentHash", "CompMin", "CompMax", "DigestedAt", "Passed", "Tier", "FilterReason", "TitleKey"
			FROM "RadarItems" WHERE "Source" = ? AND "Board" = ? AND "ExternalId" IN (`+store.Placeholders(len(chunk))+`)`,
			append([]any{kind, watched.Board}, store.Args(chunk)...)...)
		if err != nil {
			return SourceResult{}, err
		}
		for rows.Next() {
			var (
				k                             knownItem
				ext                           string
				compMin, compMax              sql.NullFloat64
				digested, tier, reason, title sql.NullString
			)
			if err := rows.Scan(&k.id, &ext, &k.contentHash, &compMin, &compMax, &digested, &k.passed, &tier, &reason, &title); err != nil {
				rows.Close()
				return SourceResult{}, err
			}
			k.compMin, k.compMax = floatPtr(compMin), floatPtr(compMax)
			k.digested, k.tier, k.filterReason = digested.Valid, tier.String, reason.String
			k.titleKey, k.hasTitleKey = title.String, title.Valid
			known[ext] = k
		}
		rows.Close()
		if _, err := tx.ExecContext(ctx, `UPDATE "RadarItems" SET "LastSeenAt" = ? WHERE "Source" = ? AND "Board" = ? AND "ExternalId" IN (`+store.Placeholders(len(chunk))+`)`,
			append([]any{now, kind, watched.Board}, store.Args(chunk)...)...); err != nil {
			return SourceResult{}, err
		}
	}

	if err := refreshChanged(ctx, tx, items, known, prefs); err != nil {
		return SourceResult{}, err
	}

	var fresh []FetchedItem
	for _, it := range items {
		if _, ok := known[it.ExternalID]; !ok {
			fresh = append(fresh, it)
		}
	}

	passed := 0
	for _, it := range fresh {
		fingerprint, titleKey := Fingerprint(it), TitleKey(it)
		duplicateOf, err := originalFor(ctx, tx, fingerprint, titleKey, src.IsAggregator(), aggregators)
		if err != nil {
			return SourceResult{}, err
		}
		outcome := Evaluate(it, prefs)
		if outcome.Passed {
			passed++
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO "RadarItems" ("Source", "Board", "ExternalId", "Company", "Title", "Locations", "Url", "Description",
			"CompMin", "CompMax", "CompCurrency", "PostedAt", "FirstSeenAt", "LastSeenAt", "ContentHash", "Fingerprint", "TitleKey", "DuplicateOfId",
			"Passed", "Tier", "FilterReason") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			kind, watched.Board, it.ExternalID, it.Company, it.Title, strings.Join(it.Locations, "; "), it.URL, it.Description,
			nullFloat(it.CompMin), nullFloat(it.CompMax), nullString(it.CompCurrency), store.FormatTimePtr(it.PostedAt), now, now,
			Hash(it.Title, it.Description), fingerprint, titleKey, duplicateOf, outcome.Passed, nullString(outcome.Tier), nullString(outcome.Reason))
		if err != nil {
			return SourceResult{}, err
		}
	}

	if !src.IsAggregator() {
		if err := adoptAggregatorCopies(ctx, tx, aggregators); err != nil {
			return SourceResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return SourceResult{}, err
	}
	slog.Info("scanned", "source", watched.Name, "fetched", len(items), "new", len(fresh), "passed", passed)
	return SourceResult{Name: watched.Name, Kind: kind, Fetched: len(items), New: len(fresh), Passed: passed}, nil
}

// originalFor finds the item a new one duplicates: the same fingerprint anywhere, or, for aggregator items,
// a primary item with the same company + title (location text often differs).
func originalFor(ctx context.Context, tx *sql.Tx, fingerprint, titleKey string, aggregator bool, aggregators []string) (any, error) {
	var id sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT min("Id") FROM "RadarItems" WHERE "Fingerprint" = ? AND "DuplicateOfId" IS NULL`, fingerprint).Scan(&id)
	if err != nil {
		return nil, err
	}
	if id.Valid {
		return id.Int64, nil
	}
	if !aggregator {
		return nil, nil
	}
	err = tx.QueryRowContext(ctx, `SELECT min("Id") FROM "RadarItems" WHERE "TitleKey" = ? AND "DuplicateOfId" IS NULL AND "Source" NOT IN (`+
		store.Placeholders(len(aggregators))+`)`, append([]any{titleKey}, store.Args(aggregators)...)...).Scan(&id)
	if err != nil || !id.Valid {
		return nil, err
	}
	return id.Int64, nil
}

// refreshChanged picks up edited postings, parser improvements, and preference edits. Items not yet digested are
// re-filtered against current preferences; digested ones keep their filter result so nothing already sent is
// re-sent or dropped. PostedAt and FirstSeenAt are never touched here.
func refreshChanged(ctx context.Context, tx *sql.Tx, items []FetchedItem, known map[string]knownItem, prefs Preferences) error {
	refreshed := 0
	for _, it := range items {
		old, ok := known[it.ExternalID]
		if !ok {
			continue
		}
		hash := Hash(it.Title, it.Description)
		contentChanged := hash != old.contentHash || !sameFloat(it.CompMin, old.compMin) || !sameFloat(it.CompMax, old.compMax) || !old.hasTitleKey
		var outcome *FilterOutcome
		if !old.digested {
			o := Evaluate(it, prefs)
			outcome = &o
		}
		filterChanged := outcome != nil && (outcome.Passed != old.passed || outcome.Tier != old.tier || outcome.Reason != old.filterReason)
		if !contentChanged && !filterChanged {
			continue
		}

		sets, args := []string{}, []any{}
		if contentChanged {
			sets = append(sets, `"Title" = ?`, `"Description" = ?`, `"Locations" = ?`, `"CompMin" = ?`, `"CompMax" = ?`, `"CompCurrency" = ?`, `"ContentHash" = ?`, `"TitleKey" = ?`)
			args = append(args, it.Title, it.Description, strings.Join(it.Locations, "; "), nullFloat(it.CompMin), nullFloat(it.CompMax), nullString(it.CompCurrency), hash, TitleKey(it))
		}
		if outcome != nil {
			sets = append(sets, `"Passed" = ?`, `"Tier" = ?`, `"FilterReason" = ?`)
			args = append(args, outcome.Passed, nullString(outcome.Tier), nullString(outcome.Reason))
		}
		if _, err := tx.ExecContext(ctx, `UPDATE "RadarItems" SET `+strings.Join(sets, ", ")+` WHERE "Id" = ?`, append(args, old.id)...); err != nil {
			return err
		}
		refreshed++
	}
	if refreshed > 0 {
		slog.Info("refreshed changed items", "count", refreshed)
	}
	return nil
}

// adoptAggregatorCopies handles the reverse case: a primary item arrived after an aggregator already reported the
// same role. The aggregator copy (less detail) becomes the duplicate, unless it was already sent to the owner.
func adoptAggregatorCopies(ctx context.Context, tx *sql.Tx, aggregators []string) error {
	if len(aggregators) == 0 {
		return nil
	}
	in := store.Placeholders(len(aggregators))
	args := append(store.Args(aggregators), store.Args(aggregators)...)
	res, err := tx.ExecContext(ctx, `UPDATE "RadarItems" AS a SET "DuplicateOfId" = (
			SELECT min(p."Id") FROM "RadarItems" p WHERE p."TitleKey" = a."TitleKey" AND p."DuplicateOfId" IS NULL AND p."Source" NOT IN (`+in+`))
		WHERE a."Source" IN (`+in+`) AND a."DuplicateOfId" IS NULL AND a."DigestedAt" IS NULL AND a."TitleKey" IS NOT NULL
		  AND EXISTS (SELECT 1 FROM "RadarItems" p WHERE p."TitleKey" = a."TitleKey" AND p."DuplicateOfId" IS NULL AND p."Source" NOT IN (`+in+`))`,
		append(args, store.Args(aggregators)...)...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		slog.Info("aggregator items matched a primary source", "count", n)
	}
	return nil
}

func sameFloat(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// TitleKey hashes normalized company + title only, to match the same role reported by an aggregator.
func TitleKey(it FetchedItem) string { return Hash(Normalize(it.Company), Normalize(it.Title)) }

// Fingerprint hashes normalized company + title + sorted locations, to detect the same item from another source.
func Fingerprint(it FetchedItem) string {
	locations := make([]string, len(it.Locations))
	for i, l := range it.Locations {
		locations[i] = Normalize(l)
	}
	slices.Sort(locations)
	return Hash(Normalize(it.Company), Normalize(it.Title), strings.Join(locations, "|"))
}
