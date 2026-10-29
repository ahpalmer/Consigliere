package kit

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ahpalmer/consigliere/internal/datapack"
	"github.com/ahpalmer/consigliere/internal/llm"
)

var (
	//go:embed prompts/kit.md
	kitPrompt string
	//go:embed prompts/prep.md
	prepPrompt string
	//go:embed prompts/thanks.md
	thanksPrompt string
)

// LinkedIn's limit for a connection request note.
const connectionNoteLimit = 300

// Long descriptions are mostly boilerplate; the first ~12K chars carry the requirements.
const maxDescriptionChars = 12_000

// LLM is the subset of llm.Client the kit uses (faked in tests).
type LLM interface {
	Structured(ctx context.Context, req llm.StructuredRequest) (string, error)
	Generate(ctx context.Context, req llm.GenerateRequest) (string, error)
}

type Kit struct {
	Pack  *datapack.Pack
	LLM   LLM
	Owner string
}

// Job is the posting a kit is for. Description may be empty for jobs added by hand.
type Job struct {
	Company, Title, URL, Locations, Description string
}

type Result struct {
	Text     string
	File     []byte // tailored resume (DOCX); nil when there's no profile/bullets.yaml
	FileName string
}

// Build assembles the kit. Only the resume selection and the drafts use the model; everything else is lookup.
func (k *Kit) Build(ctx context.Context, job Job) (*Result, error) {
	var notes []string

	var people, similar []Connection
	connectionsText, haveConnections, err := k.Pack.ReadText(ConnectionsPath)
	if err != nil {
		return nil, err
	}
	if haveConnections {
		all, err := ParseConnections(strings.NewReader(connectionsText))
		if err != nil {
			notes = append(notes, fmt.Sprintf("Couldn't read %s: %v", ConnectionsPath, err))
		}
		people, similar = AtCompany(all, job.Company), SimilarCompany(all, job.Company)
	}

	var networks Networks
	if text, ok, err := k.Pack.ReadText(NetworksPath); err != nil {
		return nil, err
	} else if ok {
		if networks, err = ParseNetworks(text); err != nil {
			notes = append(notes, err.Error())
		}
	}

	profile, haveProfile, err := LoadProfile(k.Pack)
	if err != nil {
		return nil, err
	}
	summary, _, err := k.Pack.ReadText(SummaryPath)
	if err != nil {
		return nil, err
	}

	draft, err := k.draft(ctx, job, profile, summary)
	if err != nil {
		return nil, err
	}

	result := &Result{}
	var resumeLine string
	if haveProfile {
		selection, warnings := profile.Choose(draft.selection())
		for _, w := range warnings {
			notes = append(notes, "Resume: "+w)
		}
		if result.File, err = RenderDOCX(profile, selection); err != nil {
			return nil, err
		}
		result.FileName = fileName(profile.Name, job.Company)
		count := 0
		for _, ids := range selection {
			count += len(ids)
		}
		resumeLine = fmt.Sprintf("Resume: %d bullets picked and ordered for this posting (file below). Open it, check it fits on one page, then save it as a PDF if you want one.", count)
		if n := profile.DraftCount(); n > 0 {
			notes = append(notes, fmt.Sprintf("%d draft bullets in %s weren't used. Review them and delete their `draft: true` line to make them available.", n, ProfilePath))
		}
	} else {
		resumeLine = "Resume: add " + ProfilePath + " to the data pack to get a tailored resume."
	}

	note, trimmed := limitRunes(draft.ConnectionNote, connectionNoteLimit)
	if trimmed {
		notes = append(notes, "Connection note was trimmed to LinkedIn's 300-character limit; reread it.")
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "✍️ Apply kit: %s · %s\n", job.Title, job.Company)
	if job.URL != "" {
		sb.WriteString(job.URL + "\n")
	}
	if job.Description == "" {
		sb.WriteString("(No posting text on file, so tailoring used the title only.)\n")
	}
	sb.WriteString("\n" + resumeLine + "\n")
	writeList(&sb, "Focus on", draft.Focus)
	writeList(&sb, "Gaps to prepare for", draft.Gaps)

	fmt.Fprintf(&sb, "\nPeople you know at %s", job.Company)
	switch {
	case !haveConnections:
		sb.WriteString(": add your LinkedIn export as " + ConnectionsPath + " (LinkedIn → Settings → Data privacy → Get a copy of your data → Connections).\n")
	case len(people) == 0:
		sb.WriteString(": none in your connections export.\n")
	default:
		fmt.Fprintf(&sb, " (%d):\n", len(people))
		writePeople(&sb, people, false)
	}
	if len(similar) > 0 {
		sb.WriteString("At similarly named companies (check it's the same one):\n")
		writePeople(&sb, similar, true)
	}

	sb.WriteString("\nFind more (opens in your LinkedIn):\n")
	for _, l := range PeopleSearchLinks(job.Company) {
		fmt.Fprintf(&sb, "• %s: %s\n", l.Label, l.URL)
	}

	if groups := networks.For(job.Company); len(groups) > 0 || len(networks.General) > 0 {
		sb.WriteString("\nVeteran networks:\n")
		for _, g := range groups {
			fmt.Fprintf(&sb, "• %s: %s\n", job.Company, g)
		}
		for _, g := range networks.General {
			sb.WriteString("• " + g + "\n")
		}
	}

	if note != "" {
		fmt.Fprintf(&sb, "\nConnection note (%d/%d chars):\n%s\n", utf8.RuneCountInString(note), connectionNoteLimit, note)
	}
	if strings.TrimSpace(draft.ReferralRequest) != "" {
		sb.WriteString("\nReferral request:\n" + strings.TrimSpace(draft.ReferralRequest) + "\n")
	}
	writeList(&sb, "Notes", notes)
	result.Text = strings.TrimSpace(sb.String())
	return result, nil
}

type kitDraft struct {
	Sections []struct {
		Section string   `json:"section"`
		Bullets []string `json:"bullets"`
	} `json:"sections"`
	Focus           []string `json:"focus"`
	Gaps            []string `json:"gaps"`
	ConnectionNote  string   `json:"connection_note"`
	ReferralRequest string   `json:"referral_request"`
}

func (d kitDraft) selection() Selection {
	s := Selection{}
	for _, sec := range d.Sections {
		s[sec.Section] = append(s[sec.Section], sec.Bullets...)
	}
	return s
}

func (k *Kit) draft(ctx context.Context, job Job, profile *Profile, summary string) (kitDraft, error) {
	var bank strings.Builder
	if strings.TrimSpace(summary) != "" {
		bank.WriteString("## Summary\n" + strings.TrimSpace(summary) + "\n\n")
	}
	properties := map[string]any{
		"focus":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"gaps":             map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"connection_note":  map[string]any{"type": "string"},
		"referral_request": map[string]any{"type": "string"},
	}
	if profile != nil {
		bank.WriteString("## Bullet bank\n")
		var sectionIDs, bulletIDs []string
		for _, s := range profile.Sections {
			eligible := s.Eligible()
			if len(eligible) == 0 {
				continue
			}
			sectionIDs = append(sectionIDs, s.ID)
			fmt.Fprintf(&bank, "\n### section %s: %s (min %d, max %d)\n", s.ID, strings.TrimSpace(s.Org+" "+s.Team), s.Min, s.limit())
			for _, b := range eligible {
				bulletIDs = append(bulletIDs, b.ID)
				fmt.Fprintf(&bank, "- %s [%s]: %s\n", b.ID, strings.Join(b.Tags, ", "), b.Text)
			}
		}
		if len(sectionIDs) > 0 {
			properties["sections"] = map[string]any{"type": "array", "items": llm.ObjectSchema(map[string]any{
				"section": map[string]any{"type": "string", "enum": sectionIDs},
				"bullets": map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": bulletIDs}},
			})}
		}
	}

	text, err := k.LLM.Structured(ctx, llm.StructuredRequest{
		Task:   llm.TaskDraft,
		Effort: llm.EffortMedium,
		System: []llm.SystemBlock{
			{Text: llm.Render(kitPrompt, k.Owner)},
			{Text: strings.TrimSpace(bank.String()), Cache: true},
		},
		User:   describe(job, ""),
		Schema: llm.ObjectSchema(properties),
	})
	if errors.Is(err, llm.ErrRefused) {
		return kitDraft{}, errors.New("the model declined to draft this kit")
	}
	if err != nil {
		return kitDraft{}, err
	}
	var d kitDraft
	if err := json.Unmarshal([]byte(text), &d); err != nil {
		return kitDraft{}, fmt.Errorf("kit draft wasn't valid JSON: %w", err)
	}
	d.Focus, d.Gaps = d.Focus[:min(len(d.Focus), 3)], d.Gaps[:min(len(d.Gaps), 3)]
	return d, nil
}

// Prep writes an interview prep pack from the owner's stories and the posting.
func (k *Kit) Prep(ctx context.Context, job Job, notes string) (string, error) {
	var background strings.Builder
	for _, path := range []string{SummaryPath, ResumePath, StoriesPath} {
		text, ok, err := k.Pack.ReadText(path)
		if err != nil {
			return "", err
		}
		if ok && strings.TrimSpace(text) != "" {
			fmt.Fprintf(&background, "## %s\n%s\n\n", path, strings.TrimSpace(text))
		}
	}
	return k.generate(ctx, prepPrompt, background.String(), describe(job, notes), llm.EffortMedium)
}

// Thanks drafts a thank-you note after an interview.
func (k *Kit) Thanks(ctx context.Context, job Job, notes string) (string, error) {
	return k.generate(ctx, thanksPrompt, "", describe(job, notes), llm.EffortLow)
}

func (k *Kit) generate(ctx context.Context, prompt, background, user string, effort llm.Effort) (string, error) {
	system := []llm.SystemBlock{{Text: llm.Render(prompt, k.Owner)}}
	if strings.TrimSpace(background) != "" {
		system = append(system, llm.SystemBlock{Text: strings.TrimSpace(background), Cache: true})
	}
	text, err := k.LLM.Generate(ctx, llm.GenerateRequest{Task: llm.TaskDraft, Effort: effort, MaxTokens: 4000, System: system, User: user})
	if errors.Is(err, llm.ErrRefused) {
		return "", errors.New("the model declined this one")
	}
	return text, err
}

func describe(job Job, notes string) string {
	description := strings.TrimSpace(job.Description)
	if description == "" {
		description = "(not available; work from the title)"
	} else if len(description) > maxDescriptionChars {
		description = description[:maxDescriptionChars] + "\n[truncated]"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Company: %s\nTitle: %s\n", job.Company, job.Title)
	if job.Locations != "" {
		fmt.Fprintf(&sb, "Locations: %s\n", job.Locations)
	}
	if strings.TrimSpace(notes) != "" {
		fmt.Fprintf(&sb, "\nNotes:\n%s\n", strings.TrimSpace(notes))
	}
	fmt.Fprintf(&sb, "\nPosting:\n%s", description)
	return sb.String()
}

func writePeople(sb *strings.Builder, people []Connection, withCompany bool) {
	for i, p := range people {
		if i == 10 {
			fmt.Fprintf(sb, "…and %d more\n", len(people)-10)
			return
		}
		line := "• " + p.Name
		if p.Position != "" {
			line += " — " + p.Position
		}
		if withCompany {
			line += " at " + p.Company
		}
		if p.ConnectedOn != "" {
			line += " (since " + p.ConnectedOn + ")"
		}
		sb.WriteString(line + "\n")
	}
}

func writeList(sb *strings.Builder, label string, items []string) {
	var kept []string
	for _, it := range items {
		if s := strings.TrimSpace(it); s != "" {
			kept = append(kept, s)
		}
	}
	if len(kept) == 0 {
		return
	}
	sb.WriteString("\n" + label + ":\n")
	for _, s := range kept {
		sb.WriteString("• " + s + "\n")
	}
}

// limitRunes trims s to max runes, preferring to cut at the end of a sentence.
func limitRunes(s string, max int) (string, bool) {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= max {
		return s, false
	}
	cut := string(r[:max])
	if i := strings.LastIndexAny(cut, ".!?"); i > max/2 {
		return cut[:i+1], true
	}
	return strings.TrimSpace(string(r[:max-1])) + "…", true
}

// fileName: "ANDREW PALMER", "GitHub, Inc." → "Andrew_Palmer_Resume_-_GitHub_Inc.docx".
func fileName(name, company string) string {
	words := func(s string) []string {
		return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	}
	var person []string
	for _, w := range words(name) {
		r := []rune(strings.ToLower(w))
		r[0] = unicode.ToUpper(r[0])
		person = append(person, string(r))
	}
	base := strings.Join(person, "_") + "_Resume"
	if c := words(company); len(c) > 0 {
		base += "_-_" + strings.Join(c, "_")
	}
	return base + ".docx"
}
