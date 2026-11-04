package mail

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ahpalmer/consigliere/internal/llm"
)

//go:embed prompts/read.md
var readPrompt string

// Listing is a job as the email states it. Not trusted until Source verifies the URL.
type Listing struct {
	Title    string `json:"title"`
	Company  string `json:"company"`
	Location string `json:"location"`
	URL      string `json:"url"`
	Salary   string `json:"salary"`
	Posted   string `json:"posted"`
	Snippet  string `json:"snippet"`
}

// Update kinds: what an email says about one of the owner's applications.
const (
	UpdateReceived          = "received"
	UpdateRejection         = "rejection"
	UpdateAssessment        = "assessment"
	UpdateInterviewRequest  = "interview_request"
	UpdateScheduling        = "scheduling"
	UpdateOffer             = "offer"
	UpdateRecruiterOutreach = "recruiter_outreach"
	UpdateOther             = "other"
	updateNone              = "none"
)

var updateKinds = []string{UpdateReceived, UpdateRejection, UpdateAssessment, UpdateInterviewRequest, UpdateScheduling, UpdateOffer,
	UpdateRecruiterOutreach, UpdateOther, updateNone}

// Update is an email about one application. The model's fields are untrusted text; Source keeps only links that
// appear in the email and fills in where the message came from.
type Update struct {
	Kind          string
	Company, Role string
	When          string // a stated interview time, ISO 8601 ("2026-11-05T14:00:00-05:00"), else ""
	Deadline      string // as stated, e.g. "Nov 7"
	Summary       string
	Links         []string

	MessageID, From, Subject string
	ReceivedAt               *time.Time
	// Backfill: found by the one-time look back over older mail. Recorded, never alerted on.
	Backfill bool
}

// Reading is what one email says: job listings (alerts) and/or an update about an application.
type Reading struct {
	Listings []Listing
	Update   *Update // nil when the email isn't about one of the owner's applications
}

type Reader interface {
	Read(ctx context.Context, emailText string) (Reading, error)
}

// Alert emails are mostly short; this bounds cost on a pathological one.
const maxEmailChars = 40_000

// LLMReader uses a cheap model with structured output instead of per-sender parsers, which break whenever an
// email template changes. The model has no tools: its only output is this JSON.
type LLMReader struct{ LLM *llm.Client }

func (e LLMReader) Read(ctx context.Context, emailText string) (Reading, error) {
	if len(emailText) > maxEmailChars {
		emailText = emailText[:maxEmailChars]
	}
	str := map[string]any{"type": "string"}
	text, err := e.LLM.Structured(ctx, llm.StructuredRequest{
		Task:      llm.TaskExtract,
		System:    []llm.SystemBlock{{Text: readPrompt}},
		User:      emailText,
		MaxTokens: 16000,
		Schema: llm.ObjectSchema(map[string]any{
			"items": map[string]any{
				"type": "array",
				"items": llm.ObjectSchema(map[string]any{
					"title": str, "company": str, "location": str, "url": str, "salary": str, "posted": str, "snippet": str,
				}),
			},
			"update": llm.ObjectSchema(map[string]any{
				"type":    map[string]any{"type": "string", "enum": updateKinds},
				"company": str, "role": str, "when": str, "deadline": str, "summary": str,
				"links": map[string]any{"type": "array", "items": str},
			}),
		}),
	})
	if errors.Is(err, llm.ErrRefused) {
		slog.Warn("email reading refused", "err", err)
		return Reading{}, nil
	}
	if err != nil {
		return Reading{}, err
	}
	return parseReading(text)
}

func parseReading(text string) (Reading, error) {
	var result struct {
		Items  []Listing `json:"items"`
		Update struct {
			Type, Company, Role, When, Deadline, Summary string
			Links                                        []string
		} `json:"update"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return Reading{}, fmt.Errorf("email reader returned invalid JSON: %w", err)
	}
	r := Reading{Listings: result.Items}
	if u := result.Update; u.Type != "" && u.Type != updateNone {
		r.Update = &Update{Kind: u.Type, Company: u.Company, Role: u.Role, When: u.When, Deadline: u.Deadline, Summary: u.Summary, Links: u.Links}
	}
	return r, nil
}
