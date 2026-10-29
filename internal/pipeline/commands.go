package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ahpalmer/consigliere/internal/agent"
	"github.com/ahpalmer/consigliere/internal/channel"
	"github.com/ahpalmer/consigliere/internal/commands"
)

// prefix routes button taps to Commands.
const prefix = "pp:"

// Commands are the pipeline's slash commands and buttons. Anything that calls the model runs in the background
// and reports back, so the chat stays responsive.
type Commands struct {
	Pipeline *Pipeline
	Drafts   *Drafts
	Channel  channel.Channel
	Files    channel.FileSender
	Tasks    *agent.Tasks
	State    *agent.State
}

func (c *Commands) Commands() []commands.Info {
	return []commands.Info{
		{Name: "/active", Description: "what you're working on, one card each ('/active P3' for one)"},
		{Name: "/waiting", Description: "applied, no answer yet"},
		{Name: "/completed", Description: "finished pursuits and your funnel"},
		{Name: "/activate", Description: "start on a job: '/activate 123' (radar item) or '/activate <url> <company> - <title>'"},
		{Name: "/applied", Description: "you submitted: '/applied P3' (moves it to waiting)"},
		{Name: "/interview", Description: "'/interview P3 tue 2pm [notes]' (moves it back to active)"},
		{Name: "/followedup", Description: "you followed up: '/followedup P3'"},
		{Name: "/done", Description: "tick a step: '/done P3' (next step) or '/done P3 people'"},
		{Name: "/note", Description: "'/note P3 recruiter is Dana, wants a call Friday'"},
		{Name: "/complete", Description: "'/complete P3 rejected [note]' (offer, rejected, ghosted, withdrawn, dropped)"},
		{Name: "/reopen", Description: "bring a completed one back: '/reopen P3'"},
		{Name: "/kit", Description: "apply kit: tailored resume, people, outreach drafts: '/kit P3' or '/kit 123'"},
		{Name: "/prep", Description: "interview prep pack: '/prep P3 [notes about the interview]'"},
		{Name: "/thanks", Description: "thank-you note draft: '/thanks P3 <who, what you discussed>'"},
	}
}

func (c *Commands) Handle(ctx context.Context, command, args string, _ int64) (string, error) {
	switch command {
	case "/active":
		return c.active(ctx, args)
	case "/waiting":
		ps, err := c.Pipeline.List(ctx, StageWaiting)
		if err != nil {
			return "", err
		}
		return c.Pipeline.WaitingList(ps), nil
	case "/completed":
		ps, err := c.Pipeline.List(ctx, StageCompleted)
		if err != nil {
			return "", err
		}
		return c.Pipeline.CompletedList(ps), nil
	case "/activate":
		return c.activate(ctx, args)
	case "/kit":
		return c.kit(ctx, args)
	}

	p, rest, reply, err := c.resolve(ctx, args, command)
	if p == nil {
		return reply, err
	}
	switch command {
	case "/applied":
		if p, err = c.Pipeline.Applied(ctx, p.ID); err != nil {
			return userError(err)
		}
		return c.appliedReply(p), nil
	case "/interview":
		at, note, err := ParseWhen(rest, c.Pipeline.Clock.Now(), c.Pipeline.Location)
		if err != nil {
			return "Usage: /interview P3 <when> [notes], e.g. '/interview P3 tue 2pm phone screen with Dana'. " + err.Error(), nil
		}
		if p, err = c.Pipeline.Interview(ctx, p.ID, at, note); err != nil {
			return userError(err)
		}
		if note != "" {
			if p, err = c.Pipeline.AddNote(ctx, p.ID, "Interview: "+note); err != nil {
				return "", err
			}
		}
		return c.sendCard(ctx, p, "🎤 Interview set. I'll put it in the morning briefing; /prep "+p.Ref()+" for a prep pack.")
	case "/followedup":
		if p, err = c.Pipeline.FollowedUp(ctx, p.ID); err != nil {
			return userError(err)
		}
		if p.FollowUpAt != nil {
			return fmt.Sprintf("📬 Noted. Next nudge %s.", p.FollowUpAt.In(c.Pipeline.Location).Format("Mon Jan 2")), nil
		}
		return "📬 Noted. No more follow-up reminders for " + p.Ref() + ".", nil
	case "/done":
		step := strings.ToLower(strings.TrimSpace(rest))
		if step == "" {
			next := p.NextStep()
			if next == nil {
				return "Every step is done for " + p.Ref() + ".", nil
			}
			step = next.Key
		}
		if p, err = c.Pipeline.Tick(ctx, p.ID, step); err != nil {
			return userError(fmt.Errorf("%w; steps are %s", err, stepKeys()))
		}
		return c.sendCard(ctx, p, "")
	case "/note":
		if strings.TrimSpace(rest) == "" {
			return "Usage: /note P3 <text>", nil
		}
		if _, err = c.Pipeline.AddNote(ctx, p.ID, rest); err != nil {
			return "", err
		}
		return "📝 Noted on " + p.Ref() + ".", nil
	case "/complete":
		outcome, note, _ := strings.Cut(strings.TrimSpace(rest), " ")
		if outcome == "" {
			if err := c.Channel.Notify(ctx, fmt.Sprintf("How did %s · %s end?", p.Ref(), p.Company), OutcomeButtons(p.ID)...); err != nil {
				return "", err
			}
			return "", nil
		}
		if p, err = c.Pipeline.Complete(ctx, p.ID, outcome, strings.TrimSpace(note)); err != nil {
			return userError(err)
		}
		return fmt.Sprintf("🗂 %s completed: %s.", p.Ref(), completion(p)), nil
	case "/reopen":
		if p, err = c.Pipeline.Reopen(ctx, p.ID); err != nil {
			return userError(err)
		}
		return c.sendCard(ctx, p, "Reopened.")
	case "/prep":
		return c.prep(ctx, p, rest)
	case "/thanks":
		return c.thanks(ctx, p, rest)
	}
	return "", fmt.Errorf("unknown command %s", command)
}

func (c *Commands) active(ctx context.Context, args string) (string, error) {
	if strings.TrimSpace(args) != "" {
		p, _, reply, err := c.resolve(ctx, args, "/active")
		if p == nil {
			return reply, err
		}
		return c.sendCard(ctx, p, "")
	}
	ps, err := c.Pipeline.List(ctx, StageActive)
	if err != nil {
		return "", err
	}
	if len(ps) == 0 {
		return "Nothing active. Tap 🎯 Make active on an alert, or /activate <item id>.", nil
	}
	header := fmt.Sprintf("🎯 Active: %d", len(ps))
	if c.Pipeline.SoftCap > 0 {
		header += fmt.Sprintf(" of %d", c.Pipeline.SoftCap)
	}
	if err := c.Channel.Notify(ctx, header); err != nil {
		return "", err
	}
	for _, p := range ps {
		if _, err := c.sendCard(ctx, p, ""); err != nil {
			return "", err
		}
	}
	return "", nil
}

func (c *Commands) activate(ctx context.Context, args string) (string, error) {
	args = strings.TrimSpace(args)
	const usage = "Usage: /activate 123 (a radar item id) or /activate <url> <company> - <title>"
	if args == "" {
		return usage, nil
	}
	var (
		p       *Pursuit
		created = true
		err     error
	)
	if strings.HasPrefix(args, "http://") || strings.HasPrefix(args, "https://") {
		url, rest, _ := strings.Cut(args, " ")
		company, title, ok := strings.Cut(rest, " - ")
		if !ok || strings.TrimSpace(company) == "" || strings.TrimSpace(title) == "" {
			return usage, nil
		}
		p, err = c.Pipeline.ActivateManual(ctx, url, strings.TrimSpace(company), strings.TrimSpace(title))
	} else {
		id, perr := strconv.ParseInt(strings.TrimPrefix(args, "#"), 10, 64)
		if perr != nil {
			return usage, nil
		}
		p, created, err = c.Pipeline.ActivateItem(ctx, id)
		if errors.Is(err, ErrNotFound) {
			return fmt.Sprintf("No item #%d.", id), nil
		}
	}
	if err != nil {
		return "", err
	}
	return c.activated(ctx, p, created)
}

func (c *Commands) activated(ctx context.Context, p *Pursuit, created bool) (string, error) {
	if !created {
		if p.Stage == StageCompleted {
			return fmt.Sprintf("That's %s, completed (%s). /reopen %s to pick it back up.", p.Ref(), completion(p), p.Ref()), nil
		}
		return c.sendCard(ctx, p, fmt.Sprintf("Already in the pipeline as %s.", p.Ref()))
	}
	note := ""
	if n, err := c.Pipeline.CountActive(ctx); err == nil && c.Pipeline.SoftCap > 0 && n > c.Pipeline.SoftCap {
		note = fmt.Sprintf("Heads up: that's %d active (soft cap %d). Finish or drop one before adding more.", n, c.Pipeline.SoftCap)
	}
	return c.sendCard(ctx, p, note)
}

func (c *Commands) kit(ctx context.Context, args string) (string, error) {
	ref := strings.TrimSpace(args)
	if ref == "" {
		return "Usage: /kit P3 or /kit 123 (a radar item id)", nil
	}
	p, err := c.Pipeline.Resolve(ctx, ref)
	var itemID *int64
	label := ""
	switch {
	case err == nil:
		label = p.Ref() + " · " + p.Company
	case errors.Is(err, ErrNotFound):
		id, perr := strconv.ParseInt(strings.TrimPrefix(ref, "#"), 10, 64)
		if perr != nil {
			return "No pursuit " + ref + ".", nil
		}
		p, itemID, label = nil, &id, fmt.Sprintf("#%d", id)
	default:
		return "", err
	}
	if _, err := c.Drafts.StartKit(ctx, p, itemID, c.sendKit); err != nil {
		return userError(err)
	}
	return "✍️ Building the kit for " + label + ". About a minute.", nil
}

func (c *Commands) sendKit(ctx context.Context, d Draft, file []byte) error {
	if d.Status != DraftReady {
		return c.Channel.Notify(ctx, "⚠ The kit failed: "+d.Error)
	}
	if err := c.Channel.Notify(ctx, d.Text); err != nil {
		return err
	}
	if file != nil && c.Files != nil {
		return c.Files.SendFile(ctx, d.FileName, file, "Tailored resume. Review before sending.")
	}
	return nil
}

func (c *Commands) prep(ctx context.Context, p *Pursuit, extra string) (string, error) {
	_, err := c.Drafts.StartPrep(ctx, p, extra, func(ctx context.Context, d Draft, _ []byte) error {
		if d.Status != DraftReady {
			return c.Channel.Notify(ctx, "⚠ The prep pack failed: "+d.Error)
		}
		return c.Channel.Notify(ctx, fmt.Sprintf("🎤 Prep pack: %s · %s\n\n%s\n\nTap ✅ on the card or /done %s prep when you're ready.",
			p.Title, p.Company, d.Text, p.Ref()))
	})
	if err != nil {
		return userError(err)
	}
	return "🎤 Writing a prep pack for " + p.Ref() + ". About a minute.", nil
}

func (c *Commands) thanks(ctx context.Context, p *Pursuit, details string) (string, error) {
	if strings.TrimSpace(details) == "" {
		return "Tell me who and what you talked about: /thanks " + p.Ref() + " Dana (eng manager), we discussed their VM fleet migration", nil
	}
	_, err := c.Drafts.StartThanks(ctx, p, details, func(ctx context.Context, d Draft, _ []byte) error {
		if d.Status != DraftReady {
			return c.Channel.Notify(ctx, "⚠ The thank-you draft failed: "+d.Error)
		}
		return c.Channel.Notify(ctx, fmt.Sprintf("✉️ Thank-you draft for %s · %s (edit, then send it yourself):\n\n%s\n\n/done %s thanks once it's sent.",
			p.Ref(), p.Company, d.Text, p.Ref()))
	})
	if err != nil {
		return userError(err)
	}
	return "✉️ Drafting a thank-you for " + p.Ref() + ".", nil
}

// resolve reads the leading pursuit reference from args. When it can't, p is nil and reply says why.
func (c *Commands) resolve(ctx context.Context, args, command string) (*Pursuit, string, string, error) {
	ref, rest, _ := strings.Cut(strings.TrimSpace(args), " ")
	if ref == "" {
		return nil, "", fmt.Sprintf("Which one? e.g. %s P3 (pursuit) or %s 123 (radar item).", command, command), nil
	}
	p, err := c.Pipeline.Resolve(ctx, ref)
	if errors.Is(err, ErrNotFound) {
		return nil, "", fmt.Sprintf("No pursuit %s. /active, /waiting and /completed list them; /activate starts one.", ref), nil
	}
	if err != nil {
		return nil, "", "", err
	}
	return p, strings.TrimSpace(rest), "", nil
}

func (c *Commands) appliedReply(p *Pursuit) string {
	reply := fmt.Sprintf("📨 %s is in Waiting.", p.Ref())
	if p.FollowUpAt != nil {
		reply += fmt.Sprintf(" I'll nudge you to follow up on %s.", p.FollowUpAt.In(c.Pipeline.Location).Format("Mon Jan 2"))
	}
	return reply + fmt.Sprintf("\nMake sure %s's recruiting emails get redirected to the bot's inbox (their sender domain, plus subject words like application, interview, candidacy) so replies can be tracked.", p.Company)
}

// sendCard sends p's card (with its buttons) and returns note as the command's reply ("" sends nothing more).
func (c *Commands) sendCard(ctx context.Context, p *Pursuit, note string) (string, error) {
	text, buttons := c.Pipeline.Card(p)
	if err := c.Channel.Notify(ctx, text, buttons...); err != nil {
		return "", err
	}
	return note, nil
}

func stepKeys() string {
	keys := make([]string, len(Steps))
	for i, s := range Steps {
		keys[i] = s.Key
	}
	return strings.Join(keys, ", ")
}

// userError turns validation errors into a reply instead of "Something broke".
func userError(err error) (string, error) {
	if errors.Is(err, ErrNotFound) {
		return "Not found.", nil
	}
	return "Can't do that: " + err.Error(), nil
}
