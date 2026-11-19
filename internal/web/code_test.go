package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahpalmer/consigliere/internal/datapack"
	"github.com/ahpalmer/consigliere/internal/devbox"
)

type noTokens struct{}

func (noTokens) Token(context.Context, string) (string, error) { return "t", nil }

func codeServer(t *testing.T, reposYAML string) *httptest.Server {
	t.Helper()
	root := t.TempDir()
	if reposYAML != "" {
		_ = os.MkdirAll(filepath.Join(root, "code"), 0o755)
		if err := os.WriteFile(filepath.Join(root, "code", "repos.yaml"), []byte(reposYAML), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pack, err := datapack.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Token: "s3cret", Code: &devbox.Box{Pack: pack, Tokens: noTokens{}}}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv
}

func codeRequest(t *testing.T, srv *httptest.Server, method, path, body string, out any) int {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if method != "GET" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer s3cret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func TestCodePageOnlyWithADevBox(t *testing.T) {
	srv := codeServer(t, "repos:\n  - { name: A, url: https://github.com/o/A.git }\n")
	var session map[string]bool
	codeRequest(t, srv, "GET", "/api/session", "", &session)
	if !session["signedIn"] || session["code"] {
		t.Fatalf("session = %v", session)
	}
	if code := codeRequest(t, srv, "GET", "/api/code", "", nil); code != http.StatusNotFound {
		t.Fatalf("GET /api/code = %d", code)
	}
}

func TestCodePageListsAndStartsSessions(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			_, _ = w.Write([]byte(`{"sessions":[{"name":"A-1","repo":"A","startedAt":"2026-11-18T12:00:00Z"}]}`))
		case "POST":
			var body struct{ Repo, Mode string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Mode == "continue" {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"error":"A already has a session open (A-1): open it in the Claude app"}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"name":"A-2","repo":"A"}`))
		}
	}))
	defer agent.Close()
	srv := codeServer(t, "dev_box:\n  agent: "+agent.URL+"\n  vm: /subscriptions/s/resourceGroups/g/providers/Microsoft.Compute/virtualMachines/v\n"+
		"repos:\n  - { name: A, url: https://github.com/o/A.git }\n  - { name: B, url: https://github.com/o/B.git }\n")

	var session map[string]bool
	codeRequest(t, srv, "GET", "/api/session", "", &session)
	if !session["code"] {
		t.Fatalf("session = %v", session)
	}
	var st devbox.Status
	if code := codeRequest(t, srv, "GET", "/api/code", "", &st); code != http.StatusOK || st.VM != "running" ||
		len(st.Repos) != 2 || len(st.Repos[0].Sessions) != 1 || len(st.Repos[1].Sessions) != 0 {
		t.Fatalf("GET /api/code = %d %+v", code, st)
	}

	var started devbox.Started
	if code := codeRequest(t, srv, "POST", "/api/code/repos/A/sessions", `{"mode":"new"}`, &started); code != http.StatusOK || started.Session != "A-2" {
		t.Fatalf("new session = %d %+v", code, started)
	}
	var refused map[string]string
	if code := codeRequest(t, srv, "POST", "/api/code/repos/A/sessions", `{"mode":"continue"}`, &refused); code != http.StatusConflict ||
		refused["error"] == "" {
		t.Fatalf("continue while open = %d %v", code, refused)
	}
	if code := codeRequest(t, srv, "POST", "/api/code/repos/Z/sessions", `{"mode":"new"}`, nil); code != http.StatusNotFound {
		t.Fatalf("unknown repo = %d", code)
	}
	if code := codeRequest(t, srv, "POST", "/api/code/repos/A/sessions", `{"mode":"bash"}`, nil); code != http.StatusBadRequest {
		t.Fatalf("bad mode = %d", code)
	}
}
