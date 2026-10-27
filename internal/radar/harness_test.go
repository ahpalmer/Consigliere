package radar

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ahpalmer/consigliere/internal/agent"
	"github.com/ahpalmer/consigliere/internal/channel"
	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/datapack"
	"github.com/ahpalmer/consigliere/internal/store"
)

const defaultWatchlist = `
sources:
  - { name: Acme, ats: fake, board: acme }
  - { name: Globex, ats: fake, board: globex, priority: low }
`

const defaultPreferences = `
titles:
  include: [engineer]
max_posting_age_days: 90
categories:
  - { name: software, description: software roles }
  - { name: robotics, description: robotics roles }
tiers:
  - { name: local, locations: [raleigh], alert: instant }
  - { name: anywhere }
`

// harness wires the Radar pipeline like the host does, against a temp data pack and a real SQLite file with the
// real migrations, but with fake sources, a fake scorer, a fake channel, and a controllable clock.
type harness struct {
	t          *testing.T
	ctx        context.Context
	db         *sql.DB
	packDir    string
	clock      *clock.Fake
	source     *fakeSource
	aggregator *fakeSource
	scorer     *fakeScorer
	channel    *fakeChannel
	state      *agent.State
	scanner    *Scanner
	scoring    *ScoringRunner
	digest     *Digest
	alerts     *Alerts
	feedback   *Feedback
}

type harnessOptions struct {
	watchlist, preferences string
	digestMax, alertMin    int
}

func newHarness(t *testing.T, opts harnessOptions) *harness {
	t.Helper()
	if opts.watchlist == "" {
		opts.watchlist = defaultWatchlist
	}
	if opts.preferences == "" {
		opts.preferences = defaultPreferences
	}
	if opts.digestMax == 0 {
		opts.digestMax = 25
	}
	if opts.alertMin == 0 {
		opts.alertMin = 101 // no alerts unless a test asks for them
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "radar"), 0o755)
	os.WriteFile(filepath.Join(dir, "radar", "watchlist.yaml"), []byte(opts.watchlist), 0o644)
	os.WriteFile(filepath.Join(dir, "radar", "preferences.yaml"), []byte(opts.preferences), 0o644)
	pack, err := datapack.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(dir, "db", "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	h := &harness{
		t: t, ctx: ctx, db: db, packDir: dir,
		clock:      clock.NewFake(time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)),
		source:     &fakeSource{kind: "fake", items: map[string][]FetchedItem{}},
		aggregator: &fakeSource{kind: "fakeagg", aggregator: true, items: map[string][]FetchedItem{}},
		scorer:     &fakeScorer{byTitle: map[string]*ScoreResult{}, version: "v1"},
		channel:    &fakeChannel{},
		state:      agent.NewState(time.Now()),
	}
	cfg := ConfigLoader{Pack: pack}
	ranking := RankingFactory{Config: cfg}
	h.scanner = &Scanner{DB: db, Sources: []Source{h.source, h.aggregator}, Config: cfg, Clock: h.clock}
	h.alerts = &Alerts{DB: db, Ranking: ranking, Channel: h.channel, Clock: h.clock, MinScore: opts.alertMin}
	h.scoring = &ScoringRunner{DB: db, Scorer: h.scorer, Ranking: ranking, Alerts: h.alerts, State: h.state, Clock: h.clock, BatchSize: 200, Concurrency: 4}
	h.digest = &Digest{DB: db, Ranking: ranking, Clock: h.clock, MaxItems: opts.digestMax, MinScore: 50}
	h.feedback = &Feedback{DB: db, Clock: h.clock}
	return h
}

func (h *harness) scan() ScanSummary {
	h.t.Helper()
	s, ok, err := h.scanner.Scan(h.ctx)
	if err != nil || !ok {
		h.t.Fatalf("scan: ok=%v err=%v", ok, err)
	}
	return s
}

func (h *harness) score() ScoringSummary {
	h.t.Helper()
	s, ok, err := h.scoring.Run(h.ctx)
	if err != nil || !ok {
		h.t.Fatalf("score: ok=%v err=%v", ok, err)
	}
	return s
}

func (h *harness) nextDigest() DigestResult {
	h.t.Helper()
	d, err := h.digest.Next(h.ctx)
	if err != nil {
		h.t.Fatal(err)
	}
	return d
}

type row struct {
	ID                                     int64
	Source, ExternalID, Title, Description string
	Tier                                   sql.NullString
	Passed                                 bool
	CompMax                                sql.NullFloat64
	PostedAt, FirstSeenAt, LastSeenAt      sql.NullString
	DuplicateOf                            sql.NullInt64
	FitScore, Feedback                     sql.NullInt64
	Category                               sql.NullString
}

func (h *harness) rows() []row {
	h.t.Helper()
	rs, err := h.db.Query(`SELECT "Id", "Source", "ExternalId", "Title", "Description", "Tier", "Passed", "CompMax", "PostedAt", "FirstSeenAt",
		"LastSeenAt", "DuplicateOfId", "FitScore", "Feedback", "Category" FROM "RadarItems" ORDER BY "Id"`)
	if err != nil {
		h.t.Fatal(err)
	}
	defer rs.Close()
	var out []row
	for rs.Next() {
		var r row
		if err := rs.Scan(&r.ID, &r.Source, &r.ExternalID, &r.Title, &r.Description, &r.Tier, &r.Passed, &r.CompMax, &r.PostedAt,
			&r.FirstSeenAt, &r.LastSeenAt, &r.DuplicateOf, &r.FitScore, &r.Feedback, &r.Category); err != nil {
			h.t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func (h *harness) writePreferences(yaml string) {
	os.WriteFile(filepath.Join(h.packDir, "radar", "preferences.yaml"), []byte(yaml), 0o644)
}

func fake(id, title string, opts ...func(*FetchedItem)) FetchedItem {
	it := FetchedItem{ExternalID: id, Company: "Acme", Title: title, Locations: []string{"Raleigh, NC"},
		URL: "https://example.test/" + id, Description: "About " + title}
	for _, o := range opts {
		o(&it)
	}
	return it
}

func company(c string) func(*FetchedItem)  { return func(i *FetchedItem) { i.Company = c } }
func location(l string) func(*FetchedItem) { return func(i *FetchedItem) { i.Locations = []string{l} } }
func posted(t time.Time) func(*FetchedItem) {
	return func(i *FetchedItem) { i.PostedAt = &t }
}

type fakeSource struct {
	kind       string
	aggregator bool
	items      map[string][]FetchedItem
}

func (f *fakeSource) Kind() string       { return f.kind }
func (f *fakeSource) IsAggregator() bool { return f.aggregator }
func (f *fakeSource) Fetch(_ context.Context, src WatchedSource) ([]FetchedItem, error) {
	return f.items[src.Board], nil
}

// fakeScorer scores by title lookup; anything unlisted gets 60/software. A nil entry means "declined".
// Changing version invalidates scores.
type fakeScorer struct {
	mu      sync.Mutex
	byTitle map[string]*ScoreResult
	calls   []string
	version string
}

func (f *fakeScorer) BuildContext() (ScoringContext, error) {
	return ScoringContext{Model: "fake", Rubric: "rubric", Profile: f.version, Categories: []string{"software", "robotics", "other"}}, nil
}

func (f *fakeScorer) Score(_ context.Context, item ScoreInput, _ ScoringContext) (*ScoreResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, item.Title)
	if r, ok := f.byTitle[item.Title]; ok {
		if r == nil {
			return nil, nil
		}
		return r, nil
	}
	return &ScoreResult{Fit: 60, Category: "software", Summary: item.Title + " summary", Reasons: []string{"reason"}, Gaps: []string{"gap"}}, nil
}

func (f *fakeScorer) set(title string, fit int, category string) {
	f.byTitle[title] = &ScoreResult{Fit: fit, Category: category, Summary: title + " summary"}
}

type sent struct {
	text    string
	buttons []channel.Button
}

type fakeChannel struct {
	mu   sync.Mutex
	sent []sent
}

func (f *fakeChannel) Name() string { return "fake" }
func (f *fakeChannel) Notify(_ context.Context, text string, buttons ...channel.Button) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sent{text, buttons})
	return nil
}
