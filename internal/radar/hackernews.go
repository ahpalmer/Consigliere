package radar

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// HackerNews reads the monthly "Ask HN: Who is hiring?" thread through the Algolia HN API: each top-level comment
// is one company's post, conventionally headed "Company | Role | Location | Remote | Pay". Parsing that header is
// plain code; posts that don't follow it are skipped. Watchlist: { name: HN Who is hiring, ats: hn, board: whoishiring }.
type HackerNews struct {
	HTTP *HTTPFetcher
	// Overrides the API base (tests).
	Base string
}

func (HackerNews) Kind() string { return "hn" }

// IsAggregator: a post usually also exists on the company's own board, with the full description.
func (HackerNews) IsAggregator() bool { return true }

func (h HackerNews) base() string {
	if h.Base != "" {
		return h.Base
	}
	return "https://hn.algolia.com/api/v1"
}

func (h HackerNews) Fetch(ctx context.Context, src WatchedSource) ([]FetchedItem, error) {
	var stories struct {
		Hits []struct {
			ObjectID string `json:"objectID"`
			Title    string `json:"title"`
		} `json:"hits"`
	}
	if err := h.HTTP.GetJSON(ctx, h.base()+"/search_by_date?tags=story,author_whoishiring&hitsPerPage=10", &stories); err != nil {
		return nil, err
	}
	for _, s := range stories.Hits {
		if strings.HasPrefix(strings.ToLower(s.Title), "ask hn: who is hiring") {
			var thread hnItem
			if err := h.HTTP.GetJSON(ctx, h.base()+"/items/"+s.ObjectID, &thread); err != nil {
				return nil, err
			}
			return ParseHNThread(thread), nil
		}
	}
	return nil, nil
}

type hnItem struct {
	ID        int64     `json:"id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
	Children  []hnItem  `json:"children"`
}

// ParseHNThreadJSON parses a raw thread response (used by tests).
func ParseHNThreadJSON(raw []byte) ([]FetchedItem, error) {
	var thread hnItem
	if err := json.Unmarshal(raw, &thread); err != nil {
		return nil, err
	}
	return ParseHNThread(thread), nil
}

var (
	hnRoleWords   = regexp.MustCompile(`(?i)\b(engineer|engineers|engineering|developer|developers|sre|devops|swe|scientist|architect|programmer|designer|manager|lead|director|head of|analyst|founding|intern|researcher|staff|principal)\b`)
	hnPlaceWords  = regexp.MustCompile(`(?i)\b(remote|onsite|on-site|hybrid|in-office|in office|relocation)\b`)
	hnParenURL    = regexp.MustCompile(`\s*\(\s*(https?://)?[\w.-]+\.[a-z]{2,}[^)]*\)`)
	hnPlaceLike   = regexp.MustCompile(`^[\p{L} .'-]+, ?[\p{L} .'-]+$`) // "Raleigh, NC", "London, UK"
	hnURLLike     = regexp.MustCompile(`(?i)^(https?://|www\.)|\.(com|io|ai|co|dev|org|net)(/|$)`)
	hnSeparatorRe = regexp.MustCompile(`\s+[|]\s+|\s*\|\s*`)
)

// ParseHNThread turns the thread's top-level comments into items.
func ParseHNThread(thread hnItem) []FetchedItem {
	var out []FetchedItem
	for _, c := range thread.Children {
		// HN separates paragraphs with a bare <p> (no closing tag), which HTMLToText doesn't treat as a break.
		text := HTMLToText(strings.ReplaceAll(c.Text, "<p>", "\n<p>"))
		header, _, _ := strings.Cut(text, "\n")
		if !strings.Contains(header, "|") {
			continue // not in the "Company | Role | ..." format
		}
		segments := hnSeparatorRe.Split(header, -1)
		company := strings.TrimSpace(hnParenURL.ReplaceAllString(segments[0], ""))
		if company == "" || len(company) > 80 {
			continue
		}
		var roles, places []string
		for _, seg := range segments[1:] {
			seg = strings.TrimSpace(seg)
			switch {
			case seg == "" || strings.Contains(seg, "$") || hnURLLike.MatchString(seg):
			case hnPlaceWords.MatchString(seg):
				places = append(places, seg)
			case hnRoleWords.MatchString(seg): // before "City, ST": "Platform Engineer, SRE" has a comma too
				roles = append(roles, seg)
			case hnPlaceLike.MatchString(seg):
				places = append(places, seg)
			}
		}
		if len(roles) == 0 {
			continue // no role named in the header: can't be filtered or scored usefully
		}
		lo, hi, currency := payPtrs(ExtractPayRange(text))
		posted := c.CreatedAt
		out = append(out, FetchedItem{
			ExternalID:  strconv.FormatInt(c.ID, 10),
			Company:     company,
			Title:       strings.Join(roles, " / "),
			Locations:   normalizeLocations(places, hnPlaceWords.MatchString(header) && strings.Contains(strings.ToLower(header), "remote")),
			URL:         "https://news.ycombinator.com/item?id=" + strconv.FormatInt(c.ID, 10),
			Description: text,
			CompMin:     lo, CompMax: hi, CompCurrency: currency,
			PostedAt: &posted,
		})
	}
	return out
}
