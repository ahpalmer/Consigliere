package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ahpalmer/consigliere/internal/agent"
	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/comp"
	"github.com/ahpalmer/consigliere/internal/datapack"
	"github.com/ahpalmer/consigliere/internal/kit"
	"github.com/ahpalmer/consigliere/internal/llm"
	"github.com/ahpalmer/consigliere/internal/pipeline"
	"github.com/ahpalmer/consigliere/internal/radar"
	"github.com/ahpalmer/consigliere/internal/store"
	"github.com/ahpalmer/consigliere/internal/web"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const token = "s3cret"

// brokenLLM fails every call, so drafts end as failed without a real model.
type brokenLLM struct{}

func (brokenLLM) Structured(context.Context, llm.StructuredRequest) (string, error) {
	return "", errors.New("no model in tests")
}

func (brokenLLM) Generate(context.Context, llm.GenerateRequest) (string, error) {
	return "", errors.New("no model in tests")
}

// noScorer: pasting is only tried while paused here, so nothing is scored.
type noScorer struct{}

func (noScorer) BuildContext() (radar.ScoringContext, error) { return radar.ScoringContext{}, nil }
func (noScorer) Score(context.Context, radar.ScoreInput, radar.ScoringContext) (*radar.ScoreResult, error) {
	return nil, errors.New("not in tests")
}

// dashboard is the real dashboard API over a temp database with two scored jobs, like the bot serves it.
func dashboard(t *testing.T) (*httptest.Server, *agent.State) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	pack, err := datapack.Open("../../samples/data-pack")
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFake(time.Date(2026, 11, 21, 13, 0, 0, 0, time.UTC))
	now := store.FormatTime(clk.Now())
	for i, title := range []string{"Platform Engineer", "SRE"} {
		if _, err := db.Exec(`INSERT INTO "RadarItems" ("Source", "Board", "ExternalId", "Company", "Title", "Locations", "Url", "Description",
			"FirstSeenAt", "LastSeenAt", "ContentHash", "Fingerprint", "Passed", "FitScore", "FitSummary", "Tier", "ScoredAt")
			VALUES ('greenhouse', 'acme', ?, 'Acme', ?, 'Remote', ?, 'Build fleets.', ?, ?, ?, ?, 1, 90, 'Strong match.', 'remote', ?)`,
			i, title, "https://example.com/"+title, now, now, title, title, now); err != nil {
			t.Fatal(err)
		}
	}
	location, _ := time.LoadLocation("America/New_York")
	state := agent.NewState(clk.Now())
	tasks := agent.NewTasks(ctx)
	t.Cleanup(func() { tasks.Wait(5 * time.Second) })
	pl := &pipeline.Pipeline{DB: db, Clock: clk, Location: location, SoftCap: 10, FollowUpDays: 7, GhostDays: 30}
	cfg := radar.ConfigLoader{Pack: pack}
	catalog := &radar.Catalog{DB: db, Ranking: radar.RankingFactory{Config: cfg}, Config: cfg, Clock: clk, MinScore: 0}
	drafts := &pipeline.Drafts{Pipeline: pl, Kit: &kit.Kit{Pack: pack, LLM: brokenLLM{}, Owner: "Sam"}, Tasks: tasks, State: state}
	s := &web.Server{Token: token, Pipeline: pl, Drafts: drafts, Catalog: catalog, State: state, Clock: clk, Pack: pack,
		Suggestions: &pipeline.Suggestions{Catalog: catalog, Pipeline: pl, Drafts: drafts, HighMinScore: 80, MediumMinScore: 60,
			MaxHigh: 10, MaxMedium: 20, PerCompany: 2},
		Feedback: &radar.Feedback{DB: db, Clock: clk},
		Scoring:  &radar.ScoringRunner{DB: db, Scorer: noScorer{}, Ranking: radar.RankingFactory{Config: cfg}, State: state, Clock: clk},
		Usage:    llm.NewUsageTracker(),
		Offers:   &comp.Offers{DB: db, Clock: clk},
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv, state
}

// connect runs the MCP server in memory and returns a client session to it.
func connect(t *testing.T, s *Server, opts *mcp.ClientOptions) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()
	ss, err := s.MCP("test").Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, opts).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// call calls a tool and decodes its JSON text into out (when not nil). It returns the text and whether it was an error.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, out any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if out != nil && !res.IsError {
		if err := json.Unmarshal([]byte(text), out); err != nil {
			t.Fatalf("%s: %v in %s", name, err, text)
		}
	}
	return text, res.IsError
}

func TestToolsWorkAgainstTheDashboard(t *testing.T) {
	srv, state := dashboard(t)
	cs := connect(t, &Server{API: &API{Base: srv.URL, Token: token}, PollEvery: 10 * time.Millisecond, MaxWait: 5 * time.Second}, nil)

	var options struct {
		Options []struct {
			ID    int64
			Title string
		}
	}
	call(t, cs, "search_options", map[string]any{"query": "platform", "limit": 5}, &options)
	if len(options.Options) != 1 || options.Options[0].Title != "Platform Engineer" {
		t.Fatalf("search: %+v", options)
	}
	id := options.Options[0].ID

	if text, isErr := call(t, cs, "rate_job", map[string]any{"item_id": id, "rating": "up"}, nil); isErr {
		t.Fatal(text)
	}
	var item struct{ Item struct{ Feedback *int } }
	call(t, cs, "get_item", map[string]any{"item_id": id}, &item)
	if item.Item.Feedback == nil || *item.Item.Feedback != 1 {
		t.Fatalf("rating didn't stick: %+v", item)
	}

	var activated struct{ Pursuit struct{ ID int64 } }
	call(t, cs, "make_active", map[string]any{"item_id": id}, &activated)
	pursuit := activated.Pursuit
	if pursuit.ID == 0 {
		t.Fatal("no pursuit")
	}
	for _, step := range []struct {
		tool string
		args map[string]any
	}{
		{"set_step", map[string]any{"pursuit_id": pursuit.ID, "step": "people", "done": true}},
		{"add_note", map[string]any{"pursuit_id": pursuit.ID, "text": "Met Dana at the meetup."}},
		{"mark_applied", map[string]any{"pursuit_id": pursuit.ID}},
		{"schedule_interview", map[string]any{"pursuit_id": pursuit.ID, "at": "2026-12-01T14:00", "note": "Panel"}},
	} {
		if text, isErr := call(t, cs, step.tool, step.args, nil); isErr {
			t.Fatalf("%s: %s", step.tool, text)
		}
	}
	var got struct {
		Pursuit struct {
			Stage       string
			AppliedAt   *time.Time
			InterviewAt *time.Time
			Notes       string
		}
	}
	call(t, cs, "get_pursuit", map[string]any{"pursuit_id": pursuit.ID}, &got)
	// Applied, then an interview brings it back to Active.
	if got.Pursuit.Stage != "active" || got.Pursuit.AppliedAt == nil || got.Pursuit.InterviewAt == nil || !strings.Contains(got.Pursuit.Notes, "Dana") {
		t.Fatalf("pursuit: %+v", got)
	}
	listed, _ := call(t, cs, "list_pursuits", map[string]any{"stage": "active"}, nil)
	if !strings.Contains(listed, "Platform Engineer") {
		t.Fatalf("active: %s", listed)
	}

	// A draft is started, waited on, and comes back failed (no model here) rather than hanging.
	var draft struct{ Status, Error string }
	call(t, cs, "write_draft", map[string]any{"pursuit_id": pursuit.ID, "kind": "prep"}, &draft)
	if draft.Status != "failed" {
		t.Fatalf("draft: %+v", draft)
	}

	if text, isErr := call(t, cs, "complete_pursuit", map[string]any{"pursuit_id": pursuit.ID, "outcome": "maybe"}, nil); !isErr || !strings.Contains(text, "outcome must be") {
		t.Fatalf("a bad outcome should be the dashboard's error: %q", text)
	}
	if text, isErr := call(t, cs, "get_item", map[string]any{"item_id": 999}, nil); !isErr || text != "not found" {
		t.Fatalf("missing item: %q", text)
	}

	call(t, cs, "pause_bot", nil, nil)
	if !state.Paused() {
		t.Fatal("not paused")
	}
	if text, isErr := call(t, cs, "paste_posting", map[string]any{"item_id": id, "text": "The whole posting"}, nil); !isErr || !strings.Contains(text, "Paused") {
		t.Fatalf("paste while paused: %q", text)
	}
	call(t, cs, "resume_bot", nil, nil)
	if state.Paused() {
		t.Fatal("still paused")
	}
}

func TestAWrongTokenSaysSo(t *testing.T) {
	srv, _ := dashboard(t)
	cs := connect(t, &Server{API: &API{Base: srv.URL, Token: "wrong"}}, nil)
	if text, isErr := call(t, cs, "get_summary", nil, nil); !isErr || !strings.Contains(text, "CONSIGLIERE_TOKEN") {
		t.Fatalf("%q", text)
	}
}

func TestToolsAreAnnotated(t *testing.T) {
	cs := connect(t, &Server{API: &API{}}, nil)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		tools[tool.Name] = tool
	}
	if a := tools["search_options"].Annotations; !a.ReadOnlyHint || *a.OpenWorldHint {
		t.Fatalf("search_options: %+v", a)
	}
	if a := tools["complete_pursuit"].Annotations; a.ReadOnlyHint || !*a.DestructiveHint {
		t.Fatalf("complete_pursuit: %+v", a)
	}
	if a := tools["add_note"].Annotations; a.ReadOnlyHint || *a.DestructiveHint || a.IdempotentHint {
		t.Fatalf("add_note: %+v", a)
	}
	if len(tools) != 24 {
		t.Fatalf("%d tools", len(tools))
	}
}

func TestResourcesAndPrompts(t *testing.T) {
	srv, _ := dashboard(t)
	cs := connect(t, &Server{API: &API{Base: srv.URL, Token: token}}, nil)
	ctx := context.Background()

	res, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "consigliere://item/1"})
	if err != nil || !strings.Contains(res.Contents[0].Text, "Platform Engineer") {
		t.Fatalf("item resource: %v %+v", err, res)
	}
	if res, err = cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "consigliere://pursuits/active"}); err != nil {
		t.Fatalf("pursuits resource: %v", err)
	}
	if _, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "consigliere://item/999"}); err == nil {
		t.Fatal("a missing item should be not found")
	}
	if _, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "consigliere://pursuits/someday"}); err == nil {
		t.Fatal("an unknown stage should be not found")
	}

	p, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{Name: "prep_interview", Arguments: map[string]string{"pursuit_id": "7"}})
	if err != nil || !strings.Contains(p.Messages[0].Content.(*mcp.TextContent).Text, "pursuit 7") {
		t.Fatalf("prompt: %v %+v", err, p)
	}
	if _, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{Name: "prep_interview"}); err == nil {
		t.Fatal("pursuit_id is required")
	}
}

func TestResourcePaths(t *testing.T) {
	for uri, want := range map[string]string{
		"consigliere://summary":          "/api/summary",
		"consigliere://pursuits/waiting": "/api/pursuits?stage=waiting",
		"consigliere://pursuit/12":       "/api/pursuits/12",
		"consigliere://item/3":           "/api/items/3",
		"consigliere://draft/5":          "/api/drafts/5",
		"consigliere://item/x":           "",
		"consigliere://../api/pause":     "",
	} {
		p, q, ok := path(uri)
		if len(q) > 0 {
			p += "?" + q.Encode()
		}
		if !ok {
			p = ""
		}
		if p != want {
			t.Errorf("%s: %q, want %q", uri, p, want)
		}
	}
}

// A draft that takes a while reports progress to a client that asked for it.
func TestWaitingForADraftReportsProgress(t *testing.T) {
	var checks atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := "pending"
		if r.Method == http.MethodGet && checks.Add(1) >= 3 {
			status = "ready"
		}
		json.NewEncoder(w).Encode(map[string]any{"id": 4, "status": status, "text": "Dear Acme"})
	}))
	defer api.Close()

	var mu sync.Mutex
	var progress []string
	cs := connect(t, &Server{API: &API{Base: api.URL}, PollEvery: time.Millisecond}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			mu.Lock()
			defer mu.Unlock()
			progress = append(progress, req.Params.Message)
		}})
	params := &mcp.CallToolParams{Name: "write_draft", Arguments: map[string]any{"item_id": 1, "kind": "cover"}}
	params.SetProgressToken("p1")
	res, err := cs.CallTool(context.Background(), params)
	if err != nil || res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "ready") {
		t.Fatalf("%v %+v", err, res)
	}
	// Notifications may arrive just after the result.
	deadline := time.Now().Add(time.Second)
	for {
		mu.Lock()
		n := len(progress)
		mu.Unlock()
		if n >= 3 || time.Now().After(deadline) {
			if n < 3 || !strings.HasPrefix(progress[0], "Writing draft 4") {
				t.Fatalf("progress: %q", progress)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}
