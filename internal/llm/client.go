// Package llm wraps the Claude API: per-task model routing, usage tracking, chat, and structured output.
package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/ahpalmer/consigliere/internal/config"
)

// Tasks with independently configurable models.
const (
	TaskChat    = "chat"
	TaskDraft   = "draft"
	TaskScore   = "score"
	TaskExtract = "extract"
)

// ErrRefused means the model declined the request (stop_reason "refusal").
var ErrRefused = errors.New("model declined the request")

type Client struct {
	api   anthropic.Client
	cfg   config.Llm
	Usage *UsageTracker
}

func NewClient(apiKey string, cfg config.Llm, usage *UsageTracker) *Client {
	return &Client{api: anthropic.NewClient(option.WithAPIKey(apiKey)), cfg: cfg, Usage: usage}
}

// StructuredRequest asks for JSON matching Schema. Use for classification/extraction on cheap models.
type StructuredRequest struct {
	Task string
	// System blocks, in order. Mark the last stable block Cache=true so repeated calls reuse it.
	System    []SystemBlock
	User      string
	Schema    map[string]any
	MaxTokens int64
}

type SystemBlock struct {
	Text  string
	Cache bool
}

// Structured returns the model's JSON text. ErrRefused if the model declined.
func (c *Client) Structured(ctx context.Context, req StructuredRequest) (string, error) {
	model, err := c.cfg.ModelFor(req.Task)
	if err != nil {
		return "", err
	}
	system := make([]anthropic.TextBlockParam, 0, len(req.System))
	for _, b := range req.System {
		block := anthropic.TextBlockParam{Text: b.Text}
		if b.Cache {
			block.CacheControl = anthropic.NewCacheControlEphemeralParam()
		}
		system = append(system, block)
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 8000
	}
	resp, err := c.api.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: maxTokens,
		System:    system,
		OutputConfig: anthropic.OutputConfigParam{
			Effort: anthropic.OutputConfigEffortLow,
			Format: anthropic.JSONOutputFormatParam{Schema: req.Schema},
		},
		Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(req.User))},
	})
	if err != nil {
		return "", err
	}
	c.Usage.Record(model, resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.Usage.CacheReadInputTokens)
	if resp.StopReason == anthropic.StopReasonRefusal {
		return "", fmt.Errorf("%w (%s)", ErrRefused, resp.StopDetails.Category)
	}
	var sb strings.Builder
	for _, block := range resp.Content {
		if text, ok := block.AsAny().(anthropic.TextBlock); ok {
			sb.WriteString(text.Text)
		}
	}
	return sb.String(), nil
}

// ObjectSchema builds a strict JSON schema object: every listed property required, nothing extra allowed.
func ObjectSchema(properties map[string]any) map[string]any {
	required := make([]string, 0, len(properties))
	for name := range properties {
		required = append(required, name)
	}
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}
