package radar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// FetchedItem is a source item normalized to one shape, before persistence.
type FetchedItem struct {
	ExternalID, Company, Title, URL, Description string
	// Every listed location. Sources that flag remote work add a "Remote" entry if none mentions it.
	Locations        []string
	CompMin, CompMax *float64
	CompCurrency     string
	PostedAt         *time.Time
}

// Source fetches items for one kind of watched source.
type Source interface {
	// Kind matches WatchedSource.Ats.
	Kind() string
	// IsAggregator: aggregators (alert emails, job sites) report items that usually also exist on a primary
	// source, with less detail. Their items defer to a primary item with the same company + title.
	IsAggregator() bool
	Fetch(ctx context.Context, src WatchedSource) ([]FetchedItem, error)
}

func normalizeLocations(raw []string, flaggedRemote bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range raw {
		l = strings.TrimSpace(l)
		if l == "" || seen[strings.ToLower(l)] {
			continue
		}
		seen[strings.ToLower(l)] = true
		out = append(out, l)
	}
	if flaggedRemote {
		mentioned := false
		for _, l := range out {
			if strings.Contains(strings.ToLower(l), "remote") {
				mentioned = true
				break
			}
		}
		if !mentioned {
			out = append(out, "Remote")
		}
	}
	return out
}

func payPtrs(p *PayRange) (*float64, *float64, string) {
	if p == nil {
		return nil, nil, ""
	}
	lo, hi := p.Min, p.Max
	return &lo, &hi, "USD"
}

// HTTPFetcher GETs JSON with retries. Some boards return 10MB+, so attempts get a generous timeout.
type HTTPFetcher struct {
	Client   *http.Client
	Attempts int
	Backoff  time.Duration
}

func NewHTTPFetcher() *HTTPFetcher {
	return &HTTPFetcher{Client: &http.Client{Timeout: 90 * time.Second}, Attempts: 3, Backoff: 2 * time.Second}
}

func (f *HTTPFetcher) GetJSON(ctx context.Context, url string, into any) error {
	var last error
	for attempt := 1; attempt <= f.Attempts; attempt++ {
		retry, err := f.get(ctx, url, into)
		if err == nil {
			return nil
		}
		last = err
		if !retry || attempt == f.Attempts {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(f.Backoff * time.Duration(attempt)):
		}
	}
	return last
}

// get returns whether a failure is worth retrying (network error, 429, 5xx).
func (f *HTTPFetcher) get(ctx context.Context, url string, into any) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "Consigliere/0.1 (personal feed reader)")
	req.Header.Set("Accept", "application/json")
	resp, err := f.Client.Do(req)
	if err != nil {
		return !errors.Is(err, context.Canceled), err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		retry := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return retry, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return false, fmt.Errorf("decode %s: %w", url, err)
	}
	return false, nil
}
