package radar

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Line is a compact entry for digests.
func Line(i ItemView, now time.Time) string {
	var sb strings.Builder
	fit := "–"
	if i.FitScore != nil {
		fit = strconv.Itoa(*i.FitScore)
	}
	fmt.Fprintf(&sb, "• %s · %s · %s #%d\n", fit, i.Title, i.Company, i.ID)
	sb.WriteString("  " + joinNonEmpty(" · ", shorten(i.Locations, 50), Comp(i.CompMin, i.CompMax), Age(i.PostedAt, now)) + "\n")
	if strings.TrimSpace(i.FitSummary) != "" {
		sb.WriteString("  " + i.FitSummary + "\n")
	}
	sb.WriteString("  " + i.URL)
	return sb.String()
}

// Detail is the full view for alerts and /item.
func Detail(i ItemView, now time.Time) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s · %s  #%d\n", i.Title, i.Company, i.ID)
	fit := "not scored"
	if i.FitScore != nil {
		fit = strconv.Itoa(*i.FitScore)
	}
	fmt.Fprintf(&sb, "Fit %s · %s · %s\n", fit, orDefault(i.Category, "uncategorized"), orDefault(i.Tier, "no tier"))
	sb.WriteString(joinNonEmpty(" · ", shorten(i.Locations, 80), Comp(i.CompMin, i.CompMax)) + "\n")
	posted := "unknown"
	if i.PostedAt != nil {
		posted = fmt.Sprintf("%s (%s)", i.PostedAt.Format("2006-01-02"), Age(i.PostedAt, now))
	}
	fmt.Fprintf(&sb, "Posted: %s · first seen %s\n", posted, i.FirstSeenAt.Format("2006-01-02"))
	if strings.TrimSpace(i.FitSummary) != "" {
		sb.WriteString("\n" + i.FitSummary + "\n")
	}
	appendList(&sb, "Why", i.FitReasons)
	appendList(&sb, "Gaps", i.FitGaps)
	if i.Feedback != nil {
		if *i.Feedback > 0 {
			sb.WriteString("\nYou marked this 👍\n")
		} else {
			sb.WriteString("\nYou marked this 👎\n")
		}
	}
	sb.WriteString("\n" + i.URL)
	return sb.String()
}

func Age(posted *time.Time, now time.Time) string {
	if posted == nil {
		return ""
	}
	days := int(now.Sub(*posted).Hours() / 24)
	if days < 0 {
		days = 0
	}
	switch {
	case days == 0:
		return "posted today"
	case days < 14:
		return fmt.Sprintf("posted %dd ago", days)
	case days < 60:
		return fmt.Sprintf("posted %dw ago", days/7)
	default:
		return fmt.Sprintf("posted %dmo ago", days/30)
	}
}

func Comp(min, max *float64) string {
	k := func(v float64) string {
		return "$" + strings.TrimSuffix(strings.TrimSuffix(strconv.FormatFloat(v/1000, 'f', 1, 64), "0"), ".") + "K"
	}
	switch {
	case min != nil && max != nil && *min != *max:
		return k(*min) + "–" + k(*max)
	case min != nil:
		return k(*min)
	case max != nil:
		return k(*max)
	default:
		return ""
	}
}

func appendList(sb *strings.Builder, label, newlineSeparated string) {
	if strings.TrimSpace(newlineSeparated) == "" {
		return
	}
	sb.WriteString("\n" + label + ":\n")
	for _, line := range strings.Split(newlineSeparated, "\n") {
		if strings.TrimSpace(line) != "" {
			sb.WriteString("- " + line + "\n")
		}
	}
}

func joinNonEmpty(sep string, parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}

func shorten(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
