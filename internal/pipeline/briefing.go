package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ahpalmer/consigliere/internal/channel"
	"github.com/ahpalmer/consigliere/internal/radar"
)

// Briefing is the morning message: today's to-dos across the pipeline first, then the best new options.
// Long-silent applications get their own "ghosted?" question with buttons.
type Briefing struct {
	Pipeline    *Pipeline
	Digest      *radar.Digest
	Suggestions *Suggestions // optional: mentions applications prepared on the dashboard
	Channel     channel.Channel
	Cron        string
}

func (b *Briefing) Name() string     { return "morning-briefing" }
func (b *Briefing) Schedule() string { return b.Cron }

func (b *Briefing) Run(ctx context.Context) error {
	ghosts, err := b.Pipeline.GhostChecksDue(ctx)
	if err != nil {
		return err
	}
	for _, p := range ghosts {
		since := ""
		if p.AppliedAt != nil {
			since = " since you applied " + ago(*p.AppliedAt, b.Pipeline.Clock.Now())
		}
		if err := b.Channel.Notify(ctx, fmt.Sprintf("👻 %s · %s · %s: no word%s. Mark it ghosted?", p.Ref(), p.Title, p.Company, since),
			channel.Button{Label: "👻 Ghosted", Data: fmt.Sprintf("%sghost:%d:y", prefix, p.ID)},
			channel.Button{Label: "⏳ Keep waiting", Data: fmt.Sprintf("%sghost:%d:n", prefix, p.ID)}); err != nil {
			return err
		}
	}

	// Rejections from email are batched here rather than alerted: proposed, never applied without a tap.
	rejected, err := b.Pipeline.EmailRejections(ctx, b.Pipeline.Clock.Now().Add(-24*time.Hour))
	if err != nil {
		return err
	}
	for _, p := range rejected {
		if err := b.Channel.Notify(ctx, fmt.Sprintf("📭 %s · %s · %s: the email says they're not moving forward. File it as rejected?", p.Ref(), p.Title, p.Company),
			channel.Button{Label: "🗂 Rejected", Data: fmt.Sprintf("%sout:%d:rejected", prefix, p.ID)}); err != nil {
			return err
		}
	}

	text, err := b.Compose(ctx)
	if err != nil || text == "" {
		return err
	}
	return b.Channel.Notify(ctx, text)
}

// Compose builds the briefing text, or "" when there's nothing to say. Options shown are marked as sent.
func (b *Briefing) Compose(ctx context.Context) (string, error) {
	active, err := b.Pipeline.List(ctx, StageActive)
	if err != nil {
		return "", err
	}
	waiting, err := b.Pipeline.List(ctx, StageWaiting)
	if err != nil {
		return "", err
	}
	agenda := b.Pipeline.Agenda(active, waiting)
	options, err := b.Digest.Next(ctx)
	if err != nil {
		return "", err
	}
	ready := 0
	if b.Suggestions != nil {
		if ready, err = b.Suggestions.Ready(ctx); err != nil {
			return "", err
		}
	}
	if agenda == "" && options.Shown == 0 && ready == 0 {
		return "", nil
	}
	var sb strings.Builder
	sb.WriteString("☕ Morning briefing")
	if agenda != "" {
		sb.WriteString("\n\nToday:\n" + agenda)
	}
	if ready > 0 {
		fmt.Fprintf(&sb, "\n\nReady to apply: %d prepared on the dashboard (Suggested), each with a tailored resume, drafted answers and the form link.", ready)
	}
	if options.Shown > 0 {
		sb.WriteString("\n\n" + options.Text)
	}
	return sb.String(), nil
}
