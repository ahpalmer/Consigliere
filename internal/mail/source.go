package mail

import (
	"context"
	"database/sql"
	"fmt"
	"html"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/radar"
	"github.com/ahpalmer/consigliere/internal/store"
)

// Source is the Radar source over the dedicated mailbox (watchlist: { name: ..., ats: email, board: gmail }).
// Each new message from an allowed sender is read once: job listings become items (only those whose URL appears in
// the email), and an update about one of the owner's applications goes to Updates.
//
// Allowed senders: job sites (AllowedDomains), application and scheduling systems (UpdateDomains), and companies
// the owner is pursuing (a sender domain part equal to the company's name, e.g. careers.google.com for Google).
// BlockedSenders (account-security mail) are never read, whatever the domain. A message skipped as "sender not
// allowed" is checked again on later scans, so it's read once its sender becomes allowed (a new pursuit).
type Source struct {
	DB             *sql.DB
	Mailbox        Mailbox
	Reader         Reader
	Clock          clock.Clock
	Query          string
	MaxMessages    int
	AllowedDomains []string
	UpdateDomains  []string
	BlockedSenders []string
	// The owner's own addresses that forward job mail here (e.g. an Outlook rule). Their messages are judged and read
	// as from the original sender in the forwarded header block, since forwarding makes the owner the sender.
	ForwardedBy []string
	// Companies the owner is pursuing; nil when there's no pipeline.
	Companies func(ctx context.Context) ([]string, error)
	// Updates handles application updates; nil ignores them.
	Updates UpdateHandler

	mu sync.Mutex // the hourly scan and the one-time backfill never read the same mail at once
}

// UpdateHandler applies an application update (to the pipeline) and returns a short note for the record.
type UpdateHandler interface {
	HandleUpdate(ctx context.Context, u Update) (string, error)
}

const noteNotAllowed = "sender not allowed"

func (s *Source) Kind() string       { return "email" }
func (s *Source) IsAggregator() bool { return true }

func (s *Source) Fetch(ctx context.Context, _ radar.WatchedSource) ([]radar.FetchedItem, error) {
	items, _, err := s.scan(ctx, s.Query, s.MaxMessages, false)
	return items, err
}

// Backfill reads older mail once (e.g. "newer_than:180d") for application updates only: listings that old are
// stale. Updates are recorded without alerts. Returns the handler's notes, for one summary message.
func (s *Source) Backfill(ctx context.Context, query string, max int) ([]string, error) {
	_, notes, err := s.scan(ctx, query, max, true)
	return notes, err
}

func (s *Source) scan(ctx context.Context, query string, max int, backfill bool) ([]radar.FetchedItem, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids, err := s.Mailbox.ListMessageIDs(ctx, query, max)
	if err != nil || len(ids) == 0 {
		return nil, nil, err
	}
	var companies []string
	if s.Companies != nil {
		if companies, err = s.Companies(ctx); err != nil {
			return nil, nil, err
		}
	}
	type seen struct{ from, note string }
	processed := map[string]seen{}
	rows, err := s.DB.QueryContext(ctx, `SELECT "Id", "From", coalesce("Note", '') FROM "ProcessedMessages" WHERE "Mailbox" = ? AND "Id" IN (`+
		store.Placeholders(len(ids))+`)`, append([]any{s.Mailbox.Name()}, store.Args(ids)...)...)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var id string
		var p seen
		if err := rows.Scan(&id, &p.from, &p.note); err != nil {
			rows.Close()
			return nil, nil, err
		}
		processed[id] = p
	}
	rows.Close()

	var messages []Message
	for _, id := range ids {
		// Skipped mail is looked at again once its sender is allowed, and so is a forward recorded under the owner's
		// address (before ForwardedBy listed it).
		if p, done := processed[id]; done && (p.note != noteNotAllowed || !s.allowed(p.from, companies) && !s.forwarder(p.from)) {
			continue
		}
		msg, err := s.Mailbox.Get(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		msg.ID = id
		messages = append(messages, s.unwrapForward(msg))
	}
	// Oldest first, so a "received" then a "rejection" for the same job are applied in that order.
	slices.SortStableFunc(messages, func(a, b Message) int { return receivedAt(a).Compare(receivedAt(b)) })

	var items []radar.FetchedItem
	var notes []string
	for _, msg := range messages {
		count, note := 0, ""
		switch {
		case msg.Own:
			note = "consigliere's own message"
		case !s.allowed(msg.From, companies):
			note = noteNotAllowed
			slog.Info("skipping mail from a sender that isn't allowed", "from", msg.From)
		default:
			found, update, err := s.read(ctx, msg)
			if err != nil {
				return items, notes, err
			}
			if !backfill {
				items = append(items, found...)
				count = len(found)
			}
			if update != nil && s.Updates != nil {
				update.Backfill = backfill
				if note, err = s.Updates.HandleUpdate(ctx, *update); err != nil {
					return items, notes, err
				}
				if note != "" {
					notes = append(notes, note)
				}
			}
		}
		// Recorded even when nothing was found, so the same message is never paid for twice.
		if _, err := s.DB.ExecContext(ctx, `INSERT OR REPLACE INTO "ProcessedMessages" ("Id", "Mailbox", "From", "Subject", "ReceivedAt", "ProcessedAt", "ItemCount", "Note")
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, msg.ID, s.Mailbox.Name(), msg.From, msg.Subject, store.FormatTimePtr(msg.ReceivedAt),
			store.FormatTime(s.Clock.Now()), count, nullString(note)); err != nil {
			return items, notes, err
		}
	}
	return items, notes, nil
}

func receivedAt(m Message) time.Time {
	if m.ReceivedAt == nil {
		return time.Time{}
	}
	return *m.ReceivedAt
}

// allowed: not a blocked (account-security) sender, and from a job site, an application system, or a company
// being pursued.
func (s *Source) allowed(from string, companies []string) bool {
	address := strings.ToLower(senderAddressOf(from))
	for _, b := range s.BlockedSenders {
		if b = strings.ToLower(strings.TrimSpace(b)); b != "" && strings.Contains(address, b) {
			return false
		}
	}
	return AllowedSender(from, s.AllowedDomains) || AllowedSender(from, s.UpdateDomains) || FromCompany(from, companies)
}

// FromCompany reports whether the sender's domain has a part equal to one of the companies' names with spaces and
// punctuation removed: careers.google.com for "Google", redhat.com for "Red Hat", but not notgoogle.com.
func FromCompany(from string, companies []string) bool {
	address := senderAddressOf(from)
	at := strings.LastIndex(address, "@")
	if at < 0 {
		return false
	}
	parts := strings.Split(strings.ToLower(address[at+1:]), ".")
	for _, c := range companies {
		name := strings.ReplaceAll(radar.NormalizeCompany(c), " ", "")
		if len(name) >= 3 && slices.Contains(parts[:max(len(parts)-1, 0)], name) {
			return true
		}
	}
	return false
}

// unwrapForward gives a message the owner forwarded its original sender and subject, so the allowlist, the reader
// and the record all see who really sent it. A forward with no recognizable header block keeps the owner as sender,
// which no allowlist matches, so it's skipped.
func (s *Source) unwrapForward(msg Message) Message {
	if !s.forwarder(msg.From) {
		return msg
	}
	from, ok := ForwardedSender(msg.Text)
	if !ok {
		// Not ToText: it decodes entities before stripping tags, so Outlook's "&lt;a@b&gt;" would vanish as a tag.
		from, ok = ForwardedSender(html.UnescapeString(htmlTag.ReplaceAllString(htmlBreak.ReplaceAllString(msg.HTML, "\n"), " ")))
	}
	if ok {
		msg.From = from
		msg.Subject = forwardPrefix.ReplaceAllString(msg.Subject, "")
	}
	return msg
}

// forwarder: the From header is one of the owner's forwarding addresses.
func (s *Source) forwarder(from string) bool {
	sender := strings.ToLower(senderAddressOf(from))
	return slices.ContainsFunc(s.ForwardedBy, func(a string) bool { return strings.ToLower(strings.TrimSpace(a)) == sender })
}

var (
	htmlBreak     = regexp.MustCompile(`(?i)<br\s*/?>|</(p|div|tr|li|h\d)\s*>`)
	htmlTag       = regexp.MustCompile(`<[^>]*>`)
	forwardedFrom = regexp.MustCompile(`(?im)^[\s>*]*From:\**\s*(.+)$`)
	emailAddress  = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	forwardPrefix = regexp.MustCompile(`(?i)^\s*((fwd?|fw)\s*:\s*)+`)
)

// ForwardedSender finds the original sender in a forwarded message: the first "From:" line with an address, as
// Outlook ("From: Name <a@b>", sometimes with "<mailto:a@b>") and Gmail ("---------- Forwarded message ---------")
// write it. A forwarding rule adds nothing above that block, so the first one is the original.
func ForwardedSender(text string) (string, bool) {
	for _, m := range forwardedFrom.FindAllStringSubmatch(text, -1) {
		address := emailAddress.FindString(m[1])
		if address == "" {
			continue
		}
		name := m[1]
		if i := strings.IndexAny(name, "<["); i >= 0 {
			name = name[:i]
		}
		name = strings.Trim(strings.TrimSpace(name), `"'`)
		if name == "" || strings.Contains(name, "@") {
			return address, true
		}
		return name + " <" + address + ">", true
	}
	return "", false
}

func senderAddressOf(from string) string {
	if m := senderAddress.FindStringSubmatch(from); m != nil {
		return m[1]
	}
	return strings.TrimSpace(from)
}

func (s *Source) read(ctx context.Context, msg Message) ([]radar.FetchedItem, *Update, error) {
	text := ToText(msg.HTML, msg.Text)
	urls := URLs(text)
	received := ""
	if msg.ReceivedAt != nil {
		received = "Received: " + msg.ReceivedAt.Format(time.RFC1123Z) + "\n"
	}
	reading, err := s.Reader.Read(ctx, fmt.Sprintf("From: %s\nSubject: %s\n%s\n%s", msg.From, msg.Subject, received, text))
	if err != nil {
		return nil, nil, err
	}
	var items []radar.FetchedItem
	for _, l := range reading.Listings {
		u := Canonicalize(l.URL)
		if !urls[strings.ToLower(u)] || strings.TrimSpace(l.Title) == "" || strings.TrimSpace(l.Company) == "" {
			slog.Warn("dropping extracted listing: URL not found in the email", "title", l.Title, "company", l.Company)
			continue
		}
		external := radar.Hash(u)
		// LinkedIn and Glassdoor ids are stable across alert emails, so the same job from two alerts dedupes.
		if id := LinkedInJobID(u); id != "" {
			external = "linkedin:" + id
		} else if id := GlassdoorJobID(u); id != "" {
			external = "glassdoor:" + id
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

	update := reading.Update
	if update != nil {
		var links []string
		for _, l := range update.Links {
			if u := Canonicalize(l); urls[strings.ToLower(u)] {
				links = append(links, u)
			}
		}
		update.Links = links
		update.Company, update.Role = strings.TrimSpace(update.Company), strings.TrimSpace(update.Role)
		update.MessageID, update.From, update.Subject, update.ReceivedAt = msg.ID, msg.From, msg.Subject, msg.ReceivedAt
	}
	return items, update, nil
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
