package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ahpalmer/consigliere/internal/pipeline"
	"github.com/ahpalmer/consigliere/internal/radar"
)

func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/session", s.getSession)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)

	mux.HandleFunc("GET /api/summary", s.summary)
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("POST /api/pause", s.setPaused(true))
	mux.HandleFunc("POST /api/resume", s.setPaused(false))

	mux.HandleFunc("GET /api/pursuits", s.listPursuits)
	mux.HandleFunc("POST /api/pursuits", s.activate)
	mux.HandleFunc("GET /api/pursuits/{id}", s.getPursuit)
	mux.HandleFunc("PUT /api/pursuits/{id}/steps/{step}", s.pursuitAction(func(r *http.Request, id int64, _ json.RawMessage) (*pipeline.Pursuit, error) {
		return s.Pipeline.Tick(r.Context(), id, r.PathValue("step"))
	}))
	mux.HandleFunc("DELETE /api/pursuits/{id}/steps/{step}", s.pursuitAction(func(r *http.Request, id int64, _ json.RawMessage) (*pipeline.Pursuit, error) {
		return s.Pipeline.Untick(r.Context(), id, r.PathValue("step"))
	}))
	mux.HandleFunc("POST /api/pursuits/{id}/applied", s.pursuitAction(func(r *http.Request, id int64, _ json.RawMessage) (*pipeline.Pursuit, error) {
		return s.Pipeline.Applied(r.Context(), id)
	}))
	mux.HandleFunc("POST /api/pursuits/{id}/followed-up", s.pursuitAction(func(r *http.Request, id int64, _ json.RawMessage) (*pipeline.Pursuit, error) {
		return s.Pipeline.FollowedUp(r.Context(), id)
	}))
	mux.HandleFunc("POST /api/pursuits/{id}/keep-waiting", s.pursuitAction(func(r *http.Request, id int64, _ json.RawMessage) (*pipeline.Pursuit, error) {
		return s.Pipeline.KeepWaiting(r.Context(), id)
	}))
	mux.HandleFunc("POST /api/pursuits/{id}/reopen", s.pursuitAction(func(r *http.Request, id int64, _ json.RawMessage) (*pipeline.Pursuit, error) {
		return s.Pipeline.Reopen(r.Context(), id)
	}))
	mux.HandleFunc("POST /api/pursuits/{id}/interview", s.pursuitAction(s.interview))
	mux.HandleFunc("POST /api/pursuits/{id}/complete", s.pursuitAction(func(r *http.Request, id int64, raw json.RawMessage) (*pipeline.Pursuit, error) {
		var body struct{ Outcome, Note string }
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, badRequest("invalid JSON")
		}
		return s.Pipeline.Complete(r.Context(), id, body.Outcome, strings.TrimSpace(body.Note))
	}))
	mux.HandleFunc("POST /api/pursuits/{id}/notes", s.pursuitAction(func(r *http.Request, id int64, raw json.RawMessage) (*pipeline.Pursuit, error) {
		var body struct{ Text string }
		if err := json.Unmarshal(raw, &body); err != nil || strings.TrimSpace(body.Text) == "" {
			return nil, badRequest("write a note")
		}
		return s.Pipeline.AddNote(r.Context(), id, strings.TrimSpace(body.Text))
	}))
	mux.HandleFunc("POST /api/pursuits/{id}/drafts", s.startDraft)

	mux.HandleFunc("GET /api/drafts/{id}", s.getDraft)
	mux.HandleFunc("GET /api/drafts/{id}/file", s.draftFile)

	mux.HandleFunc("GET /api/options", s.options)
	mux.HandleFunc("GET /api/items/{id}", s.getItem)
	mux.HandleFunc("PUT /api/items/{id}/feedback", s.feedback)
}

// --- JSON shapes ---

type stepJSON struct {
	Key    string     `json:"key"`
	Label  string     `json:"label"`
	DoneAt *time.Time `json:"doneAt"`
}

type pursuitJSON struct {
	ID            int64      `json:"id"`
	Ref           string     `json:"ref"`
	ItemID        *int64     `json:"itemId"`
	Company       string     `json:"company"`
	Title         string     `json:"title"`
	URL           string     `json:"url"`
	Stage         string     `json:"stage"`
	Outcome       string     `json:"outcome"`
	StageReached  string     `json:"stageReached"`
	Notes         string     `json:"notes"`
	ActivatedAt   time.Time  `json:"activatedAt"`
	AppliedAt     *time.Time `json:"appliedAt"`
	InterviewAt   *time.Time `json:"interviewAt"`
	FollowUpAt    *time.Time `json:"followUpAt"`
	GhostCheckAt  *time.Time `json:"ghostCheckAt"`
	CompletedAt   *time.Time `json:"completedAt"`
	Fit           *int       `json:"fit"`
	Tier          string     `json:"tier"`
	Steps         []stepJSON `json:"steps"`
	NextStep      string     `json:"nextStep"`
	FollowUpDue   bool       `json:"followUpDue"`
	GhostCheckDue bool       `json:"ghostCheckDue"`
}

func (s *Server) pursuitJSON(p *pipeline.Pursuit) pursuitJSON {
	now := s.Clock.Now()
	out := pursuitJSON{ID: p.ID, Ref: p.Ref(), ItemID: p.ItemID, Company: p.Company, Title: p.Title, URL: p.URL, Stage: string(p.Stage),
		Outcome: p.Outcome, StageReached: p.StageReached, Notes: p.Notes, ActivatedAt: p.ActivatedAt, AppliedAt: p.AppliedAt,
		InterviewAt: p.InterviewAt, FollowUpAt: p.FollowUpAt, GhostCheckAt: p.GhostCheckAt, CompletedAt: p.CompletedAt, Fit: p.Fit, Tier: p.Tier,
		Steps: []stepJSON{}}
	for _, step := range p.VisibleSteps() {
		sj := stepJSON{Key: step.Key, Label: step.Label}
		if at, done := p.Done[step.Key]; done {
			sj.DoneAt = &at
		}
		out.Steps = append(out.Steps, sj)
	}
	if next := p.NextStep(); next != nil && p.Stage != pipeline.StageCompleted {
		out.NextStep = next.Label
	}
	if p.Stage == pipeline.StageWaiting {
		out.FollowUpDue = p.FollowUpAt != nil && !p.FollowUpAt.After(now)
		out.GhostCheckDue = p.GhostCheckAt != nil && !p.GhostCheckAt.After(now)
	}
	return out
}

type draftJSON struct {
	ID         int64      `json:"id"`
	PursuitID  *int64     `json:"pursuitId"`
	ItemID     *int64     `json:"itemId"`
	Kind       string     `json:"kind"`
	Title      string     `json:"title"`
	Status     string     `json:"status"`
	Text       string     `json:"text"`
	Error      string     `json:"error"`
	FileName   string     `json:"fileName"`
	CreatedAt  time.Time  `json:"createdAt"`
	FinishedAt *time.Time `json:"finishedAt"`
}

func toDraftJSON(d pipeline.Draft) draftJSON {
	return draftJSON{ID: d.ID, PursuitID: d.PursuitID, ItemID: d.ItemID, Kind: d.Kind, Title: d.Title, Status: d.Status, Text: d.Text,
		Error: d.Error, FileName: d.FileName, CreatedAt: d.CreatedAt, FinishedAt: d.FinishedAt}
}

type pursuitRefJSON struct {
	ID    int64  `json:"id"`
	Ref   string `json:"ref"`
	Stage string `json:"stage"`
}

type optionJSON struct {
	ID          int64           `json:"id"`
	Company     string          `json:"company"`
	Title       string          `json:"title"`
	Locations   string          `json:"locations"`
	URL         string          `json:"url"`
	Source      string          `json:"source"`
	Comp        string          `json:"comp"`
	PostedAt    *time.Time      `json:"postedAt"`
	FirstSeenAt time.Time       `json:"firstSeenAt"`
	Tier        string          `json:"tier"`
	Category    string          `json:"category"`
	Fit         *int            `json:"fit"`
	RankScore   int             `json:"rankScore"`
	Priority    string          `json:"priority"`
	Summary     string          `json:"summary"`
	Reasons     []string        `json:"reasons"`
	Gaps        []string        `json:"gaps"`
	Feedback    *int            `json:"feedback"`
	Sent        bool            `json:"sent"`
	Pursuit     *pursuitRefJSON `json:"pursuit"`
}

func toOptionJSON(o radar.Option, p *pipeline.Pursuit) optionJSON {
	out := optionJSON{ID: o.ID, Company: o.Company, Title: o.Title, Locations: o.Locations, URL: o.URL, Source: o.Source,
		Comp: radar.Comp(o.CompMin, o.CompMax), PostedAt: o.PostedAt, FirstSeenAt: o.FirstSeenAt, Tier: o.Tier, Category: o.Category,
		Fit: o.FitScore, RankScore: o.RankScore, Priority: priorityName(o.Priority), Summary: o.FitSummary,
		Reasons: lines(o.FitReasons), Gaps: lines(o.FitGaps), Feedback: o.Feedback, Sent: o.Sent}
	if p != nil {
		out.Pursuit = &pursuitRefJSON{ID: p.ID, Ref: p.Ref(), Stage: string(p.Stage)}
	}
	return out
}

func priorityName(p radar.Priority) string {
	switch p {
	case radar.PriorityHigh:
		return "high"
	case radar.PriorityLow:
		return "low"
	}
	return "normal"
}

func lines(s string) []string {
	out := []string{}
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// --- handlers ---

func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	active, err := s.Pipeline.List(ctx, pipeline.StageActive)
	if err != nil {
		serverError(w, err)
		return
	}
	waiting, err := s.Pipeline.List(ctx, pipeline.StageWaiting)
	if err != nil {
		serverError(w, err)
		return
	}
	completed, err := s.Pipeline.List(ctx, pipeline.StageCompleted)
	if err != nil {
		serverError(w, err)
		return
	}
	type todoJSON struct {
		Kind     string      `json:"kind"`
		Pursuit  pursuitJSON `json:"pursuit"`
		Label    string      `json:"label"`
		PrepDone bool        `json:"prepDone"`
	}
	todos := []todoJSON{}
	for _, t := range s.Pipeline.Todos(active, waiting) {
		todos = append(todos, todoJSON{Kind: t.Kind, Pursuit: s.pursuitJSON(t.Pursuit), Label: t.Label, PrepDone: t.PrepDone})
	}
	outcomes, reached := map[string]int{}, map[string]int{}
	for _, p := range completed {
		outcomes[p.Outcome]++
		reached[p.StageReached]++
	}
	// Open pursuits count toward the funnel by how far they've got.
	for _, p := range slices.Concat(active, waiting) {
		switch {
		case p.InterviewAt != nil:
			reached["interview"]++
		case p.AppliedAt != nil:
			reached["applied"]++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"counts":  map[string]int{"active": len(active), "waiting": len(waiting), "completed": len(completed)},
		"softCap": s.Pipeline.SoftCap,
		"paused":  s.State.Paused(),
		"todos":   todos,
		"funnel": map[string]int{
			"applied":     reached["applied"] + reached["interview"] + reached["offer"],
			"interviewed": reached["interview"] + reached["offer"],
			"offers":      reached["offer"],
		},
		"outcomes": outcomes,
	})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sources, err := s.Catalog.Sources(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	type sourceJSON struct {
		Name     string     `json:"name"`
		Ats      string     `json:"ats"`
		Board    string     `json:"board"`
		Priority string     `json:"priority"`
		LastRun  *time.Time `json:"lastRun"`
		Fetched  int        `json:"fetched"`
		New      int        `json:"new"`
		Error    string     `json:"error"`
	}
	out := []sourceJSON{}
	for _, src := range sources {
		out = append(out, sourceJSON{src.Name, src.Ats, src.Board, src.Priority, src.LastRun, src.Fetched, src.New, src.Error})
	}
	type usageJSON struct {
		Model        string `json:"model"`
		Calls        int64  `json:"calls"`
		InputTokens  int64  `json:"inputTokens"`
		OutputTokens int64  `json:"outputTokens"`
		CachedTokens int64  `json:"cachedTokens"`
	}
	usage := []usageJSON{}
	if s.Usage != nil {
		for _, u := range s.Usage.Snapshot() {
			usage = append(usage, usageJSON{u.Model, u.Calls, u.InputTokens, u.OutputTokens, u.CachedTokens})
		}
	}
	scoring := map[string]any{"running": false, "pending": 0}
	if s.Scoring != nil {
		pending, err := s.Scoring.CountPending(ctx)
		if err != nil {
			serverError(w, err)
			return
		}
		scoring = map[string]any{"running": s.Scoring.IsRunning(), "pending": pending}
	}
	writeJSON(w, http.StatusOK, map[string]any{"paused": s.State.Paused(), "startedAt": s.State.StartedAt, "sources": out,
		"usage": usage, "scoring": scoring})
}

func (s *Server) setPaused(paused bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if paused {
			s.State.Pause()
			slog.Info("paused from the dashboard")
		} else {
			s.State.Resume()
			slog.Info("resumed from the dashboard")
		}
		writeJSON(w, http.StatusOK, map[string]bool{"paused": s.State.Paused()})
	}
}

func (s *Server) listPursuits(w http.ResponseWriter, r *http.Request) {
	stage := pipeline.Stage(r.URL.Query().Get("stage"))
	if !slices.Contains([]pipeline.Stage{pipeline.StageActive, pipeline.StageWaiting, pipeline.StageCompleted}, stage) {
		writeError(w, http.StatusBadRequest, "stage must be active, waiting or completed")
		return
	}
	ps, err := s.Pipeline.List(r.Context(), stage)
	if err != nil {
		serverError(w, err)
		return
	}
	out := []pursuitJSON{}
	for _, p := range ps {
		out = append(out, s.pursuitJSON(p))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) activate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ItemID              *int64 `json:"itemId"`
		URL, Company, Title string
	}
	if !readJSON(w, r, &body) {
		return
	}
	ctx := r.Context()
	var (
		p       *pipeline.Pursuit
		created = true
		err     error
	)
	switch {
	case body.ItemID != nil:
		p, created, err = s.Pipeline.ActivateItem(ctx, *body.ItemID)
	case strings.TrimSpace(body.Company) != "" && strings.TrimSpace(body.Title) != "":
		url := strings.TrimSpace(body.URL)
		if url != "" && !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
			writeError(w, http.StatusBadRequest, "the link must start with https://")
			return
		}
		p, err = s.Pipeline.ActivateManual(ctx, url, strings.TrimSpace(body.Company), strings.TrimSpace(body.Title))
	default:
		writeError(w, http.StatusBadRequest, "give an item id, or a company and title")
		return
	}
	if err != nil {
		actionError(w, err)
		return
	}
	warning := ""
	if n, err := s.Pipeline.CountActive(ctx); err == nil && created && s.Pipeline.SoftCap > 0 && n > s.Pipeline.SoftCap {
		warning = fmt.Sprintf("That's %d active (soft cap %d). Finish or drop one before adding more.", n, s.Pipeline.SoftCap)
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"pursuit": s.pursuitJSON(p), "created": created, "warning": warning})
}

func (s *Server) getPursuit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	p, err := s.Pipeline.Get(ctx, id)
	if err != nil {
		actionError(w, err)
		return
	}
	events, err := s.Pipeline.Events(ctx, id)
	if err != nil {
		serverError(w, err)
		return
	}
	drafts, err := s.Drafts.ForPursuit(ctx, p)
	if err != nil {
		serverError(w, err)
		return
	}
	type eventJSON struct {
		At     time.Time `json:"at"`
		Kind   string    `json:"kind"`
		Detail string    `json:"detail"`
	}
	ev := []eventJSON{}
	for _, e := range events {
		ev = append(ev, eventJSON{e.At, e.Kind, e.Detail})
	}
	dr := []draftJSON{}
	for _, d := range drafts {
		d.Text = "" // the list shows titles; GET /api/drafts/{id} has the text
		dr = append(dr, toDraftJSON(d))
	}
	writeJSON(w, http.StatusOK, map[string]any{"pursuit": s.pursuitJSON(p), "events": ev, "drafts": dr, "outcomes": pipeline.Outcomes})
}

func (s *Server) interview(r *http.Request, id int64, raw json.RawMessage) (*pipeline.Pursuit, error) {
	var body struct{ At, Note string }
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, badRequest("invalid JSON")
	}
	at, err := time.Parse(time.RFC3339, body.At)
	if err != nil {
		// <input type="datetime-local"> sends local time without a zone: the owner's zone.
		if at, err = time.ParseInLocation("2006-01-02T15:04", body.At, s.Pipeline.Location); err != nil {
			return nil, badRequest("give the interview time as 2026-11-03T14:00")
		}
	}
	note := strings.TrimSpace(body.Note)
	p, err := s.Pipeline.Interview(r.Context(), id, at, note)
	if err != nil || note == "" {
		return p, err
	}
	return s.Pipeline.AddNote(r.Context(), id, "Interview: "+note)
}

// pursuitAction wraps a pursuit change: parse the id and body, apply, reply with the updated pursuit.
func (s *Server) pursuitAction(f func(r *http.Request, id int64, body json.RawMessage) (*pipeline.Pursuit, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		var raw json.RawMessage
		if r.ContentLength != 0 && r.Method != http.MethodDelete {
			if !readJSON(w, r, &raw) {
				return
			}
		}
		if raw == nil {
			raw = json.RawMessage("{}")
		}
		p, err := f(r, id, raw)
		if err != nil {
			actionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, s.pursuitJSON(p))
	}
}

func (s *Server) startDraft(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct{ Kind, Notes string }
	if !readJSON(w, r, &body) {
		return
	}
	ctx := r.Context()
	p, err := s.Pipeline.Get(ctx, id)
	if err != nil {
		actionError(w, err)
		return
	}
	var d pipeline.Draft
	switch body.Kind {
	case "kit":
		d, err = s.Drafts.StartKit(ctx, p, nil, nil)
	case "prep":
		d, err = s.Drafts.StartPrep(ctx, p, body.Notes, nil)
	case "thanks":
		d, err = s.Drafts.StartThanks(ctx, p, body.Notes, nil)
	default:
		writeError(w, http.StatusBadRequest, "kind must be kit, prep or thanks")
		return
	}
	if err != nil {
		actionError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, toDraftJSON(d))
}

func (s *Server) getDraft(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	d, err := s.Drafts.Get(r.Context(), id)
	if err != nil {
		actionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDraftJSON(d))
}

func (s *Server) draftFile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	name, data, err := s.Drafts.File(r.Context(), id)
	if err != nil {
		actionError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", safeFileName(name)))
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(data)
}

func safeFileName(name string) string {
	clean := strings.Map(func(r rune) rune {
		if r < 0x20 || r == '"' || r == '\\' || r == '/' {
			return '_'
		}
		return r
	}, name)
	if clean == "" {
		return "resume.docx"
	}
	return clean
}

func (s *Server) options(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := radar.OptionsQuery{Search: q.Get("q"), Tier: q.Get("tier"), Category: q.Get("category"), Limit: 200}
	if v := q.Get("minScore"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "minScore must be a number")
			return
		}
		query.MinScore = &n
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			query.Limit = n
		}
	}
	ctx := r.Context()
	page, err := s.Catalog.Options(ctx, query)
	if err != nil {
		serverError(w, err)
		return
	}
	ids := make([]int64, len(page.Options))
	for i, o := range page.Options {
		ids[i] = o.ID
	}
	pursuits, err := s.Pipeline.ForItems(ctx, ids)
	if err != nil {
		serverError(w, err)
		return
	}
	options := []optionJSON{}
	for _, o := range page.Options {
		options = append(options, toOptionJSON(o, pursuits[o.ID]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"options": options, "total": page.Total, "tiers": nonNil(page.Tiers),
		"categories": nonNil(page.Categories), "unscored": page.Unscored, "defaultMinScore": s.Catalog.MinScore})
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (s *Server) getItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	item, err := s.Catalog.Item(ctx, id)
	if err != nil {
		actionError(w, err)
		return
	}
	pursuits, err := s.Pipeline.ForItems(ctx, []int64{id})
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": toOptionJSON(item.Option, pursuits[id]), "description": item.Description})
}

func (s *Server) feedback(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct{ Value int }
	if !readJSON(w, r, &body) {
		return
	}
	if body.Value != 1 && body.Value != -1 {
		writeError(w, http.StatusBadRequest, "value must be 1 or -1")
		return
	}
	n, err := s.Feedback.Set(r.Context(), []int64{id}, body.Value)
	if err != nil {
		serverError(w, err)
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"feedback": body.Value})
}

// --- plumbing ---

type badRequest string

func (b badRequest) Error() string { return string(b) }

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "bad id")
		return 0, false
	}
	return id, true
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("writing response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// actionError: not found is 404; anything else from a pipeline change is the owner asking for something that
// can't be done right now (completing twice, an unknown outcome), so it's shown as the reason.
func actionError(w http.ResponseWriter, err error) {
	var bad badRequest
	switch {
	case errors.Is(err, pipeline.ErrNotFound), errors.Is(err, radar.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, pipeline.ErrPaused):
		writeError(w, http.StatusConflict, "Paused. Resume first.")
	case errors.As(err, &bad):
		writeError(w, http.StatusBadRequest, bad.Error())
	default:
		slog.Warn("dashboard action refused", "err", err)
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func serverError(w http.ResponseWriter, err error) {
	slog.Error("dashboard request failed", "err", err)
	writeError(w, http.StatusInternalServerError, "something broke; see the logs")
}
