// Package devbox runs the dashboard's Code page (docs/dev-box.md): which of the owner's repos have a Claude Code
// session open on the dev box, and opening one there, waking the VM first when it's asleep.
//
// It talks to two things: the dev box's agent (deploy/dev/box/bin/dev-agent) over the tailnet, and Azure, as the bot's
// managed identity, which may read and start that one VM and nothing more. Where they are comes from the dev_box
// entry in the data pack's code/repos.yaml, so a bot whose pack has none has no Code page.
package devbox

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ahpalmer/consigliere/internal/azure"
	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/datapack"
)

const reposFile = "code/repos.yaml"

// Modes a session starts in: the repo's last conversation, or a new one.
const (
	Continue = "continue"
	New      = "new"
)

var ErrUnknownRepo = errors.New("no such repo in code/repos.yaml")

// AgentError is the agent's refusal, e.g. a session already open for "continue". Its message is for the owner.
type AgentError struct {
	Status  int
	Message string
}

func (e *AgentError) Error() string { return e.Message }

type Box struct {
	Pack   *datapack.Pack
	Tokens azure.TokenSource
	HTTP   *http.Client
	// Azure Resource Manager; defaults to https://management.azure.com.
	ARM string
	// How long a woken VM gets to answer, and how often to ask. Default 6 minutes and 5 seconds.
	WakeTimeout, PollEvery time.Duration
	// Where the last copy of each repo's plan is kept for while the box sleeps (optional), and the clock.
	DB    *sql.DB
	Clock clock.Clock

	mu sync.Mutex
	// Sessions asked for while the VM was asleep, by repo; dropped once opened, kept with Error when that failed.
	waiting map[string]*waiting
}

type waiting struct {
	Mode  string
	Error string
}

type settings struct {
	Agent string // the agent's base URL on the tailnet
	VM    string // the VM's Azure resource ID
	Repos []string
	// A repo's plan file when it isn't docs/roadmap.md, by repo.
	Plans map[string]string
	// Put the VM to sleep after this long with no session output and no one logged in; 0 never.
	SleepAfter time.Duration
}

// Status is what the Code page shows.
type Status struct {
	Enabled bool `json:"enabled"`
	// running (the agent answers), booting (Azure says running, the agent doesn't answer yet), starting, asleep,
	// stopping, or unknown.
	VM      string `json:"vm"`
	VMError string `json:"vmError"`
	Repos   []Repo `json:"repos"`
}

type Repo struct {
	Name     string    `json:"name"`
	Sessions []Session `json:"sessions"`
	// From the dev box's clone, while it's up: the checked-out branch, uncommitted files, commits ahead of and behind
	// its upstream (nil without one), the last commit, and open PRs (nil when GitHub didn't answer).
	Branch     string  `json:"branch"`
	Changes    int     `json:"changes"`
	Ahead      *int    `json:"ahead"`
	Behind     *int    `json:"behind"`
	LastCommit *Commit `json:"lastCommit"`
	PRs        []PR    `json:"prs"`
	Known      bool    `json:"known"` // false: the box is asleep or the repo isn't cloned yet, so the fields above are empty
	// A session asked for while the VM was asleep: "continue" or "new" while it wakes, with Error if that failed.
	Waiting string `json:"waiting"`
	Error   string `json:"error"`
}

type Commit struct {
	Subject string    `json:"subject"`
	At      time.Time `json:"at"`
}

type PR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Branch string `json:"branch"`
	Draft  bool   `json:"draft"`
}

type Session struct {
	Name      string    `json:"name"`
	Repo      string    `json:"repo"`
	StartedAt time.Time `json:"startedAt"`
}

// Started is the outcome of Start: the new session, or Waking when it opens once the VM is up.
type Started struct {
	Session string `json:"session"`
	Waking  bool   `json:"waking"`
}

func (b *Box) settings() (settings, bool, error) {
	text, found, err := b.Pack.ReadText(reposFile)
	if err != nil || !found {
		return settings{}, false, err
	}
	var doc struct {
		DevBox struct {
			Agent string `yaml:"agent"`
			VM    string `yaml:"vm"`
			// Hours; missing means 3, 0 means never.
			SleepAfterHours *float64 `yaml:"sleep_after_hours"`
		} `yaml:"dev_box"`
		Repos []struct {
			Name string `yaml:"name"`
			Plan string `yaml:"plan"`
		} `yaml:"repos"`
	}
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return settings{}, false, fmt.Errorf("%s: %w", reposFile, err)
	}
	if doc.DevBox.Agent == "" && doc.DevBox.VM == "" {
		return settings{}, false, nil
	}
	if u, err := url.Parse(doc.DevBox.Agent); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return settings{}, false, fmt.Errorf("%s: dev_box.agent must be the agent's URL, e.g. http://<dev box>:8790", reposFile)
	}
	if !strings.HasPrefix(doc.DevBox.VM, "/subscriptions/") {
		return settings{}, false, fmt.Errorf("%s: dev_box.vm must be the VM's resource ID, /subscriptions/...", reposFile)
	}
	s := settings{Agent: strings.TrimRight(doc.DevBox.Agent, "/"), VM: doc.DevBox.VM, SleepAfter: 3 * time.Hour}
	if h := doc.DevBox.SleepAfterHours; h != nil {
		s.SleepAfter = time.Duration(max(*h, 0) * float64(time.Hour))
	}
	s.Plans = map[string]string{}
	for _, r := range doc.Repos {
		if r.Name != "" {
			s.Repos = append(s.Repos, r.Name)
			if r.Plan != "" {
				s.Plans[r.Name] = r.Plan
			}
		}
	}
	return s, true, nil
}

// Enabled reports whether the data pack sets up a dev box.
func (b *Box) Enabled() bool {
	_, ok, err := b.settings()
	return ok && err == nil
}

func (b *Box) Status(ctx context.Context) (Status, error) {
	s, ok, err := b.settings()
	if err != nil || !ok {
		return Status{}, err
	}
	out := Status{Enabled: true, Repos: []Repo{}}
	sessions, err := b.sessions(ctx, s)
	var refused *replyError
	switch {
	case err == nil:
		out.VM = "running"
	case errors.As(err, &refused):
		out.VM, out.VMError = "running", "The dev box answered: "+refused.Message
	default:
		power, perr := b.power(ctx, s)
		if perr != nil {
			out.VM, out.VMError = "unknown", fmt.Sprintf("The dev box doesn't answer (%v), and Azure can't say why (%v).", err, perr)
			break
		}
		out.VM = power // starting, stopping, deallocating
		if name, ok := map[string]string{"running": "booting", "deallocated": "asleep", "stopped": "asleep"}[power]; ok {
			out.VM = name
		}
	}
	var details map[string]Repo
	if out.VM == "running" && out.VMError == "" {
		details, _ = b.repos(ctx, s) // the page still works without them
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, name := range s.Repos {
		r := Repo{Name: name, Sessions: []Session{}}
		if d, ok := details[name]; ok {
			r.Branch, r.Changes, r.Ahead, r.Behind, r.LastCommit, r.PRs, r.Known = d.Branch, d.Changes, d.Ahead, d.Behind, d.LastCommit, d.PRs, true
		}
		for _, ses := range sessions {
			if ses.Repo == name {
				r.Sessions = append(r.Sessions, ses)
			}
		}
		if w := b.waiting[name]; w != nil {
			r.Waiting, r.Error = w.Mode, w.Error
		}
		out.Repos = append(out.Repos, r)
	}
	return out, nil
}

// Start opens a session for repo. When the agent doesn't answer it starts the VM (if it isn't already) and opens
// the session in the background once the agent is up; Status shows the progress.
func (b *Box) Start(ctx context.Context, repo, mode string) (Started, error) {
	if mode != Continue && mode != New {
		return Started{}, fmt.Errorf("mode must be %q or %q", Continue, New)
	}
	s, ok, err := b.settings()
	if err != nil {
		return Started{}, err
	}
	if !ok || !slices.Contains(s.Repos, repo) {
		return Started{}, ErrUnknownRepo
	}
	b.mu.Lock()
	if w := b.waiting[repo]; w != nil && w.Error == "" {
		b.mu.Unlock()
		return Started{Waking: true}, nil // already on it
	}
	delete(b.waiting, repo)
	b.mu.Unlock()

	name, err := b.open(ctx, s, repo, mode)
	var refused *AgentError
	if err == nil || errors.As(err, &refused) {
		return Started{Session: name}, err
	}
	// The agent didn't answer: the VM is asleep or still booting.
	power, err := b.power(ctx, s)
	if err != nil {
		return Started{}, fmt.Errorf("the dev box doesn't answer, and Azure can't say why: %w", err)
	}
	if power != "running" && power != "starting" {
		if err := b.startVM(ctx, s); err != nil {
			return Started{}, err
		}
	}
	b.mu.Lock()
	if b.waiting == nil {
		b.waiting = map[string]*waiting{}
	}
	w := &waiting{Mode: mode}
	b.waiting[repo] = w
	b.mu.Unlock()
	go b.openWhenAwake(s, repo, w)
	return Started{Waking: true}, nil
}

func (b *Box) openWhenAwake(s settings, repo string, w *waiting) {
	timeout, every := b.WakeTimeout, b.PollEvery
	if timeout == 0 {
		timeout = 6 * time.Minute
	}
	if every == 0 {
		every = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	fail := func(msg string) {
		b.mu.Lock()
		w.Error = msg
		b.mu.Unlock()
	}
	for {
		select {
		case <-ctx.Done():
			fail(fmt.Sprintf("The dev box didn't answer within %v of waking. Try again, or check it with deploy/dev.sh.", timeout))
			return
		case <-time.After(every):
		}
		if _, err := b.sessions(ctx, s); err != nil {
			continue
		}
		if _, err := b.open(ctx, s, repo, w.Mode); err != nil {
			fail(err.Error())
			return
		}
		b.mu.Lock()
		if b.waiting[repo] == w {
			delete(b.waiting, repo)
		}
		b.mu.Unlock()
		return
	}
}

// Stop ends a session; its conversation stays on disk for the next Continue Coding.
func (b *Box) Stop(ctx context.Context, name string) error {
	s, ok, err := b.settings()
	if err != nil {
		return err
	}
	if !ok {
		return ErrUnknownRepo
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err = b.call(ctx, http.MethodDelete, s.Agent+"/sessions/"+url.PathEscape(name), nil, nil, nil)
	var refused *replyError
	if errors.As(err, &refused) {
		return &AgentError{Status: refused.Status, Message: refused.Message}
	}
	return err
}

// Slept is what SleepIfIdle did: put the VM to sleep after it was idle this long, or nothing (zero).
type Slept struct{ IdleFor time.Duration }

// SleepIfIdle deallocates the VM when its agent says nothing has happened for the data pack's sleep_after_hours: no
// session printing anything and no one logged in. A box that doesn't answer is left alone (asleep, or booting).
func (b *Box) SleepIfIdle(ctx context.Context, now time.Time) (Slept, error) {
	s, ok, err := b.settings()
	if err != nil || !ok || s.SleepAfter <= 0 {
		return Slept{}, err
	}
	b.mu.Lock()
	waking := len(b.waiting) > 0
	b.mu.Unlock()
	if waking {
		return Slept{}, nil // a session was just asked for
	}
	actx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var activity struct {
		IdleSince time.Time `json:"idleSince"`
	}
	if err := b.call(actx, http.MethodGet, s.Agent+"/activity", nil, nil, &activity); err != nil || activity.IdleSince.IsZero() {
		return Slept{}, nil
	}
	idle := now.Sub(activity.IdleSince)
	if idle < s.SleepAfter {
		return Slept{}, nil
	}
	token, err := b.Tokens.Token(ctx, armScope)
	if err != nil {
		return Slept{}, err
	}
	if err := b.call(ctx, http.MethodPost, b.armURL(s, "/deallocate"), &token, nil, nil); err != nil {
		var refused *replyError
		if errors.As(err, &refused) && refused.Status == http.StatusForbidden {
			return Slept{}, errors.New("the bot may not put the dev box to sleep yet: redeploy infra/dev.bicep to give it the deallocate permission")
		}
		return Slept{}, fmt.Errorf("put the dev box to sleep: %w", err)
	}
	return Slept{IdleFor: idle}, nil
}

// --- the agent ---

// repos asks the agent for each repo's branch, changes and PRs; it can take a few seconds when PRs aren't cached.
func (b *Box) repos(ctx context.Context, s settings) (map[string]Repo, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	var out struct{ Repos []Repo }
	if err := b.call(ctx, http.MethodGet, s.Agent+"/repos", nil, nil, &out); err != nil {
		return nil, err
	}
	byName := map[string]Repo{}
	for _, r := range out.Repos {
		byName[r.Name] = r
	}
	return byName, nil
}

func (b *Box) sessions(ctx context.Context, s settings) ([]Session, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var out struct{ Sessions []Session }
	if err := b.call(ctx, http.MethodGet, s.Agent+"/sessions", nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Sessions, nil
}

// open asks the agent for a session; an *AgentError means it answered and refused.
func (b *Box) open(ctx context.Context, s settings, repo, mode string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second) // the agent waits a few seconds to see Claude stay up
	defer cancel()
	var out struct{ Name string }
	err := b.call(ctx, http.MethodPost, s.Agent+"/sessions", nil, map[string]string{"repo": repo, "mode": mode}, &out)
	var refused *replyError
	if errors.As(err, &refused) {
		return "", &AgentError{Status: refused.Status, Message: refused.Message}
	}
	return out.Name, err
}

// --- Azure ---

const armScope = "https://management.azure.com/.default"

func (b *Box) armURL(s settings, action string) string {
	base := b.ARM
	if base == "" {
		base = "https://management.azure.com"
	}
	return strings.TrimRight(base, "/") + s.VM + action + "?api-version=2024-07-01"
}

// power is the VM's power state as Azure names it: running, deallocated, starting, stopped...
func (b *Box) power(ctx context.Context, s settings) (string, error) {
	token, err := b.Tokens.Token(ctx, armScope)
	if err != nil {
		return "", err
	}
	var view struct{ Statuses []struct{ Code string } }
	if err := b.call(ctx, http.MethodGet, b.armURL(s, "/instanceView"), &token, nil, &view); err != nil {
		return "", fmt.Errorf("read the VM's state: %w", err)
	}
	for _, st := range view.Statuses {
		if state, ok := strings.CutPrefix(st.Code, "PowerState/"); ok {
			return state, nil
		}
	}
	return "unknown", nil
}

func (b *Box) startVM(ctx context.Context, s settings) error {
	token, err := b.Tokens.Token(ctx, armScope)
	if err != nil {
		return err
	}
	if err := b.call(ctx, http.MethodPost, b.armURL(s, "/start"), &token, nil, nil); err != nil {
		return fmt.Errorf("start the dev box: %w", err)
	}
	return nil
}

// replyError is an error status from the agent or Azure, with the reply's message.
type replyError struct {
	Status  int
	Message string
}

func (e *replyError) Error() string { return e.Message }

// call sends a request and decodes a JSON reply into out. An error status becomes a *replyError carrying the reply's
// "error" (the agent's) or "error.message" (Azure's).
func (b *Box) call(ctx context.Context, method, url string, token *string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != nil {
		req.Header.Set("Authorization", "Bearer "+*token)
	}
	client := b.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		var reply struct{ Error json.RawMessage }
		_ = json.Unmarshal(raw, &reply)
		var msg string
		var armErr struct{ Message string }
		if json.Unmarshal(reply.Error, &msg) != nil && json.Unmarshal(reply.Error, &armErr) == nil {
			msg = armErr.Message
		}
		if msg == "" {
			msg = resp.Status
		}
		return &replyError{Status: resp.StatusCode, Message: msg}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}
