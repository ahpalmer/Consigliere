package devbox

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ahpalmer/consigliere/internal/channel"
	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/datapack"
)

const vmID = "/subscriptions/sub-1/resourceGroups/rg-consigliere-dev/providers/Microsoft.Compute/virtualMachines/consigliere-dev"

type tokens struct{}

func (tokens) Token(_ context.Context, scope string) (string, error) {
	if scope != armScope {
		return "", errors.New("unexpected scope " + scope)
	}
	return "arm-token", nil
}

// fakeBox is the dev box's agent and Azure's view of its VM.
type fakeBox struct {
	mu       sync.Mutex
	up       bool   // the agent answers
	power    string // Azure's PowerState
	starts   int    // VM starts asked for
	sessions []map[string]string
	opened   []string // "repo mode"
	// M19: what /activity says, Azure deallocations, and whether the bot's role allows them.
	idleSince    string
	deallocates  int
	noDeallocate bool
}

func (f *fakeBox) agent(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if !f.up {
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			conn.Close() // like a VM that isn't there
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /sessions":
			_ = json.NewEncoder(w).Encode(map[string]any{"sessions": f.sessions})
		case "GET /repos":
			_, _ = w.Write([]byte(`{"repos":[{"name":"Consigliere","branch":"feature/x","changes":2,"ahead":1,"behind":0,
				"lastCommit":{"subject":"Add things","at":"2026-11-20T10:00:00-05:00"},
				"prs":[{"number":7,"title":"M15","url":"https://github.com/o/Consigliere/pull/7","branch":"feature/x","draft":false}]},
				{"name":"Unlisted","branch":"main","changes":0,"ahead":null,"behind":null,"lastCommit":null,"prs":null}]}`))
		case "GET /activity":
			_ = json.NewEncoder(w).Encode(map[string]any{"idleSince": f.idleSince, "sessions": len(f.sessions), "logins": 0})
		case "DELETE /sessions/Consigliere-1":
			for i, s := range f.sessions {
				if s["name"] == "Consigliere-1" {
					f.sessions = append(f.sessions[:i], f.sessions[i+1:]...)
					_ = json.NewEncoder(w).Encode(map[string]string{"stopped": "Consigliere-1"})
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "no session Consigliere-1"})
		case "POST /sessions":
			var body struct{ Repo, Mode string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			for _, s := range f.sessions {
				if body.Mode == Continue && s["repo"] == body.Repo {
					w.WriteHeader(http.StatusConflict)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": body.Repo + " already has a session open"})
					return
				}
			}
			name := body.Repo + "-1"
			f.sessions = append(f.sessions, map[string]string{"name": name, "repo": body.Repo, "startedAt": "2026-11-18T12:00:00Z"})
			f.opened = append(f.opened, body.Repo+" "+body.Mode)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"name": name, "repo": body.Repo})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeBox) arm(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer arm-token" || r.URL.Query().Get("api-version") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"AuthenticationFailed","message":"no token"}}`))
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET " + vmID + "/instanceView":
			_ = json.NewEncoder(w).Encode(map[string]any{"statuses": []map[string]string{
				{"code": "ProvisioningState/succeeded"}, {"code": "PowerState/" + f.power}}})
		case "POST " + vmID + "/start":
			f.starts++
			f.power = "starting"
			w.WriteHeader(http.StatusAccepted)
		case "POST " + vmID + "/deallocate":
			if f.noDeallocate {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":{"code":"AuthorizationFailed","message":"no deallocate"}}`))
				return
			}
			f.deallocates++
			f.power = "deallocating"
			f.up = false
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newBox(t *testing.T, f *fakeBox, reposYAML string) *Box {
	t.Helper()
	root := t.TempDir()
	if reposYAML != "" {
		if err := os.MkdirAll(filepath.Join(root, "code"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "code", "repos.yaml"), []byte(reposYAML), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pack, err := datapack.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return &Box{Pack: pack, Tokens: tokens{}, ARM: f.arm(t).URL, PollEvery: 10 * time.Millisecond, WakeTimeout: 5 * time.Second}
}

func reposYAML(agentURL string) string {
	return "dev_box:\n  agent: " + agentURL + "\n  vm: " + vmID + "\n" +
		"repos:\n  - { name: Consigliere, url: https://github.com/o/Consigliere.git }\n" +
		"  - { name: Consigliere-data, url: https://github.com/o/Consigliere-data.git, protect_main: true }\n"
}

func TestNoDevBoxMeansNoCodePage(t *testing.T) {
	f := &fakeBox{}
	for _, yaml := range []string{"", "repos:\n  - { name: A, url: https://github.com/o/A.git }\n"} {
		box := newBox(t, f, yaml)
		if box.Enabled() {
			t.Fatalf("enabled with repos.yaml %q", yaml)
		}
		if st, err := box.Status(context.Background()); err != nil || st.Enabled {
			t.Fatalf("status = %+v, %v", st, err)
		}
	}
}

func TestStatusListsSessionsByRepo(t *testing.T) {
	f := &fakeBox{up: true, power: "running", sessions: []map[string]string{
		{"name": "Consigliere-1", "repo": "Consigliere", "startedAt": "2026-11-18T12:00:00Z"},
		{"name": "Consigliere-2", "repo": "Consigliere", "startedAt": "2026-11-18T13:00:00Z"},
		{"name": "Other-1", "repo": "Other", "startedAt": "2026-11-18T13:00:00Z"}, // not in repos.yaml
	}}
	box := newBox(t, f, reposYAML(f.agent(t).URL))

	st, err := box.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Enabled || st.VM != "running" || len(st.Repos) != 2 {
		t.Fatalf("status = %+v", st)
	}
	if got := st.Repos[0]; got.Name != "Consigliere" || len(got.Sessions) != 2 || got.Sessions[1].Name != "Consigliere-2" {
		t.Fatalf("Consigliere = %+v", got)
	}
	if got := st.Repos[1]; got.Name != "Consigliere-data" || len(got.Sessions) != 0 {
		t.Fatalf("Consigliere-data = %+v", got)
	}
}

func TestStatusWhileAsleepAsksAzure(t *testing.T) {
	f := &fakeBox{power: "deallocated"}
	box := newBox(t, f, reposYAML(f.agent(t).URL))
	st, err := box.Status(context.Background())
	if err != nil || st.VM != "asleep" || len(st.Repos) != 2 {
		t.Fatalf("status = %+v, %v", st, err)
	}
	f.power = "running" // booting: Azure says running, the agent isn't up yet
	if st, _ := box.Status(context.Background()); st.VM != "booting" {
		t.Fatalf("vm = %q, want booting", st.VM)
	}
}

func TestStartOpensASessionWhenTheBoxIsUp(t *testing.T) {
	f := &fakeBox{up: true, power: "running"}
	box := newBox(t, f, reposYAML(f.agent(t).URL))

	got, err := box.Start(context.Background(), "Consigliere", Continue)
	if err != nil || got.Session != "Consigliere-1" || got.Waking {
		t.Fatalf("start = %+v, %v", got, err)
	}
	// A second "continue" would fork the open conversation: the agent refuses, and the reason comes back.
	_, err = box.Start(context.Background(), "Consigliere", Continue)
	var refused *AgentError
	if !errors.As(err, &refused) || refused.Status != http.StatusConflict {
		t.Fatalf("second continue = %v", err)
	}
	if f.starts != 0 {
		t.Fatalf("started the VM %d times while it was up", f.starts)
	}
}

func TestStartWakesTheVMThenOpensTheSession(t *testing.T) {
	f := &fakeBox{power: "deallocated"}
	box := newBox(t, f, reposYAML(f.agent(t).URL))

	got, err := box.Start(context.Background(), "Consigliere-data", New)
	if err != nil || !got.Waking {
		t.Fatalf("start = %+v, %v", got, err)
	}
	st, _ := box.Status(context.Background())
	if st.VM != "starting" || st.Repos[1].Waiting != New {
		t.Fatalf("while waking: %+v", st)
	}
	// Asking again while it wakes doesn't start it twice.
	if got, err := box.Start(context.Background(), "Consigliere-data", New); err != nil || !got.Waking {
		t.Fatalf("second start = %+v, %v", got, err)
	}

	f.mu.Lock()
	f.up, f.power = true, "running"
	f.mu.Unlock()
	deadline := time.Now().Add(3 * time.Second)
	for {
		st, _ = box.Status(context.Background())
		if len(st.Repos[1].Sessions) == 1 && st.Repos[1].Waiting == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("session never opened: %+v", st)
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.starts != 1 || len(f.opened) != 1 || f.opened[0] != "Consigliere-data new" {
		t.Fatalf("starts = %d, opened = %v", f.starts, f.opened)
	}
}

func TestStartRefusesUnknownReposAndModes(t *testing.T) {
	f := &fakeBox{up: true, power: "running"}
	box := newBox(t, f, reposYAML(f.agent(t).URL))
	if _, err := box.Start(context.Background(), "Elsewhere", New); !errors.Is(err, ErrUnknownRepo) {
		t.Fatalf("unknown repo = %v", err)
	}
	if _, err := box.Start(context.Background(), "Consigliere", "rm -rf"); err == nil {
		t.Fatal("accepted a made-up mode")
	}
	if len(f.opened) != 0 {
		t.Fatalf("opened %v", f.opened)
	}
}

func TestBadDevBoxSettingsAreReported(t *testing.T) {
	f := &fakeBox{}
	box := newBox(t, f, "dev_box:\n  agent: consigliere-dev:8790\n  vm: "+vmID+"\nrepos: []\n")
	if _, err := box.Status(context.Background()); err == nil {
		t.Fatal("accepted an agent address without a scheme")
	}
}

func TestStatusShowsEachReposBranchChangesAndPRs(t *testing.T) {
	f := &fakeBox{up: true, power: "running"}
	box := newBox(t, f, reposYAML(f.agent(t).URL))
	st, err := box.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c, data := st.Repos[0], st.Repos[1]
	if !c.Known || c.Branch != "feature/x" || c.Changes != 2 || *c.Ahead != 1 || len(c.PRs) != 1 || c.PRs[0].Number != 7 || c.LastCommit.Subject != "Add things" {
		t.Fatalf("%+v", c)
	}
	if data.Known || data.Name != "Consigliere-data" {
		t.Fatalf("a repo the agent didn't report: %+v", data)
	}

	// Asleep: no details, and the page still works.
	f.set(func() { f.up, f.power = false, "deallocated" })
	if st, err = box.Status(context.Background()); err != nil || st.Repos[0].Known {
		t.Fatalf("%+v %v", st, err)
	}
}

func TestStopEndsASession(t *testing.T) {
	f := &fakeBox{up: true, power: "running", sessions: []map[string]string{{"name": "Consigliere-1", "repo": "Consigliere", "startedAt": "2026-11-18T12:00:00Z"}}}
	box := newBox(t, f, reposYAML(f.agent(t).URL))
	if err := box.Stop(context.Background(), "Consigliere-1"); err != nil {
		t.Fatal(err)
	}
	var refused *AgentError
	if err := box.Stop(context.Background(), "Consigliere-1"); !errors.As(err, &refused) || refused.Status != http.StatusNotFound {
		t.Fatalf("stopping it again: %v", err)
	}
}

func (f *fakeBox) deallocated() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deallocates
}

func (f *fakeBox) set(change func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change()
}

func TestSleepsAfterTheIdleHours(t *testing.T) {
	now := time.Date(2026, 11, 21, 15, 0, 0, 0, time.UTC)
	f := &fakeBox{up: true, power: "running", idleSince: "2026-11-21T13:00:00Z"}
	agent := f.agent(t).URL
	box := newBox(t, f, reposYAML(agent))
	ctx := context.Background()

	if slept, err := box.SleepIfIdle(ctx, now); err != nil || slept.IdleFor != 0 || f.deallocated() != 0 {
		t.Fatalf("2 hours idle (default 3): %+v %v", slept, err)
	}
	slept, err := box.SleepIfIdle(ctx, now.Add(time.Hour))
	if err != nil || slept.IdleFor != 3*time.Hour || f.deallocated() != 1 {
		t.Fatalf("3 hours idle: %+v %v", slept, err)
	}
	if slept, err := box.SleepIfIdle(ctx, now.Add(2*time.Hour)); err != nil || slept.IdleFor != 0 || f.deallocated() != 1 {
		t.Fatalf("asleep already: %+v %v", slept, err)
	}

	// sleep_after_hours: 0 turns it off; a role without deallocate says what to do.
	f.set(func() { f.up, f.power = true, "running" })
	off := newBox(t, f, strings.Replace(reposYAML(agent), "  vm: ", "  sleep_after_hours: 0\n  vm: ", 1))
	if slept, err := off.SleepIfIdle(ctx, now.Add(10*time.Hour)); err != nil || slept.IdleFor != 0 {
		t.Fatalf("off: %+v %v", slept, err)
	}
	f.set(func() { f.noDeallocate = true })
	if _, err := box.SleepIfIdle(ctx, now.Add(10*time.Hour)); err == nil || !strings.Contains(err.Error(), "infra/dev.bicep") {
		t.Fatalf("no permission: %v", err)
	}
}

type notes struct{ sent []string }

func (n *notes) Name() string { return "test" }
func (n *notes) Notify(_ context.Context, text string, _ ...channel.Button) error {
	n.sent = append(n.sent, text)
	return nil
}

func TestCodeCommand(t *testing.T) {
	f := &fakeBox{up: true, power: "running", idleSince: "2026-11-21T09:00:00Z"}
	box := newBox(t, f, reposYAML(f.agent(t).URL))
	c := &Commands{Box: box}
	ctx := context.Background()

	list, _ := c.Handle(ctx, "/code", "", 0)
	if !strings.Contains(list, "Dev box: running") || !strings.Contains(list, "⚪ Consigliere · feature/x · 2 changed · 1 to push · 1 PR") {
		t.Fatalf("%s", list)
	}
	if reply, _ := c.Handle(ctx, "/code", "consigliere", 0); reply != "Opened Consigliere-1. It's in the Claude app → Code in a few seconds." {
		t.Fatalf("open: %q", reply)
	}
	if reply, _ := c.Handle(ctx, "/code", "Consigliere", 0); !strings.Contains(reply, "already has a session open") {
		t.Fatalf("continue twice: %q", reply)
	}
	if reply, _ := c.Handle(ctx, "/code", "Nope", 0); !strings.Contains(reply, "No repo") {
		t.Fatalf("unknown: %q", reply)
	}
	if reply, _ := c.Handle(ctx, "/code", "stop Consigliere-1", 0); !strings.HasPrefix(reply, "Stopped Consigliere-1") || !strings.Contains(reply, "/code Consigliere picks") {
		t.Fatalf("stop: %q", reply)
	}

	n := &notes{}
	job := &SleepJob{Box: box, Channel: n, Clock: clock.NewFake(time.Date(2026, 11, 21, 13, 0, 0, 0, time.UTC))}
	if err := job.Run(ctx); err != nil || len(n.sent) != 1 || !strings.Contains(n.sent[0], "after 4 hours") {
		t.Fatalf("%v %q", err, n.sent)
	}
}
