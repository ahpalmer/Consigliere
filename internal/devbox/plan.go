package devbox

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ahpalmer/consigliere/internal/store"
)

// The plan on the Code page (M20): a repo's roadmap, read by the dev box's agent from its clone, with the open
// milestones first. The last copy is kept in the database, so the page has it while the box sleeps.

// DefaultPlanPath is where a repo's plan lives unless its entry in code/repos.yaml says `plan: <path>`.
const DefaultPlanPath = "docs/roadmap.md"

var ErrNoPlanYet = errors.New("the dev box is asleep and hasn't sent this plan yet; open a session (or /code) to wake it")

// Plan is a repo's plan as the page shows it.
type Plan struct {
	Repo string `json:"repo"`
	Path string `json:"path"`
	// The default branch, and when the dev box last fetched from GitHub.
	Branch string     `json:"branch"`
	AsOf   *time.Time `json:"asOf"`
	// When the bot read it; Stale when that's an old copy because the box is asleep.
	ReadAt time.Time `json:"readAt"`
	Stale  bool      `json:"stale"`
	Found  bool      `json:"found"`
	// Milestones from the plan's table, open ones first. Empty when the plan isn't in that shape; Text has it all.
	Milestones []Milestone `json:"milestones"`
	Text       string      `json:"text"`
	// Open branches whose copy of the plan differs from the default branch's.
	Branches []string `json:"branches"`
}

type Milestone struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Open    bool   `json:"open"`
	Details string `json:"details"`
	// Where a branch says something else: its status there, or that the milestone is only on that branch.
	OnBranches []BranchStatus `json:"onBranches"`
}

type BranchStatus struct {
	Branch string `json:"branch"`
	Status string `json:"status"`
	New    bool   `json:"new"` // not on the default branch at all
}

// Plan reads a repo's plan through the agent, or, while the box is asleep, the last copy read.
func (b *Box) Plan(ctx context.Context, repo string) (Plan, error) {
	s, ok, err := b.settings()
	if err != nil {
		return Plan{}, err
	}
	if !ok || !slices.Contains(s.Repos, repo) {
		return Plan{}, ErrUnknownRepo
	}
	path := s.Plans[repo]
	if path == "" {
		path = DefaultPlanPath
	}
	key := "devbox_plan:" + repo

	actx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	var raw struct {
		Branch   string
		Text     *string
		AsOf     *time.Time
		Branches []struct{ Branch, Text string }
	}
	err = b.call(actx, http.MethodGet, s.Agent+"/plan/"+url.PathEscape(repo)+"?"+url.Values{"path": {path}}.Encode(), nil, nil, &raw)
	var refused *replyError
	switch {
	case errors.As(err, &refused):
		return Plan{}, &AgentError{Status: refused.Status, Message: refused.Message}
	case err != nil:
		return b.cachedPlan(ctx, key)
	}
	p := Plan{Repo: repo, Path: path, Branch: raw.Branch, AsOf: raw.AsOf, ReadAt: b.now(), Milestones: []Milestone{}, Branches: []string{}}
	if raw.Text != nil {
		p.Found, p.Text = true, *raw.Text
		p.Milestones = ParseMilestones(p.Text)
		for _, br := range raw.Branches {
			p.Branches = append(p.Branches, br.Branch)
			p.Milestones = mergeBranch(p.Milestones, br.Branch, ParseMilestones(br.Text))
		}
	}
	if b.DB != nil {
		if saved, err := json.Marshal(p); err == nil {
			_ = store.SetState(ctx, b.DB, key, string(saved))
		}
	}
	return p, nil
}

func (b *Box) cachedPlan(ctx context.Context, key string) (Plan, error) {
	if b.DB == nil {
		return Plan{}, ErrNoPlanYet
	}
	saved, found, err := store.GetState(ctx, b.DB, key)
	if err != nil || !found {
		return Plan{}, errors.Join(ErrNoPlanYet, err)
	}
	var p Plan
	if err := json.Unmarshal([]byte(saved), &p); err != nil {
		return Plan{}, ErrNoPlanYet
	}
	p.Stale = true
	return p, nil
}

func (b *Box) now() time.Time {
	if b.Clock != nil {
		return b.Clock.Now()
	}
	return time.Now().UTC()
}

var (
	// | **M20** | The plan on the Code page ... | Planned |
	milestoneRow = regexp.MustCompile(`^\|\s*\*\*(M\d+)\*\*\s*\|(.*)\|([^|]*)\|\s*$`)
	// **M20: Title.** text...   or   - **M18:** text...
	detailsStart = regexp.MustCompile(`^(?:- )?\*\*(M\d+)(?:-M\d+)?:`)
)

// ParseMilestones reads the milestone table (`| **M1** | what | status |`) and each milestone's details: the
// paragraph that starts `**M1: ...**`, or a list item that starts `- **M1:**`. Open milestones come first, each group
// in the plan's order. Nil when the plan has no such table.
func ParseMilestones(text string) []Milestone {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var out []Milestone
	index := map[string]int{}
	for _, line := range lines {
		if m := milestoneRow.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			status := strings.TrimSpace(m[3])
			lower := strings.ToLower(status)
			index[m[1]] = len(out)
			out = append(out, Milestone{ID: m[1], Title: strings.TrimSpace(m[2]), Status: status,
				Open: !strings.HasPrefix(lower, "done"), OnBranches: []BranchStatus{}})
		}
	}
	for i := 0; i < len(lines); i++ {
		m := detailsStart.FindStringSubmatch(strings.TrimSpace(lines[i]))
		if m == nil {
			continue
		}
		j, ok := index[m[1]]
		if !ok || out[j].Details != "" {
			continue
		}
		item := strings.HasPrefix(strings.TrimSpace(lines[i]), "- ")
		block := []string{strings.TrimPrefix(strings.TrimSpace(lines[i]), "- ")}
		for i+1 < len(lines) {
			next := lines[i+1]
			// A paragraph ends at a blank line or the next milestone's details; a list item also at the next item or
			// anything not indented under it.
			if strings.TrimSpace(next) == "" || detailsStart.MatchString(strings.TrimSpace(next)) ||
				(item && (strings.HasPrefix(strings.TrimSpace(next), "- ") || !strings.HasPrefix(next, "  "))) {
				break
			}
			block = append(block, strings.TrimSpace(next))
			i++
		}
		out[j].Details = strings.Join(block, "\n")
	}
	slices.SortStableFunc(out, func(a, b Milestone) int {
		switch {
		case a.Open == b.Open:
			return 0
		case a.Open:
			return -1
		}
		return 1
	})
	return out
}

// mergeBranch notes where a branch's plan says something else: a different status, or a milestone only it has.
func mergeBranch(base []Milestone, branch string, other []Milestone) []Milestone {
	for _, o := range other {
		i := slices.IndexFunc(base, func(m Milestone) bool { return m.ID == o.ID })
		switch {
		case i < 0:
			o.OnBranches = []BranchStatus{{Branch: branch, Status: o.Status, New: true}}
			base = append(base, o)
		case base[i].Status != o.Status:
			base[i].OnBranches = append(base[i].OnBranches, BranchStatus{Branch: branch, Status: o.Status})
			// A milestone a branch has moved on keeps its place among the open ones until that branch merges.
		}
	}
	return base
}
