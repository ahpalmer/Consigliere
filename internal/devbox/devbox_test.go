package devbox

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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
