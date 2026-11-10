package kit

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ahpalmer/consigliere/internal/llm"
)

//go:embed prompts/cover.md
var coverPrompt string

// WinsPath holds the owner's accomplishments in their own words, beyond what fits on the resume.
const WinsPath = "profile/wins.md"

// The instructions box is for a few pages at most; the cap only catches a whole document pasted by mistake.
const maxInstructionsChars = 20000

// CoverLetter is a letter for one posting, written from the owner's background and their instructions for it.
type CoverLetter struct {
	Text     string // the letter, then notes for the owner
	File     []byte // the letter as DOCX
	FileName string
	Answers  []Answer // the letter and a "why us" answer, to paste into a form
}

type coverDraft struct {
	Paragraphs []string `json:"paragraphs"`
	WhyCompany string   `json:"why_company"`
	Notes      []string `json:"notes"`
}

// CoverLetter writes the letter body with the model; the header, greeting and sign-off are fixed.
func (k *Kit) CoverLetter(ctx context.Context, job Job, instructions string, date time.Time) (*CoverLetter, error) {
	instructions = strings.TrimSpace(instructions)
	if n := utf8.RuneCountInString(instructions); n > maxInstructionsChars {
		return nil, fmt.Errorf("instructions are too long (%d characters; keep them under %d)", n, maxInstructionsChars)
	}

	var background strings.Builder
	for _, path := range []string{SummaryPath, ResumePath, WinsPath, StoriesPath} {
		text, ok, err := k.Pack.ReadText(path)
		if err != nil {
			return nil, err
		}
		if ok && strings.TrimSpace(text) != "" {
			fmt.Fprintf(&background, "## %s\n%s\n\n", path, strings.TrimSpace(text))
		}
	}
	if background.Len() == 0 {
		return nil, fmt.Errorf("add %s or %s to the data pack first: the letter is written from them", ResumePath, SummaryPath)
	}

	// The header matches the resume's ("ANDREW PALMER"); the sign-off is in normal case.
	header, name, contact := k.Owner, k.Owner, ""
	if profile, ok, err := LoadProfile(k.Pack); err != nil {
		return nil, err
	} else if ok {
		header, name, contact = profile.Name, strings.Join(titleWords(profile.Name), " "), profile.Contact
	}

	user := describe(job, "")
	if instructions != "" {
		user = "Instructions from " + k.Owner + ":\n" + instructions + "\n\n" + user
	}
	text, err := k.LLM.Structured(ctx, llm.StructuredRequest{
		Task:   llm.TaskDraft,
		Effort: llm.EffortHigh, // the one draft a stranger reads closely
		System: []llm.SystemBlock{
			{Text: llm.Render(coverPrompt, k.Owner)},
			{Text: strings.TrimSpace(background.String()), Cache: true},
		},
		User: user,
		Schema: llm.ObjectSchema(map[string]any{
			"paragraphs":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"why_company": map[string]any{"type": "string"},
			"notes":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}),
	})
	if errors.Is(err, llm.ErrRefused) {
		return nil, errors.New("the model declined to write this letter")
	}
	if err != nil {
		return nil, err
	}
	var d coverDraft
	if err := json.Unmarshal([]byte(text), &d); err != nil {
		return nil, fmt.Errorf("cover letter wasn't valid JSON: %w", err)
	}
	var body []string
	for _, p := range d.Paragraphs {
		if p = strings.TrimSpace(p); p != "" {
			body = append(body, p)
		}
	}
	if len(body) == 0 {
		return nil, errors.New("the model returned an empty letter; try again")
	}

	paragraphs := append([]string{"Dear " + job.Company + " Hiring Team,"}, body...)
	paragraphs = append(paragraphs, "Sincerely,\n"+name)
	letter := strings.Join(paragraphs, "\n\n")
	file, err := RenderLetterDOCX(header, contact, date.Format("January 2, 2006"), paragraphs)
	if err != nil {
		return nil, err
	}

	out := &CoverLetter{Text: letter, File: file, FileName: fileName(name, "Cover_Letter", job.Company),
		Answers: []Answer{{"Cover letter", letter}}}
	if why := strings.TrimSpace(d.WhyCompany); why != "" {
		out.Answers = append(out.Answers, Answer{"Why " + job.Company + "?", why})
	}
	var sb strings.Builder
	sb.WriteString(letter + "\n")
	for _, a := range out.Answers[1:] {
		sb.WriteString("\n" + a.Label + "\n" + a.Text + "\n")
	}
	writeList(&sb, "Notes for you (not in the letter)", d.Notes)
	out.Text = strings.TrimSpace(sb.String())
	return out, nil
}
