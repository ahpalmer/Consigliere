// Package router dispatches chat messages: slash commands are handled deterministically; everything else goes to
// the LLM.
package router

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ahpalmer/consigliere/internal/agent"
	"github.com/ahpalmer/consigliere/internal/commands"
	"github.com/ahpalmer/consigliere/internal/llm"
)

// Chatter is the conversational fallback (the LLM).
type Chatter interface {
	Reply(ctx context.Context, conversation int64, text string) (string, error)
	Reset(conversation int64)
}

type Router struct {
	state  *agent.State
	chat   Chatter
	usage  *llm.UsageTracker
	skills map[string]commands.Handler
	help   string
	now    func() time.Time
}

var builtIns = []commands.Info{
	{Name: "/status", Description: "uptime, pause state, token usage"},
	{Name: "/pause", Description: "kill switch: stop scheduled work and LLM calls"},
	{Name: "/resume", Description: "undo /pause"},
	{Name: "/reset", Description: "forget this conversation"},
}

func New(state *agent.State, chat Chatter, usage *llm.UsageTracker, handlers ...commands.Handler) *Router {
	r := &Router{state: state, chat: chat, usage: usage, skills: map[string]commands.Handler{}, now: time.Now}
	var lines []string
	for _, c := range builtIns {
		lines = append(lines, c.Name+" - "+c.Description)
	}
	for _, h := range handlers {
		for _, c := range h.Commands() {
			r.skills[strings.ToLower(c.Name)] = h
			lines = append(lines, c.Name+" - "+c.Description)
		}
	}
	r.help = strings.Join(lines, "\n") + "\nAnything else is sent to the assistant."
	return r
}

func (r *Router) Help() string { return r.help }

func (r *Router) Handle(ctx context.Context, chatID int64, text string) (string, error) {
	text = strings.TrimSpace(text)
	head, args, _ := strings.Cut(text, " ")
	// Telegram appends @botname to commands tapped in group chats.
	command, _, _ := strings.Cut(strings.ToLower(head), "@")
	args = strings.TrimSpace(args)

	switch command {
	case "/start", "/help":
		return r.help, nil
	case "/status":
		return r.status(), nil
	case "/pause":
		r.state.Pause()
		return "Paused. Scheduled work and LLM calls are stopped until /resume.", nil
	case "/resume":
		r.state.Resume()
		return "Resumed.", nil
	case "/reset":
		r.chat.Reset(chatID)
		return "Conversation cleared.", nil
	}

	if r.state.Paused() {
		return "I'm paused. Send /resume first.", nil
	}
	if h, ok := r.skills[command]; ok {
		return h.Handle(ctx, command, args, chatID)
	}
	if strings.HasPrefix(command, "/") {
		return fmt.Sprintf("Unknown command %s.\n\n%s", command, r.help), nil
	}
	return r.chat.Reply(ctx, chatID, text)
}

func (r *Router) status() string {
	uptime := r.now().Sub(r.state.StartedAt)
	var sb strings.Builder
	state := "running"
	if r.state.Paused() {
		state = "PAUSED"
	}
	fmt.Fprintf(&sb, "State: %s\nUptime: %dh %dm\n", state, int(uptime.Hours()), int(uptime.Minutes())%60)
	snapshot := r.usage.Snapshot()
	if len(snapshot) == 0 {
		sb.WriteString("No LLM calls yet.")
	}
	for _, u := range snapshot {
		fmt.Fprintf(&sb, "%s: %d calls, %d in / %d out (%d cached)\n", u.Model, u.Calls, u.InputTokens, u.OutputTokens, u.CachedTokens)
	}
	return strings.TrimSpace(sb.String())
}
