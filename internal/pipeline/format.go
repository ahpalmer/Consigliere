package pipeline

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ahpalmer/consigliere/internal/channel"
)

const timeFormat = "Mon Jan 2, 3:04 PM"

// Card is one pursuit with its checklist and the buttons that move it along.
func (pl *Pipeline) Card(p *Pursuit) (string, []channel.Button) {
	now := pl.Clock.Now()
	var sb strings.Builder
	icon := map[Stage]string{StageActive: "🎯", StageWaiting: "⏳", StageCompleted: "🗂"}[p.Stage]
	fmt.Fprintf(&sb, "%s %s · %s · %s\n", icon, p.Ref(), p.Title, p.Company)

	var facts []string
	switch p.Stage {
	case StageActive:
		facts = append(facts, "activated "+ago(p.ActivatedAt, now))
	case StageWaiting:
		if p.AppliedAt != nil {
			facts = append(facts, "applied "+ago(*p.AppliedAt, now))
		}
	case StageCompleted:
		facts = append(facts, completion(p))
	}
	if p.Fit != nil {
		facts = append(facts, "fit "+strconv.Itoa(*p.Fit))
	}
	if p.Tier != "" {
		facts = append(facts, p.Tier)
	}
	if p.ItemID != nil {
		facts = append(facts, "#"+strconv.FormatInt(*p.ItemID, 10))
	}
	sb.WriteString(strings.Join(facts, " · ") + "\n")
	if p.URL != "" {
		sb.WriteString(p.URL + "\n")
	}
	if p.InterviewAt != nil {
		fmt.Fprintf(&sb, "🎤 Interview: %s\n", p.InterviewAt.In(pl.Location).Format(timeFormat))
	}
	if p.Stage == StageWaiting && p.FollowUpAt != nil {
		fmt.Fprintf(&sb, "📬 Follow up: %s\n", p.FollowUpAt.In(pl.Location).Format("Mon Jan 2"))
	}

	if p.Stage != StageCompleted {
		sb.WriteString("\n")
		next := p.NextStep()
		for _, s := range p.VisibleSteps() {
			mark := "⬜"
			if _, done := p.Done[s.Key]; done {
				mark = "✅"
			}
			line := mark + " " + s.Label
			if next != nil && next.Key == s.Key {
				line += "  ← next"
			}
			sb.WriteString(line + "\n")
		}
	}
	if notes := lastLines(p.Notes, 3); notes != "" {
		sb.WriteString("\nNotes:\n" + notes + "\n")
	}
	return strings.TrimSpace(sb.String()), pl.buttons(p)
}

func (pl *Pipeline) buttons(p *Pursuit) []channel.Button {
	id := strconv.FormatInt(p.ID, 10)
	var b []channel.Button
	switch p.Stage {
	case StageCompleted:
		return nil
	case StageWaiting:
		b = append(b, channel.Button{Label: "📬 Followed up", Data: prefix + "fu:" + id})
	default:
		if next := p.NextStep(); next != nil {
			b = append(b, channel.Button{Label: "✅ " + next.Label, Data: prefix + "tick:" + id + ":" + next.Key})
		}
	}
	if p.InterviewAt != nil {
		b = append(b, channel.Button{Label: "🎤 Prep pack", Data: prefix + "prep:" + id})
	} else {
		b = append(b, channel.Button{Label: "✍️ Kit", Data: prefix + "kit:" + id})
	}
	if p.Stage == StageActive && p.AppliedAt == nil && p.InterviewAt == nil {
		b = append(b, channel.Button{Label: "📨 Applied", Data: prefix + "app:" + id, Row: 1})
	}
	b = append(b, channel.Button{Label: "🗂 Complete", Data: prefix + "cmp:" + id, Row: 1})
	return b
}

// OutcomeButtons ask how a pursuit ended.
func OutcomeButtons(id int64) []channel.Button {
	labels := map[string]string{"offer": "🎉 Offer", "rejected": "❌ Rejected", "ghosted": "👻 Ghosted", "withdrawn": "↩️ Withdrew", "dropped": "🗑 Dropped"}
	var b []channel.Button
	for i, o := range Outcomes {
		b = append(b, channel.Button{Label: labels[o], Data: fmt.Sprintf("%sout:%d:%s", prefix, id, o), Row: i / 3})
	}
	return b
}

// WaitingList is the /waiting view: one line per pursuit, oldest application first.
func (pl *Pipeline) WaitingList(ps []*Pursuit) string {
	if len(ps) == 0 {
		return "Nothing waiting. Tap 📨 Applied on an active job (or /applied P3) once you've submitted."
	}
	now := pl.Clock.Now()
	var sb strings.Builder
	fmt.Fprintf(&sb, "⏳ Waiting (%d)\n", len(ps))
	for _, p := range ps {
		line := fmt.Sprintf("\n• %s · %s · %s", p.Ref(), p.Title, p.Company)
		if p.AppliedAt != nil {
			line += "\n  applied " + ago(*p.AppliedAt, now)
		}
		if p.FollowUpAt != nil {
			if !p.FollowUpAt.After(now) {
				line += " · follow-up due"
			} else {
				line += " · follow up " + p.FollowUpAt.In(pl.Location).Format("Jan 2")
			}
		}
		sb.WriteString(line)
	}
	sb.WriteString("\n\n/followedup P3 after you follow up · /interview P3 <when> · /complete P3 <outcome>")
	return sb.String()
}

// CompletedList is the /completed view: totals, then the most recent pursuits.
func (pl *Pipeline) CompletedList(ps []*Pursuit) string {
	if len(ps) == 0 {
		return "Nothing completed yet."
	}
	outcomes, reached := map[string]int{}, map[string]int{}
	for _, p := range ps {
		outcomes[p.Outcome]++
		reached[p.StageReached]++
	}
	var totals []string
	for _, o := range Outcomes {
		if outcomes[o] > 0 {
			totals = append(totals, fmt.Sprintf("%d %s", outcomes[o], o))
		}
	}
	applied := reached["applied"] + reached["interview"] + reached["offer"]
	interviews := reached["interview"] + reached["offer"]

	var sb strings.Builder
	fmt.Fprintf(&sb, "🗂 Completed (%d): %s\n", len(ps), strings.Join(totals, " · "))
	fmt.Fprintf(&sb, "Funnel: %d applied → %d interviewed → %d offers\n", applied, interviews, reached["offer"])
	for i, p := range ps {
		if i == 20 {
			fmt.Fprintf(&sb, "\n…and %d older", len(ps)-20)
			break
		}
		fmt.Fprintf(&sb, "\n• %s · %s · %s\n  %s", p.Ref(), p.Title, p.Company, completion(p))
	}
	sb.WriteString("\n\n/reopen P3 to bring one back")
	return sb.String()
}

func completion(p *Pursuit) string {
	s := p.Outcome
	switch p.StageReached {
	case "interview":
		s += " after interviewing"
	case "applied":
		if p.Outcome != "ghosted" {
			s += " after applying"
		}
	case "active":
		s += " before applying"
	}
	if p.CompletedAt != nil {
		s += " · " + p.CompletedAt.Format("Jan 2")
	}
	return s
}

// Agenda is the to-do part of the morning briefing: interviews this week, thank-yous owed, follow-ups due,
// and each active pursuit's next step. Empty when there's nothing to do.
func (pl *Pipeline) Agenda(active, waiting []*Pursuit) string {
	now := pl.Clock.Now()
	var interviews, thanks, followUps, next []string
	for _, p := range active {
		switch {
		case p.InterviewAt != nil && p.InterviewAt.After(now) && p.InterviewAt.Before(now.AddDate(0, 0, 7)):
			line := fmt.Sprintf("🎤 %s: %s · %s · %s", p.InterviewAt.In(pl.Location).Format(timeFormat), p.Ref(), p.Title, p.Company)
			if _, done := p.Done["prep"]; !done {
				line += fmt.Sprintf(" (prep not done: /prep %s)", p.Ref())
			}
			interviews = append(interviews, line)
		case p.InterviewAt != nil && !p.InterviewAt.After(now) && p.InterviewAt.After(now.Add(-72*time.Hour)):
			if _, done := p.Done["thanks"]; !done {
				thanks = append(thanks, fmt.Sprintf("✉️ Thank-you for %s · %s (/thanks %s <who, what you discussed>)", p.Ref(), p.Company, p.Ref()))
			}
		default:
			if s := p.NextStep(); s != nil {
				next = append(next, fmt.Sprintf("🎯 %s · %s: %s", p.Ref(), p.Company, s.Label))
			}
		}
	}
	for _, p := range waiting {
		if p.FollowUpAt != nil && !p.FollowUpAt.After(now) {
			applied := ""
			if p.AppliedAt != nil {
				applied = " (applied " + ago(*p.AppliedAt, now) + ")"
			}
			followUps = append(followUps, fmt.Sprintf("📬 Follow up: %s · %s · %s%s", p.Ref(), p.Title, p.Company, applied))
		}
	}
	var sb strings.Builder
	for _, group := range [][]string{interviews, thanks, followUps, next} {
		for _, line := range group {
			sb.WriteString(line + "\n")
		}
	}
	return strings.TrimSpace(sb.String())
}

// ParseWhen reads a date and optional time from the start of s ("2026-11-03 14:00", "11/3 2pm", "tomorrow 9:30am",
// "fri 1 pm") in loc, and returns the rest of s. Dates without a year are the next such date.
func ParseWhen(s string, now time.Time, loc *time.Location) (time.Time, string, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return time.Time{}, "", fmt.Errorf("missing date")
	}
	today := now.In(loc)
	day, ok := parseDay(strings.ToLower(fields[0]), today, loc)
	if !ok {
		return time.Time{}, "", fmt.Errorf("couldn't read %q as a date; try 2026-11-03, 11/3, tomorrow, or fri", fields[0])
	}
	rest := fields[1:]
	hour, minute := 9, 0
	if len(rest) > 0 {
		token := strings.ToLower(rest[0])
		consumed := 1
		if len(rest) > 1 && (strings.EqualFold(rest[1], "am") || strings.EqualFold(rest[1], "pm")) {
			token += strings.ToLower(rest[1])
			consumed = 2
		}
		if h, m, ok := parseClock(token); ok {
			hour, minute = h, m
			rest = rest[consumed:]
		}
	}
	return time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, loc), strings.Join(rest, " "), nil
}

func parseDay(token string, today time.Time, loc *time.Location) (time.Time, bool) {
	midnight := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)
	switch token {
	case "today":
		return midnight, true
	case "tomorrow", "tmrw":
		return midnight.AddDate(0, 0, 1), true
	}
	for d := 0; d < 7; d++ {
		wd := time.Weekday(d)
		name := strings.ToLower(wd.String())
		if token == name || token == name[:3] {
			ahead := (int(wd) - int(today.Weekday()) + 7) % 7
			if ahead == 0 {
				ahead = 7
			}
			return midnight.AddDate(0, 0, ahead), true
		}
	}
	if t, err := time.ParseInLocation("2006-01-02", token, loc); err == nil {
		return t, true
	}
	if t, err := time.ParseInLocation("1/2/2006", token, loc); err == nil {
		return t, true
	}
	if t, err := time.ParseInLocation("1/2", token, loc); err == nil {
		t = time.Date(today.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
		if t.Before(midnight) {
			t = t.AddDate(1, 0, 0)
		}
		return t, true
	}
	return time.Time{}, false
}

func parseClock(token string) (int, int, bool) {
	// A bare number ("2") is ambiguous, so it's left as part of the note.
	for _, layout := range []string{"15:04", "3:04pm", "3pm"} {
		if t, err := time.Parse(layout, token); err == nil {
			return t.Hour(), t.Minute(), true
		}
	}
	return 0, 0, false
}

// ago: "today", "3d ago".
func ago(since, now time.Time) string {
	d := int(now.Sub(since).Hours() / 24)
	if d <= 0 {
		return "today"
	}
	return strconv.Itoa(d) + "d ago"
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
