package mail

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ahpalmer/consigliere/internal/llm"
)

//go:embed prompts/extract.md
var extractPrompt string

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

type Extractor interface {
	Extract(ctx context.Context, emailText string) ([]Listing, error)
}

// Alert emails are mostly short; this bounds cost on a pathological one.
const maxEmailChars = 40_000

// LLMExtractor uses a cheap model with structured output instead of per-sender HTML parsers, which break whenever
// an email template changes.
type LLMExtractor struct{ LLM *llm.Client }

func (e LLMExtractor) Extract(ctx context.Context, emailText string) ([]Listing, error) {
	if len(emailText) > maxEmailChars {
		emailText = emailText[:maxEmailChars]
	}
	str := map[string]any{"type": "string"}
	text, err := e.LLM.Structured(ctx, llm.StructuredRequest{
		Task:      llm.TaskExtract,
		System:    []llm.SystemBlock{{Text: extractPrompt}},
		User:      emailText,
		MaxTokens: 16000,
		Schema: llm.ObjectSchema(map[string]any{
			"items": map[string]any{
				"type": "array",
				"items": llm.ObjectSchema(map[string]any{
					"title": str, "company": str, "location": str, "url": str, "salary": str, "posted": str, "snippet": str,
				}),
			},
		}),
	})
	if errors.Is(err, llm.ErrRefused) {
		slog.Warn("extraction refused", "err", err)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result struct {
		Items []Listing `json:"items"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, fmt.Errorf("extractor returned invalid JSON: %w", err)
	}
	return result.Items, nil
}
