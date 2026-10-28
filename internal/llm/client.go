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
	// Effort is low by default (cheap bulk calls); raise it for judgment-heavy drafting.
	Effort Effort
}

// GenerateRequest asks for plain text (drafts, prep notes).
type GenerateRequest struct {
	Task      string
	System    []SystemBlock
	User      string
	MaxTokens int64
	Effort    Effort
}

type SystemBlock struct {
	Text  string
	Cache bool
}

type Effort string

const (
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
)

// Structured returns the model's JSON text. ErrRefused if the model declined.
func (c *Client) Structured(ctx context.Context, req StructuredRequest) (string, error) {
	return c.complete(ctx, req.Task, req.System, req.User, req.MaxTokens, req.Effort, req.Schema)
}

// Generate returns the model's text reply. ErrRefused if the model declined.
func (c *Client) Generate(ctx context.Context, req GenerateRequest) (string, error) {
	text, err := c.complete(ctx, req.Task, req.System, req.User, req.MaxTokens, req.Effort, nil)
	return strings.TrimSpace(text), err
}

func (c *Client) complete(ctx context.Context, task string, systemBlocks []SystemBlock, user string, maxTokens int64, effort Effort, schema map[string]any) (string, error) {
	model, err := c.cfg.ModelFor(task)
	if err != nil {
		return "", err
	}
	system := make([]anthropic.TextBlockParam, 0, len(systemBlocks))
	for _, b := range systemBlocks {
		block := anthropic.TextBlockParam{Text: b.Text}
		if b.Cache {
			block.CacheControl = anthropic.NewCacheControlEphemeralParam()
		}
		system = append(system, block)
	}
	if maxTokens == 0 {
		maxTokens = 8000
	}
	if effort == "" {
		effort = EffortLow
	}
	output := anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffort(effort)}
	if schema != nil {
		output.Format = anthropic.JSONOutputFormatParam{Schema: schema}
	}
	resp, err := c.api.Messages.New(ctx, anthropic.MessageNewParams{
		Model:        model,
		MaxTokens:    maxTokens,
		System:       system,
		OutputConfig: output,
		Messages:     []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(user))},
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
	if resp.StopReason == anthropic.StopReasonMaxTokens {
		return "", fmt.Errorf("reply cut off at max_tokens (%d)", maxTokens)
	}
	if strings.TrimSpace(sb.String()) == "" {
		return "", fmt.Errorf("empty reply (stop reason %q)", resp.StopReason)
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
