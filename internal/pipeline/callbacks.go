package pipeline

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ahpalmer/consigliere/internal/channel"
)

// ActivateButton is "🎯 Make active" for a radar item, shown on alerts and item details.
func ActivateButton(itemID int64) channel.Button {
	return channel.Button{Label: "🎯 Make active", Data: fmt.Sprintf("%sact:%d", prefix, itemID)}
}

func (c *Commands) Prefix() string { return prefix }

// HandleCallback handles pipeline buttons: pp:<action>:<id>[:<arg>].
func (c *Commands) HandleCallback(ctx context.Context, data string) (string, error) {
	action, id, arg, ok := parseCallback(data)
	if !ok {
		return "Unrecognized button.", nil
	}
	if action == "act" {
		p, created, err := c.Pipeline.ActivateItem(ctx, id)
		if err != nil {
			return userErrorToast(err)
		}
		if _, err := c.activated(ctx, p, created); err != nil {
			return "", err
		}
		if created {
			return "🎯 Active as " + p.Ref(), nil
		}
		return "Already " + p.Ref(), nil
	}

	p, err := c.Pipeline.Get(ctx, id)
	if err != nil {
		return userErrorToast(err)
	}
	switch action {
	case "tick":
		if _, err := c.Pipeline.Tick(ctx, id, arg); err != nil {
			return userErrorToast(err)
		}
		return "✅ " + stepLabel(arg), nil
	case "app":
		if p, err = c.Pipeline.Applied(ctx, id); err != nil {
			return userErrorToast(err)
		}
		if err := c.Channel.Notify(ctx, c.appliedReply(p)); err != nil {
			return "", err
		}
		return "📨 Moved to Waiting", nil
	case "fu":
		if _, err := c.Pipeline.FollowedUp(ctx, id); err != nil {
			return userErrorToast(err)
		}
		return "📬 Noted", nil
	case "cmp":
		if err := c.Channel.Notify(ctx, fmt.Sprintf("How did %s · %s · %s end?", p.Ref(), p.Title, p.Company), OutcomeButtons(id)...); err != nil {
			return "", err
		}
		return "Pick an outcome below", nil
	case "out":
		if _, err := c.Pipeline.Complete(ctx, id, arg, ""); err != nil {
			return userErrorToast(err)
		}
		return "🗂 Completed", nil
	case "ghost":
		if arg == "y" {
			if _, err := c.Pipeline.Complete(ctx, id, "ghosted", ""); err != nil {
				return userErrorToast(err)
			}
			return "👻 Marked ghosted", nil
		}
		if _, err := c.Pipeline.KeepWaiting(ctx, id); err != nil {
			return userErrorToast(err)
		}
		return "⏳ I'll ask again in two weeks", nil
	case "kit", "prep":
		if c.State != nil && c.State.Paused() {
			return "Paused. /resume first.", nil
		}
		var reply string
		if action == "kit" {
			reply, err = c.kit(ctx, p.Ref())
		} else {
			reply, err = c.prep(ctx, p, "")
		}
		if err != nil {
			return "", err
		}
		return firstLine(reply), nil
	}
	return "Unrecognized button.", nil
}

// Refresh redraws the message a button sat on, so checklists and stages update in place.
func (c *Commands) Refresh(ctx context.Context, data string) (string, []channel.Button, bool, error) {
	action, id, _, ok := parseCallback(data)
	if !ok {
		return "", nil, false, nil
	}
	switch action {
	case "tick", "app", "fu":
		p, err := c.Pipeline.Get(ctx, id)
		if err != nil {
			return "", nil, false, err
		}
		text, buttons := c.Pipeline.Card(p)
		return text, buttons, true, nil
	case "out", "ghost":
		p, err := c.Pipeline.Get(ctx, id)
		if err != nil {
			return "", nil, false, err
		}
		if p.Stage != StageCompleted {
			return fmt.Sprintf("⏳ %s · %s · %s: still waiting.", p.Ref(), p.Title, p.Company), nil, true, nil
		}
		return fmt.Sprintf("🗂 %s · %s · %s: %s.", p.Ref(), p.Title, p.Company, completion(p)), nil, true, nil
	}
	return "", nil, false, nil
}

func parseCallback(data string) (action string, id int64, arg string, ok bool) {
	parts := strings.Split(strings.TrimPrefix(data, prefix), ":")
	if len(parts) < 2 {
		return "", 0, "", false
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", 0, "", false
	}
	if len(parts) > 2 {
		arg = parts[2]
	}
	return parts[0], id, arg, true
}

func userErrorToast(err error) (string, error) {
	reply, err := userError(err)
	return firstLine(reply), err
}

// Telegram toasts are short; keep the first line and cap the length.
func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	if r := []rune(s); len(r) > 190 {
		s = string(r[:189]) + "…"
	}
	return s
}
