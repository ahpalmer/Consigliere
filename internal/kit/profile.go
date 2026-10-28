// Package kit builds an apply kit for one posting: known contacts, people-search links, networks, a tailored
// one-page resume (selected from the owner's own bullets, never rewritten), and outreach drafts.
package kit

import (
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ahpalmer/consigliere/internal/datapack"
)

const (
	ProfilePath     = "profile/bullets.yaml"
	ConnectionsPath = "profile/connections.csv"
	NetworksPath    = "profile/networks.yaml"
	StoriesPath     = "profile/stories.md"
	SummaryPath     = "profile/summary.md"
	ResumePath      = "profile/resume.master.md"
)

// Profile is profile/bullets.yaml: the resume as structured data plus the bullet bank.
type Profile struct {
	Name      string    `yaml:"name"`
	Contact   string    `yaml:"contact"`
	Tagline   string    `yaml:"tagline"`
	Skills    []string  `yaml:"skills"`
	Sections  []Section `yaml:"sections"`
	Education []Dated   `yaml:"education"`
}

// Section is one experience block. Org is the employer line (omit it to continue the previous employer);
// Team is an optional sub-heading.
type Section struct {
	ID        string   `yaml:"id"`
	Org       string   `yaml:"org"`
	Dates     string   `yaml:"dates"`
	Team      string   `yaml:"team"`
	TeamDates string   `yaml:"team_dates"`
	Min       int      `yaml:"min"`
	Max       int      `yaml:"max"`
	Bullets   []Bullet `yaml:"bullets"`
}

type Bullet struct {
	ID   string   `yaml:"id"`
	Text string   `yaml:"text"`
	Tags []string `yaml:"tags"`
	// Draft bullets haven't been reviewed by the owner and are never put on a resume.
	Draft bool `yaml:"draft"`
}

type Dated struct {
	Text  string `yaml:"text"`
	Dates string `yaml:"dates"`
}

// LoadProfile reads and validates profile/bullets.yaml. ok=false when the file doesn't exist.
func LoadProfile(pack *datapack.Pack) (*Profile, bool, error) {
	text, ok, err := pack.ReadText(ProfilePath)
	if err != nil || !ok {
		return nil, ok, err
	}
	var p Profile
	if err := yaml.Unmarshal([]byte(text), &p); err != nil {
		return nil, true, fmt.Errorf("parse %s: %w", ProfilePath, err)
	}
	return &p, true, p.validate()
}

func (p *Profile) validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("%s: name is required", ProfilePath)
	}
	if len(p.Sections) == 0 {
		return fmt.Errorf("%s: no sections", ProfilePath)
	}
	sections, bullets := map[string]bool{}, map[string]bool{}
	for _, s := range p.Sections {
		if s.ID == "" || sections[s.ID] {
			return fmt.Errorf("%s: every section needs a unique id (got %q)", ProfilePath, s.ID)
		}
		sections[s.ID] = true
		if s.Max > 0 && s.Min > s.Max {
			return fmt.Errorf("%s: section %s has min > max", ProfilePath, s.ID)
		}
		for _, b := range s.Bullets {
			if b.ID == "" || bullets[b.ID] {
				return fmt.Errorf("%s: every bullet needs a unique id (got %q in %s)", ProfilePath, b.ID, s.ID)
			}
			if strings.TrimSpace(b.Text) == "" {
				return fmt.Errorf("%s: bullet %s has no text", ProfilePath, b.ID)
			}
			bullets[b.ID] = true
		}
	}
	return nil
}

// Eligible returns a section's reviewed bullets, in file order.
func (s Section) Eligible() []Bullet {
	var out []Bullet
	for _, b := range s.Bullets {
		if !b.Draft {
			out = append(out, b)
		}
	}
	return out
}

func (s Section) limit() int {
	if s.Max > 0 {
		return s.Max
	}
	return len(s.Bullets)
}

// Selection is the chosen bullet ids per section id, in display order.
type Selection map[string][]string

// Choose turns a model's proposal into a valid selection: per section, it keeps only ids that exist in that
// section and aren't drafts (in the proposed order, deduplicated), caps at max, and tops up to min from the
// remaining eligible bullets in file order. Rejected ids are reported, never invented into text.
func (p *Profile) Choose(proposed Selection) (Selection, []string) {
	var warnings []string
	known := map[string]string{} // bullet id → section id
	drafts := map[string]bool{}
	for _, s := range p.Sections {
		for _, b := range s.Bullets {
			known[b.ID] = s.ID
			drafts[b.ID] = b.Draft
		}
	}
	for section, ids := range proposed {
		for _, id := range ids {
			switch owner, ok := known[id]; {
			case !ok:
				warnings = append(warnings, fmt.Sprintf("dropped unknown bullet id %q", id))
			case owner != section:
				warnings = append(warnings, fmt.Sprintf("dropped %q: it belongs to section %s, not %s", id, owner, section))
			case drafts[id]:
				warnings = append(warnings, fmt.Sprintf("dropped %q: still marked draft", id))
			}
		}
	}

	out := Selection{}
	for _, s := range p.Sections {
		var chosen []string
		for _, id := range proposed[s.ID] {
			if known[id] == s.ID && !drafts[id] && !slices.Contains(chosen, id) && len(chosen) < s.limit() {
				chosen = append(chosen, id)
			}
		}
		for _, b := range s.Eligible() {
			if len(chosen) >= s.Min {
				break
			}
			if !slices.Contains(chosen, b.ID) {
				chosen = append(chosen, b.ID)
			}
		}
		out[s.ID] = chosen
	}
	slices.Sort(warnings)
	return out, warnings
}

// BulletText returns a bullet's text by id.
func (p *Profile) BulletText(id string) (string, bool) {
	for _, s := range p.Sections {
		for _, b := range s.Bullets {
			if b.ID == id {
				return b.Text, true
			}
		}
	}
	return "", false
}

// DraftCount is how many bullets still need the owner's review.
func (p *Profile) DraftCount() int {
	n := 0
	for _, s := range p.Sections {
		for _, b := range s.Bullets {
			if b.Draft {
				n++
			}
		}
	}
	return n
}
