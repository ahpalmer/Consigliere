package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ahpalmer/consigliere/internal/agent"
	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/datapack"
	"github.com/ahpalmer/consigliere/internal/kit"
	"github.com/ahpalmer/consigliere/internal/llm"
	"github.com/ahpalmer/consigliere/internal/pipeline"
	"github.com/ahpalmer/consigliere/internal/radar"
	"github.com/ahpalmer/consigliere/internal/store"
)

type fakeLLM struct{}

func (fakeLLM) Structured(_ context.Context, req llm.StructuredRequest) (string, error) {
	if props, _ := req.Schema["properties"].(map[string]any); props["paragraphs"] != nil {
		return `{"paragraphs":["I want to run Acme's fleets.","I ran 4,000 machines."],"why_company":"Fleets.","notes":["Checked."]}`, nil
	}
	return `{"why_company":"Acme runs the fleets I want to work on.","cover_letter":"Dear Hiring Team,\n\nI run fleets.\n\nSam"}`, nil
}
func (fakeLLM) Generate(_ context.Context, req llm.GenerateRequest) (string, error) {
	return "Likely questions: tell me about a fleet rollout.", nil
}

type fixture struct {
	t     *testing.T
	srv   *httptest.Server
	tasks *agent.Tasks
	state *agent.State
}

func newFixture(t *testing.T, token string) *fixture {
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
	clk := clock.NewFake(time.Date(2026, 10, 28, 13, 0, 0, 0, time.UTC))
	location, _ := time.LoadLocation("America/New_York")
	for i, row := range []struct {
		title string
		fit   int
	}{{"Platform Engineer", 88}, {"SRE", 72}, {"Sales Engineer", 20}} {
		_, err := db.Exec(`INSERT INTO "RadarItems" ("Source", "Board", "ExternalId", "Company", "Title", "Locations", "Url", "Description",
			"FirstSeenAt", "LastSeenAt", "ContentHash", "Fingerprint", "Passed", "FitScore", "FitSummary", "FitReasons", "Tier", "ScoredAt")
			VALUES ('greenhouse', 'acme', ?, 'Acme', ?, 'Remote', ?, 'Build fleets.', ?, ?, ?, ?, 1, ?, 'Strong infra match.', 'fleet work
Go services', 'remote', ?)`,
			i, row.title, "https://example.com/"+row.title, store.FormatTime(clk.Now()), store.FormatTime(clk.Now()), row.title, row.title, row.fit,
			store.FormatTime(clk.Now()))
		if err != nil {
			t.Fatal(err)
		}
	}
	state := agent.NewState(clk.Now())
	tasks := agent.NewTasks(ctx)
	pl := &pipeline.Pipeline{DB: db, Clock: clk, Location: location, SoftCap: 10, FollowUpDays: 7, GhostDays: 30}
	radarCfg := radar.ConfigLoader{Pack: pack}
	ranking := radar.RankingFactory{Config: radarCfg}
	drafts := &pipeline.Drafts{Pipeline: pl, Kit: &kit.Kit{Pack: pack, LLM: fakeLLM{}, Owner: "Sam"}, Tasks: tasks, State: state}
	catalog := &radar.Catalog{DB: db, Ranking: ranking, Config: radarCfg, Clock: clk, MinScore: 50}
	s := &Server{Token: token, Pipeline: pl, State: state, Clock: clk, Drafts: drafts, Catalog: catalog,
		Suggestions: &pipeline.Suggestions{Catalog: catalog, Pipeline: pl, Drafts: drafts, HighMinScore: 80, MediumMinScore: 60,
			MaxHigh: 10, MaxMedium: 20, PerCompany: 2, AutoPrepare: 5},
		Feedback: &radar.Feedback{DB: db, Clock: clk},
		Usage:    llm.NewUsageTracker(),
		Assets:   fstest.MapFS{"index.html": {Data: []byte("<!doctype html><div id=root></div>")}, "assets/app.js": {Data: []byte("1")}},
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return &fixture{t: t, srv: srv, tasks: tasks, state: state}
}

// do sends a request and decodes the JSON reply into out (if non-nil), returning the status.
func (f *fixture) do(method, path, body string, out any, headers ...string) int {
	f.t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, f.srv.URL+path, reader)
	if err != nil {
		f.t.Fatal(err)
	}
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			f.t.Fatalf("%s %s: decoding: %v", method, path, err)
		}
	}
	return resp.StatusCode
}

func TestSignInWithToken(t *testing.T) {
	f := newFixture(t, "s3cret")

	var session map[string]bool
	f.do("GET", "/api/session", "", &session)
	if !session["signInRequired"] || session["signedIn"] {
		t.Fatalf("session before sign-in = %v", session)
	}
	if code := f.do("GET", "/api/summary", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("summary without sign-in = %d", code)
	}
	if code := f.do("POST", "/api/login", `{"token":"wrong"}`, nil); code != http.StatusUnauthorized {
		t.Fatalf("wrong token = %d", code)
	}

	req, _ := http.NewRequest("POST", f.srv.URL+"/api/login", strings.NewReader(`{"token":"s3cret"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			cookie = c
		}
	}
	if resp.StatusCode != http.StatusOK || cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("login = %d, cookie %+v", resp.StatusCode, cookie)
	}
	if strings.Contains(cookie.Value, "s3cret") {
		t.Fatal("the cookie carries the token itself")
	}
	if code := f.do("GET", "/api/summary", "", nil, "Cookie", sessionCookie+"="+cookie.Value); code != http.StatusOK {
		t.Fatalf("summary with cookie = %d", code)
	}
	if code := f.do("GET", "/api/summary", "", nil, "Authorization", "Bearer s3cret"); code != http.StatusOK {
		t.Fatalf("summary with bearer token = %d", code)
	}
	if code := f.do("GET", "/api/summary", "", nil, "Cookie", sessionCookie+"=forged"); code != http.StatusUnauthorized {
		t.Fatalf("summary with a forged cookie = %d", code)
	}
}

func TestWritesNeedJSONAndNoTokenMeansLoopbackOnly(t *testing.T) {
	f := newFixture(t, "")
	// A cross-site form can only send form encodings, which are refused.
	req, _ := http.NewRequest("POST", f.srv.URL+"/api/pause", strings.NewReader("x=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType || f.state.Paused() {
		t.Fatalf("form post = %d, paused %v", resp.StatusCode, f.state.Paused())
	}
	// Without a token, a rebinding page's Host header gives it away.
	req, _ = http.NewRequest("GET", f.srv.URL+"/api/summary", nil)
	req.Host = "evil.example"
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign host = %d", resp.StatusCode)
	}
	if code := f.do("GET", "/api/summary", "", nil); code != http.StatusOK {
		t.Fatalf("loopback summary = %d", code)
	}

	if err := CheckAddr("0.0.0.0:8080", ""); err == nil {
		t.Error("allowed a non-loopback address without a token")
	}
	for _, addr := range []string{"127.0.0.1:8080", "localhost:8080", "[::1]:8080"} {
		if err := CheckAddr(addr, ""); err != nil {
			t.Errorf("%s: %v", addr, err)
		}
	}
	if err := CheckAddr("0.0.0.0:8080", "t"); err != nil {
		t.Error(err)
	}
}

func TestPursuitLifecycleOverTheAPI(t *testing.T) {
	f := newFixture(t, "")

	var options struct {
		Options []optionJSON
		Total   int
	}
	f.do("GET", "/api/options", "", &options)
	if options.Total != 2 || options.Options[0].Title != "Platform Engineer" || len(options.Options[0].Reasons) != 2 {
		t.Fatalf("options = %+v", options)
	}
	item := options.Options[0].ID

	var activated struct {
		Pursuit pursuitJSON
		Created bool
	}
	if code := f.do("POST", "/api/pursuits", `{"itemId":`+itoa(item)+`}`, &activated); code != http.StatusCreated || !activated.Created {
		t.Fatalf("activate = %d %+v", code, activated)
	}
	p := activated.Pursuit
	if p.Stage != "active" || p.NextStep != "Write cover letter" || len(p.Steps) != 4 {
		t.Fatalf("new pursuit = %+v", p)
	}
	// Activating again finds the same pursuit.
	if code := f.do("POST", "/api/pursuits", `{"itemId":`+itoa(item)+`}`, &activated); code != http.StatusOK || activated.Created {
		t.Fatalf("second activate = %d %+v", code, activated)
	}

	base := "/api/pursuits/" + itoa(p.ID)
	var got pursuitJSON
	f.do("PUT", base+"/steps/resume", "", &got)
	if got.Steps[0].DoneAt == nil || got.NextStep != "Find people" {
		t.Fatalf("after tick = %+v", got)
	}
	f.do("DELETE", base+"/steps/resume", "", &got)
	if got.Steps[0].DoneAt != nil {
		t.Fatal("untick didn't stick")
	}
	if code := f.do("PUT", base+"/steps/nonsense", "", nil); code != http.StatusBadRequest {
		t.Fatalf("unknown step = %d", code)
	}

	f.do("POST", base+"/applied", "", &got)
	if got.Stage != "waiting" || got.AppliedAt == nil || got.FollowUpAt == nil {
		t.Fatalf("after applied = %+v", got)
	}
	// datetime-local input: no zone, read as the owner's (New York): 14:00 EDT is 18:00 UTC.
	f.do("POST", base+"/interview", `{"at":"2026-10-30T14:00","note":"phone screen with Dana"}`, &got)
	if got.Stage != "active" || got.InterviewAt == nil || got.InterviewAt.UTC().Hour() != 18 || !strings.Contains(got.Notes, "phone screen") {
		t.Fatalf("after interview = %+v", got)
	}
	if code := f.do("POST", base+"/complete", `{"outcome":"bogus"}`, nil); code != http.StatusBadRequest {
		t.Fatalf("bogus outcome = %d", code)
	}

	var summary struct {
		Counts map[string]int
		Todos  []struct{ Kind string }
	}
	f.do("GET", "/api/summary", "", &summary)
	if summary.Counts["active"] != 1 || len(summary.Todos) != 1 || summary.Todos[0].Kind != "interview" {
		t.Fatalf("summary = %+v", summary)
	}

	f.do("POST", base+"/complete", `{"outcome":"rejected","note":"went with someone internal"}`, &got)
	if got.Stage != "completed" || got.StageReached != "interview" {
		t.Fatalf("after complete = %+v", got)
	}
	var detail struct {
		Events []struct{ Kind string }
	}
	f.do("GET", base, "", &detail)
	var kinds []string
	for _, e := range detail.Events {
		kinds = append(kinds, e.Kind)
	}
	if strings.Join(kinds, ",") != "activated,step,step_undone,applied,interview,note,completed" {
		t.Fatalf("events = %v", kinds)
	}

	// Options show the item's pursuit.
	f.do("GET", "/api/options", "", &options)
	if options.Options[0].Pursuit == nil || options.Options[0].Pursuit.Stage != "completed" {
		t.Fatalf("option pursuit = %+v", options.Options[0].Pursuit)
	}
}

func TestManualActivationValidatesTheLink(t *testing.T) {
	f := newFixture(t, "")
	if code := f.do("POST", "/api/pursuits", `{"url":"javascript:alert(1)","company":"Globex","title":"SRE"}`, nil); code != http.StatusBadRequest {
		t.Fatalf("javascript link = %d", code)
	}
	var activated struct{ Pursuit pursuitJSON }
	if code := f.do("POST", "/api/pursuits", `{"url":"https://globex.example/jobs/1","company":"Globex","title":"SRE"}`, &activated); code != http.StatusCreated {
		t.Fatalf("manual activate = %d", code)
	}
	if activated.Pursuit.ItemID != nil || activated.Pursuit.Company != "Globex" {
		t.Fatalf("manual pursuit = %+v", activated.Pursuit)
	}
}

func TestDraftsAreWrittenInTheBackgroundAndKept(t *testing.T) {
	f := newFixture(t, "")
	var activated struct{ Pursuit pursuitJSON }
	f.do("POST", "/api/pursuits", `{"itemId":1}`, &activated)
	base := "/api/pursuits/" + itoa(activated.Pursuit.ID)

	if code := f.do("POST", base+"/drafts", `{"kind":"thanks"}`, nil); code != http.StatusBadRequest {
		t.Fatalf("thanks without details = %d", code)
	}
	var d draftJSON
	if code := f.do("POST", base+"/drafts", `{"kind":"prep","notes":"panel"}`, &d); code != http.StatusAccepted || d.Status != "pending" {
		t.Fatalf("start prep = %d %+v", code, d)
	}
	if !f.tasks.Wait(5 * time.Second) {
		t.Fatal("draft never finished")
	}
	f.do("GET", "/api/drafts/"+itoa(d.ID), "", &d)
	if d.Status != "ready" || !strings.Contains(d.Text, "fleet rollout") {
		t.Fatalf("finished draft = %+v", d)
	}
	var detail struct{ Drafts []draftJSON }
	f.do("GET", base, "", &detail)
	if len(detail.Drafts) != 1 || detail.Drafts[0].Kind != "prep" {
		t.Fatalf("pursuit drafts = %+v", detail.Drafts)
	}
	if code := f.do("GET", "/api/drafts/"+itoa(d.ID)+"/file", "", nil); code != http.StatusNotFound {
		t.Fatalf("file for a draft without one = %d", code)
	}

	// The kill switch stops new model work.
	f.state.Pause()
	if code := f.do("POST", base+"/drafts", `{"kind":"prep"}`, nil); code != http.StatusConflict {
		t.Fatalf("prep while paused = %d", code)
	}
}

func TestAppRoutesFallBackToIndex(t *testing.T) {
	f := newFixture(t, "s3cret")
	for path, want := range map[string]string{"/": "id=root", "/pursuits/3": "id=root", "/assets/app.js": "1"} {
		resp, err := http.Get(f.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), want) {
			t.Errorf("%s = %d %q", path, resp.StatusCode, body)
		}
		if resp.Header.Get("Content-Security-Policy") == "" {
			t.Errorf("%s has no CSP", path)
		}
	}
	if code := f.do("GET", "/api/nope", "", nil, "Authorization", "Bearer s3cret"); code != http.StatusNotFound {
		t.Fatalf("unknown API route = %d", code)
	}
}

func TestSuggestionsPrepareAndApply(t *testing.T) {
	f := newFixture(t, "")
	type page struct {
		High, Medium []suggestionJSON
		Applicant    []kit.Field
	}
	var got page
	f.do("GET", "/api/suggestions", "", &got)
	// Platform Engineer (88) is a strong match, SRE (72) a decent one, Sales Engineer (20) neither.
	if len(got.High) != 1 || got.High[0].Title != "Platform Engineer" || len(got.Medium) != 1 || got.Medium[0].Title != "SRE" {
		t.Fatalf("suggestions = %+v", got)
	}
	sre := got.Medium[0]
	if sre.Kit != nil || sre.ApplyURL != sre.URL {
		t.Fatalf("sre = %+v", sre)
	}
	if !slices.ContainsFunc(got.Applicant, func(f kit.Field) bool { return f.Label == "Email" && f.Value == "sam@example.com" }) {
		t.Fatalf("applicant = %+v", got.Applicant)
	}

	var d draftJSON
	if code := f.do("POST", "/api/items/"+itoa(sre.ID)+"/kit", "", &d); code != http.StatusAccepted || d.Status != "pending" {
		t.Fatalf("prepare = %d %+v", code, d)
	}
	if !f.tasks.Wait(5 * time.Second) {
		t.Fatal("kit still running")
	}
	f.do("GET", "/api/suggestions", "", &got)
	k := got.Medium[0].Kit
	if k == nil || k.Status != "ready" || k.FileName == "" || len(k.Answers) != 2 || k.Answers[0].Label != "Why Acme?" || k.Text != "" {
		t.Fatalf("kit = %+v", k)
	}
	if code := f.do("GET", "/api/drafts/"+itoa(k.ID)+"/file", "", nil); code != http.StatusOK {
		t.Fatalf("resume download = %d", code)
	}

	// Applying moves it into the pipeline (waiting), so it leaves the suggestions; 👎 drops one too.
	var p pursuitJSON
	if code := f.do("POST", "/api/items/"+itoa(sre.ID)+"/applied", "", &p); code != http.StatusOK || p.Stage != "waiting" || p.AppliedAt == nil {
		t.Fatalf("applied = %d %+v", code, p)
	}
	f.do("PUT", "/api/items/"+itoa(got.High[0].ID)+"/feedback", `{"value":-1}`, nil)
	f.do("GET", "/api/suggestions", "", &got)
	if len(got.High) != 0 || len(got.Medium) != 0 {
		t.Fatalf("after = %+v", got)
	}
}

func TestCoverLetterForAnItemJoinsItsPursuit(t *testing.T) {
	f := newFixture(t, "")
	var d draftJSON
	if code := f.do("POST", "/api/items/2/cover", `{"instructions":"I like fleets."}`, &d); code != http.StatusAccepted || d.Kind != "cover" || d.PursuitID != nil {
		t.Fatalf("cover = %d %+v", code, d)
	}
	if !f.tasks.Wait(5 * time.Second) {
		t.Fatal("letter still being written")
	}
	f.do("GET", "/api/drafts/"+itoa(d.ID), "", &d)
	if d.Status != "ready" || !strings.HasPrefix(d.Text, "Dear Acme Hiring Team,") || !strings.Contains(d.Text, "Checked.") ||
		!strings.HasSuffix(d.FileName, "Cover_Letter_-_Acme.docx") || d.Answers[0].Label != "Cover letter" {
		t.Fatalf("letter = %+v", d)
	}
	if code := f.do("GET", "/api/drafts/"+itoa(d.ID)+"/file", "", nil); code != http.StatusOK {
		t.Fatalf("download = %d", code)
	}
	var item struct{ Drafts []draftJSON }
	f.do("GET", "/api/items/2", "", &item)
	if len(item.Drafts) != 1 || item.Drafts[0].Text != "" {
		t.Fatalf("item drafts = %+v", item.Drafts)
	}

	// Once it's a pursuit, a new letter belongs to it, and the pursuit lists both.
	var activated struct{ Pursuit pursuitJSON }
	f.do("POST", "/api/pursuits", `{"itemId":2}`, &activated)
	f.do("POST", "/api/items/2/cover", `{}`, &d)
	if d.PursuitID == nil || *d.PursuitID != activated.Pursuit.ID {
		t.Fatalf("second letter = %+v", d)
	}
	f.tasks.Wait(5 * time.Second)
	var detail struct{ Drafts []draftJSON }
	f.do("GET", "/api/pursuits/"+itoa(activated.Pursuit.ID), "", &detail)
	if len(detail.Drafts) != 2 {
		t.Fatalf("pursuit drafts = %+v", detail.Drafts)
	}
	if code := f.do("POST", "/api/items/999/cover", `{}`, nil); code != http.StatusNotFound {
		t.Fatalf("unknown item = %d", code)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
