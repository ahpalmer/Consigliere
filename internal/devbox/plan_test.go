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
	"testing"
	"time"

	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/store"
)

const roadmap = `# Roadmap

| # | Milestone | Status |
|---|---|---|
| **M0** | Skeleton | Done |
| **M5** | Cloud on AKS | Done, replaced by M12 |
| **M9** | Package comparison | Built |
| **M18** | A **Code** page | In progress |

## Details for open milestones

**M9: Package comparison.** The current package goes in the data pack,
each value dated.

Something else.

**M16-M18: Coding from the phone.** The owner works on their repositories.
- **M18:** the dashboard's Code page. Each repo shows active or inactive;
  Continue Coding opens a session.
- **M19:** the page grows.
`

func TestParseMilestones(t *testing.T) {
	ms := ParseMilestones(roadmap)
	var ids []string
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	if strings.Join(ids, " ") != "M9 M18 M0 M5" {
		t.Fatalf("open first, in order: %v", ids)
	}
	if ms[0].Title != "Package comparison" || ms[0].Status != "Built" || !ms[0].Open || ms[3].Open {
		t.Fatalf("%+v", ms)
	}
	if ms[0].Details != "**M9: Package comparison.** The current package goes in the data pack,\neach value dated." {
		t.Fatalf("paragraph details: %q", ms[0].Details)
	}
	if ms[1].Title != "A **Code** page" || ms[1].Details != "**M18:** the dashboard's Code page. Each repo shows active or inactive;\nContinue Coding opens a session." {
		t.Fatalf("list item details: %q", ms[1].Details)
	}
	if ParseMilestones("# Notes\n\nNo table here.") != nil {
		t.Fatal("a plan without the table has no milestones")
	}
}

func TestParseTheRealRoadmap(t *testing.T) {
	text, err := os.ReadFile("../../docs/roadmap.md")
	if err != nil {
		t.Fatal(err)
	}
	ms := ParseMilestones(string(text))
	if len(ms) < 15 || !ms[0].Open {
		t.Fatalf("%d milestones", len(ms))
	}
	for _, m := range ms {
		if m.ID == "M20" && (!m.Open || !strings.HasPrefix(m.Details, "**M20:**")) {
			t.Fatalf("M20: %+v", m)
		}
	}
}

func TestBranchDifferences(t *testing.T) {
	branch := strings.Replace(roadmap, "| **M18** | A **Code** page | In progress |", "| **M18** | A **Code** page | Done |\n| **M21** | Something new | Planned |", 1)
	ms := mergeBranch(ParseMilestones(roadmap), "feature/x", ParseMilestones(branch))
	m18, m21 := ms[1], ms[len(ms)-1]
	if len(m18.OnBranches) != 1 || m18.OnBranches[0] != (BranchStatus{Branch: "feature/x", Status: "Done"}) {
		t.Fatalf("M18: %+v", m18)
	}
	if m21.ID != "M21" || !m21.OnBranches[0].New {
		t.Fatalf("M21: %+v", m21)
	}
}

func TestPlanIsKeptForWhileTheBoxSleeps(t *testing.T) {
	up := true
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up {
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		if r.URL.Path != "/plan/Consigliere" || r.URL.Query().Get("path") != "docs/plan.md" {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "no repo " + r.URL.Path})
			return
		}
		branch := strings.Replace(roadmap, "| Built |", "| Done |", 1)
		_ = json.NewEncoder(w).Encode(map[string]any{"path": "docs/plan.md", "branch": "main", "text": roadmap, "asOf": "2026-11-21T00:00:00Z",
			"branches": []map[string]string{{"branch": "feature/m9", "text": branch}}})
	}))
	defer agent.Close()
	f := &fakeBox{power: "deallocated"}
	box := newBox(t, f, strings.Replace(reposYAML(agent.URL), "{ name: Consigliere, url:", "{ name: Consigliere, plan: docs/plan.md, url:", 1))
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	box.DB, box.Clock = db, clock.NewFake(time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC))
	ctx := context.Background()

	p, err := box.Plan(ctx, "Consigliere")
	if err != nil || p.Stale || !p.Found || p.Path != "docs/plan.md" || len(p.Milestones) != 4 || p.Branches[0] != "feature/m9" {
		t.Fatalf("%+v %v", p, err)
	}
	if on := p.Milestones[0].OnBranches; len(on) != 1 || on[0].Status != "Done" {
		t.Fatalf("M9 on the branch: %+v", on)
	}

	up = false
	if p, err = box.Plan(ctx, "Consigliere"); err != nil || !p.Stale || len(p.Milestones) != 4 || !p.ReadAt.Equal(time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("asleep: %+v %v", p, err)
	}
	if _, err := box.Plan(ctx, "Consigliere-data"); !errors.Is(err, ErrNoPlanYet) {
		t.Fatalf("never read: %v", err)
	}
	if _, err := box.Plan(ctx, "Nope"); !errors.Is(err, ErrUnknownRepo) {
		t.Fatalf("unknown: %v", err)
	}
}
