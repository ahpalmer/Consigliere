package mail

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/radar"
	"github.com/ahpalmer/consigliere/internal/store"
)

// Source is the Radar source over the dedicated mailbox (watchlist: { name: ..., ats: email, board: gmail }).
// Each new message from an allowed sender is extracted once; listings whose URL doesn't appear in the email are
// discarded.
type Source struct {
	DB             *sql.DB
	Mailbox        Mailbox
	Extractor      Extractor
	Clock          clock.Clock
	Query          string
	MaxMessages    int
	AllowedDomains []string
}

func (s *Source) Kind() string       { return "email" }
func (s *Source) IsAggregator() bool { return true }

func (s *Source) Fetch(ctx context.Context, _ radar.WatchedSource) ([]radar.FetchedItem, error) {
	ids, err := s.Mailbox.ListMessageIDs(ctx, s.Query, s.MaxMessages)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	processed := map[string]bool{}
	rows, err := s.DB.QueryContext(ctx, `SELECT "Id" FROM "ProcessedMessages" WHERE "Mailbox" = ? AND "Id" IN (`+store.Placeholders(len(ids))+`)`,
		append([]any{s.Mailbox.Name()}, store.Args(ids)...)...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		processed[id] = true
	}
	rows.Close()

	var items []radar.FetchedItem
	for _, id := range ids {
		if processed[id] {
			continue
		}
		msg, err := s.Mailbox.Get(ctx, id)
		if err != nil {
			return items, err
		}
		count, note := 0, ""
		if !AllowedSender(msg.From, s.AllowedDomains) {
			note = "sender not allowed"
			slog.Info("skipping mail from a sender that isn't allowed", "from", msg.From)
		} else {
			found, err := s.extract(ctx, msg)
			if err != nil {
				return items, err
			}
			items = append(items, found...)
			count = len(found)
		}
		// Recorded even when nothing was found, so the same message is never paid for twice.
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO "ProcessedMessages" ("Id", "Mailbox", "From", "Subject", "ReceivedAt", "ProcessedAt", "ItemCount", "Note")
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, s.Mailbox.Name(), msg.From, msg.Subject, store.FormatTimePtr(msg.ReceivedAt),
			store.FormatTime(s.Clock.Now()), count, nullString(note)); err != nil {
			return items, err
		}
	}
	return items, nil
}

func (s *Source) extract(ctx context.Context, msg Message) ([]radar.FetchedItem, error) {
	text := ToText(msg.HTML, msg.Text)
	urls := URLs(text)
	listings, err := s.Extractor.Extract(ctx, fmt.Sprintf("From: %s\nSubject: %s\n\n%s", msg.From, msg.Subject, text))
	if err != nil {
		return nil, err
	}
	var items []radar.FetchedItem
	for _, l := range listings {
		u := Canonicalize(l.URL)
		if !urls[strings.ToLower(u)] || strings.TrimSpace(l.Title) == "" || strings.TrimSpace(l.Company) == "" {
			slog.Warn("dropping extracted listing: URL not found in the email", "title", l.Title, "company", l.Company)
			continue
		}
		external := radar.Hash(u)
		// LinkedIn ids are stable across alert emails, so the same job from two alerts dedupes.
		if id := LinkedInJobID(u); id != "" {
			external = "linkedin:" + id
		}
		var locations []string
		if strings.TrimSpace(l.Location) != "" {
			locations = []string{strings.TrimSpace(l.Location)}
		}
		var description []string
		for _, part := range []string{l.Snippet, payLine(l.Salary), "(From an alert email: " + msg.Subject + ". Full description not included.)"} {
			if part != "" {
				description = append(description, part)
			}
		}
		item := radar.FetchedItem{
			ExternalID:  external,
			Company:     strings.TrimSpace(l.Company),
			Title:       strings.TrimSpace(l.Title),
			Locations:   locations,
			URL:         u,
			Description: strings.Join(description, "\n"),
			PostedAt:    PostedAt(l.Posted, msg.ReceivedAt),
		}
		if pay := radar.ExtractPayRange(l.Salary); pay != nil {
			lo, hi := pay.Min, pay.Max
			item.CompMin, item.CompMax, item.CompCurrency = &lo, &hi, "USD"
		}
		items = append(items, item)
	}
	return items, nil
}

func payLine(salary string) string {
	if salary == "" {
		return ""
	}
	return "Pay: " + salary
}

var senderAddress = regexp.MustCompile(`<([^>]+)>`)

// AllowedSender reports whether the From header's domain is (a subdomain of) an allowed domain.
func AllowedSender(from string, domains []string) bool {
	address := from
	if m := senderAddress.FindStringSubmatch(from); m != nil {
		address = m[1]
	}
	at := strings.LastIndex(address, "@")
	domain := strings.ToLower(strings.TrimSpace(address[at+1:]))
	for _, d := range domains {
		d = strings.ToLower(d)
		if domain == d || strings.HasSuffix(domain, "."+d) {
			return true
		}
	}
	return false
}

var relativeAge = regexp.MustCompile(`(?i)(\d+)\s*(minute|hour|day|week|month)s?\s+ago`)

// PostedAt turns "3 days ago" into a date relative to when the alert arrived. Alerts list newly posted jobs, so
// with no stated age the email's own date is the best available posting date (the source's date, not our clock).
func PostedAt(posted string, received *time.Time) *time.Time {
	if received == nil {
		return nil
	}
	r := *received
	m := relativeAge.FindStringSubmatch(posted)
	if m == nil {
		return &r
	}
	n, _ := strconv.Atoi(m[1])
	var t time.Time
	switch strings.ToLower(m[2]) {
	case "minute":
		t = r.Add(-time.Duration(n) * time.Minute)
	case "hour":
		t = r.Add(-time.Duration(n) * time.Hour)
	case "day":
		t = r.AddDate(0, 0, -n)
	case "week":
		t = r.AddDate(0, 0, -7*n)
	default:
		t = r.AddDate(0, 0, -30*n)
	}
	return &t
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
