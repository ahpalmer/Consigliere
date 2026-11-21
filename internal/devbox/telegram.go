package devbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ahpalmer/consigliere/internal/channel"
	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/commands"
)

// SleepJob puts the dev box to sleep once it's been idle for sleep_after_hours, and says so. The VM also stops every
// night on its own schedule; this saves the hours in between.
type SleepJob struct {
	Box     *Box
	Channel channel.Channel
	Clock   clock.Clock
	Cron    string
}

func (j *SleepJob) Name() string     { return "devbox-sleep" }
func (j *SleepJob) Schedule() string { return j.Cron }

func (j *SleepJob) Run(ctx context.Context) error {
	slept, err := j.Box.SleepIfIdle(ctx, j.Clock.Now())
	if err != nil || slept.IdleFor == 0 {
		return err
	}
	return j.Channel.Notify(ctx, fmt.Sprintf("😴 Put the dev box to sleep after %s with nothing going on. /code <repo> (or the Code page) wakes it.",
		hours(slept.IdleFor)))
}

func hours(d time.Duration) string {
	h := d.Round(time.Hour) / time.Hour
	if h == 1 {
		return "an hour"
	}
	return fmt.Sprintf("%d hours", h)
}

// Commands is /code in Telegram: the repos and their sessions, and opening or stopping one.
type Commands struct{ Box *Box }

func (c *Commands) Commands() []commands.Info {
	if !c.Box.Enabled() {
		return nil
	}
	return []commands.Info{{Name: "/code", Description: "the dev box: '/code' lists repos, '/code <repo> [new]' opens a session, '/code stop <session>'"}}
}

func (c *Commands) Handle(ctx context.Context, command, args string, _ int64) (string, error) {
	if command != "/code" {
		return "", nil
	}
	if !c.Box.Enabled() {
		return "No dev box is set up (dev_box in code/repos.yaml).", nil
	}
	fields := strings.Fields(args)
	switch {
	case len(fields) == 0:
		return c.list(ctx)
	case len(fields) == 2 && strings.EqualFold(fields[0], "stop"):
		err := c.Box.Stop(ctx, fields[1])
		var refused *AgentError
		switch {
		case errors.As(err, &refused):
			return refused.Message, nil
		case err != nil:
			return "Couldn't reach the dev box: " + err.Error(), nil
		}
		return fmt.Sprintf("Stopped %s. Its conversation is kept; /code %s picks it up.", fields[1], strings.TrimRight(strings.TrimRight(fields[1], "0123456789"), "-")), nil
	case len(fields) <= 2:
		mode := Continue
		if len(fields) == 2 {
			if !strings.EqualFold(fields[1], "new") {
				return "Usage: /code <repo> [new], or /code stop <session>", nil
			}
			mode = New
		}
		return c.open(ctx, fields[0], mode)
	}
	return "Usage: /code, /code <repo> [new], or /code stop <session>", nil
}

func (c *Commands) list(ctx context.Context) (string, error) {
	st, err := c.Box.Status(ctx)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Dev box: %s", st.VM)
	if st.VMError != "" {
		fmt.Fprintf(&b, " (%s)", st.VMError)
	}
	b.WriteString("\n")
	for _, r := range st.Repos {
		b.WriteString("\n")
		if len(r.Sessions) > 0 {
			b.WriteString("🟢 ")
		} else {
			b.WriteString("⚪ ")
		}
		b.WriteString(r.Name)
		if r.Known {
			fmt.Fprintf(&b, " · %s", r.Branch)
			if r.Changes > 0 {
				fmt.Fprintf(&b, " · %d changed", r.Changes)
			}
			if r.Ahead != nil && *r.Ahead > 0 {
				fmt.Fprintf(&b, " · %d to push", *r.Ahead)
			}
			if len(r.PRs) > 0 {
				fmt.Fprintf(&b, " · %d PR", len(r.PRs))
				if len(r.PRs) > 1 {
					b.WriteString("s")
				}
			}
		}
		for _, s := range r.Sessions {
			fmt.Fprintf(&b, "\n    %s", s.Name)
		}
		if r.Waiting != "" {
			if r.Error != "" {
				fmt.Fprintf(&b, "\n    couldn't open: %s", r.Error)
			} else {
				b.WriteString("\n    opening once the box is up…")
			}
		}
	}
	b.WriteString("\n\n/code <repo> opens a session (its last conversation; add 'new' for a fresh one), /code stop <session> ends one.")
	return b.String(), nil
}

func (c *Commands) open(ctx context.Context, repo, mode string) (string, error) {
	// Repo names are case-sensitive on the box; let the owner type them any way.
	if st, err := c.Box.Status(ctx); err == nil {
		for _, r := range st.Repos {
			if strings.EqualFold(r.Name, repo) {
				repo = r.Name
			}
		}
	}
	started, err := c.Box.Start(ctx, repo, mode)
	var refused *AgentError
	switch {
	case errors.Is(err, ErrUnknownRepo):
		return fmt.Sprintf("No repo %q in code/repos.yaml. /code lists them.", repo), nil
	case errors.As(err, &refused):
		return refused.Message, nil
	case err != nil:
		return "", err
	case started.Waking:
		return fmt.Sprintf("Waking the dev box; %s opens in about a minute, then it's in the Claude app → Code.", repo), nil
	}
	return fmt.Sprintf("Opened %s. It's in the Claude app → Code in a few seconds.", started.Session), nil
}
