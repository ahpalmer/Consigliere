package llm

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
)

type turn struct {
	user bool
	text string
}

// Chat handles conversational turns. History is in-memory per conversation and stored as text only,
// so trimming old turns never edits thinking blocks the API would need to validate.
type Chat struct {
	client   *Client
	system   string
	maxTurns int

	mu        sync.Mutex
	histories map[int64][]turn
}

func NewChat(client *Client, systemPrompt string) *Chat {
	maxTurns := client.cfg.MaxHistoryTurns
	if maxTurns <= 0 {
		maxTurns = 20
	}
	return &Chat{client: client, system: systemPrompt, maxTurns: maxTurns, histories: map[int64][]turn{}}
}

func (c *Chat) Reply(ctx context.Context, conversation int64, userText string) (string, error) {
	model, err := c.client.cfg.ModelFor(TaskChat)
	if err != nil {
		return "", err
	}

	c.mu.Lock()
	c.histories[conversation] = append(c.histories[conversation], turn{user: true, text: userText})
	history := append([]turn(nil), c.histories[conversation]...)
	c.mu.Unlock()

	messages := make([]anthropic.BetaMessageParam, 0, len(history))
	for _, t := range history {
		if t.user {
			messages = append(messages, anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(t.text)))
		} else {
			messages = append(messages, anthropic.BetaMessageParam{
				Role:    anthropic.BetaMessageParamRoleAssistant,
				Content: []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock(t.text)},
			})
		}
	}

	resp, err := c.client.api.Beta.Messages.New(ctx, anthropic.BetaMessageNewParams{
		Model:     model,
		MaxTokens: 16000,
		// Server-side refusal fallback: a policy decline is re-served by a fallback model in the same call.
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
		Fallbacks: anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()},
		System: []anthropic.BetaTextBlockParam{{
			Text:         c.system,
			CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
		}},
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffortMedium},
		Messages:     messages,
	})
	if err != nil {
		return "", err
	}
	c.client.Usage.Record(model, resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.Usage.CacheReadInputTokens)

	var reply string
	if resp.StopReason == anthropic.BetaStopReasonRefusal {
		slog.Warn("chat request refused", "category", resp.StopDetails.Category)
		reply = "I can't help with that one."
	} else {
		var parts []string
		for _, block := range resp.Content {
			if text, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
				parts = append(parts, text.Text)
			}
		}
		reply = strings.TrimSpace(strings.Join(parts, "\n"))
		if reply == "" {
			reply = "(no text in reply)"
		}
	}

	c.mu.Lock()
	h := append(c.histories[conversation], turn{text: reply})
	if excess := len(h) - c.maxTurns*2; excess > 0 {
		h = h[excess:]
	}
	c.histories[conversation] = h
	c.mu.Unlock()
	return reply, nil
}

func (c *Chat) Reset(conversation int64) {
	c.mu.Lock()
	delete(c.histories, conversation)
	c.mu.Unlock()
}
