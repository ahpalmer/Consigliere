package radar

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Greenhouse public job board API: boards-api.greenhouse.io/v1/boards/{board}/jobs.
type Greenhouse struct{ HTTP *HTTPFetcher }

func (Greenhouse) Kind() string       { return "greenhouse" }
func (Greenhouse) IsAggregator() bool { return false }

func (g Greenhouse) Fetch(ctx context.Context, src WatchedSource) ([]FetchedItem, error) {
	var resp greenhouseResponse
	if err := g.HTTP.GetJSON(ctx, "https://boards-api.greenhouse.io/v1/boards/"+url.PathEscape(src.Board)+"/jobs?content=true", &resp); err != nil {
		return nil, err
	}
	return resp.items(src), nil
}

// ParseGreenhouse parses a raw board response (used by tests).
func ParseGreenhouse(raw []byte, src WatchedSource) ([]FetchedItem, error) {
	var resp greenhouseResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	return resp.items(src), nil
}

type greenhouseResponse struct {
	Jobs []struct {
		ID          int64  `json:"id"`
		Title       string `json:"title"`
		AbsoluteURL string `json:"absolute_url"`
		CompanyName string `json:"company_name"`
		Location    *struct {
			Name string `json:"name"`
		} `json:"location"`
		Content  string `json:"content"`
		Metadata []struct {
			Name  string          `json:"name"`
			Value json.RawMessage `json:"value"`
		} `json:"metadata"`
		FirstPublished *time.Time `json:"first_published"`
	} `json:"jobs"`
}

func (r greenhouseResponse) items(src WatchedSource) []FetchedItem {
	out := make([]FetchedItem, 0, len(r.Jobs))
	for _, j := range r.Jobs {
		description := HTMLToText(j.Content)
		remote := false
		for _, m := range j.Metadata {
			var v string
			if m.Name == "Location Type" && json.Unmarshal(m.Value, &v) == nil && strings.Contains(strings.ToLower(v), "remote") {
				remote = true
			}
		}
		var locations []string
		if j.Location != nil {
			locations = strings.FieldsFunc(j.Location.Name, func(r rune) bool { return r == '|' || r == ';' })
		}
		company := j.CompanyName
		if strings.TrimSpace(company) == "" {
			company = src.Name
		}
		lo, hi, currency := payPtrs(ExtractPayRange(description))
		out = append(out, FetchedItem{
			ExternalID:  strconv.FormatInt(j.ID, 10),
			Company:     strings.TrimSpace(company),
			Title:       strings.TrimSpace(j.Title),
			Locations:   normalizeLocations(locations, remote),
			URL:         j.AbsoluteURL,
			Description: description,
			CompMin:     lo, CompMax: hi, CompCurrency: currency,
			PostedAt: j.FirstPublished, // not updated_at: an edit date is not a posting date
		})
	}
	return out
}

// Lever postings API: api.lever.co/v0/postings/{board}?mode=json.
type Lever struct{ HTTP *HTTPFetcher }

func (Lever) Kind() string       { return "lever" }
func (Lever) IsAggregator() bool { return false }

func (l Lever) Fetch(ctx context.Context, src WatchedSource) ([]FetchedItem, error) {
	var postings []leverPosting
	if err := l.HTTP.GetJSON(ctx, "https://api.lever.co/v0/postings/"+url.PathEscape(src.Board)+"?mode=json", &postings); err != nil {
		return nil, err
	}
	return leverItems(postings, src), nil
}

func ParseLever(raw []byte, src WatchedSource) ([]FetchedItem, error) {
	var postings []leverPosting
	if err := json.Unmarshal(raw, &postings); err != nil {
		return nil, err
	}
	return leverItems(postings, src), nil
}

type leverPosting struct {
	ID         string `json:"id"`
	Text       string `json:"text"`
	HostedURL  string `json:"hostedUrl"`
	Categories *struct {
		Location     string   `json:"location"`
		AllLocations []string `json:"allLocations"`
	} `json:"categories"`
	WorkplaceType    string `json:"workplaceType"`
	DescriptionPlain string `json:"descriptionPlain"`
	AdditionalPlain  string `json:"additionalPlain"`
	Lists            []struct {
		Text    string `json:"text"`
		Content string `json:"content"`
	} `json:"lists"`
	SalaryRange *struct {
		Min      float64 `json:"min"`
		Max      float64 `json:"max"`
		Currency string  `json:"currency"`
		Interval string  `json:"interval"`
	} `json:"salaryRange"`
	CreatedAt *int64 `json:"createdAt"`
}

func leverItems(postings []leverPosting, src WatchedSource) []FetchedItem {
	out := make([]FetchedItem, 0, len(postings))
	for _, p := range postings {
		var lists []string
		for _, l := range p.Lists {
			lists = append(lists, l.Text+"\n"+HTMLToText(l.Content))
		}
		var parts []string
		for _, s := range []string{p.DescriptionPlain, strings.Join(lists, "\n\n"), p.AdditionalPlain} {
			if strings.TrimSpace(s) != "" {
				parts = append(parts, s)
			}
		}
		description := strings.TrimSpace(strings.Join(parts, "\n\n"))

		// Lever's salaryRange is optional; fall back to scanning the text.
		var pay *PayRange
		currency := ""
		if s := p.SalaryRange; s != nil && s.Min > 0 && s.Max > 0 && (s.Interval == "" || strings.Contains(strings.ToLower(s.Interval), "year")) {
			pay, currency = &PayRange{Min: s.Min, Max: s.Max}, s.Currency
		} else {
			pay = ExtractPayRange(description)
		}
		lo, hi, defaultCurrency := payPtrs(pay)
		if currency == "" {
			currency = defaultCurrency
		}

		var locations []string
		if p.Categories != nil {
			if len(p.Categories.AllLocations) > 0 {
				locations = p.Categories.AllLocations
			} else {
				locations = []string{p.Categories.Location}
			}
		}
		var posted *time.Time
		if p.CreatedAt != nil {
			t := time.UnixMilli(*p.CreatedAt).UTC()
			posted = &t
		}
		out = append(out, FetchedItem{
			ExternalID:  p.ID,
			Company:     strings.TrimSpace(src.Name),
			Title:       strings.TrimSpace(p.Text),
			Locations:   normalizeLocations(locations, strings.EqualFold(p.WorkplaceType, "remote")),
			URL:         p.HostedURL,
			Description: description,
			CompMin:     lo, CompMax: hi, CompCurrency: currency,
			PostedAt: posted,
		})
	}
	return out
}

// Ashby public job board API: api.ashbyhq.com/posting-api/job-board/{board}.
type Ashby struct{ HTTP *HTTPFetcher }

func (Ashby) Kind() string       { return "ashby" }
func (Ashby) IsAggregator() bool { return false }

func (a Ashby) Fetch(ctx context.Context, src WatchedSource) ([]FetchedItem, error) {
	var resp ashbyResponse
	if err := a.HTTP.GetJSON(ctx, "https://api.ashbyhq.com/posting-api/job-board/"+url.PathEscape(src.Board)+"?includeCompensation=true", &resp); err != nil {
		return nil, err
	}
	return resp.items(src), nil
}

func ParseAshby(raw []byte, src WatchedSource) ([]FetchedItem, error) {
	var resp ashbyResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	return resp.items(src), nil
}

type ashbyResponse struct {
	Jobs []struct {
		ID                 string  `json:"id"`
		Title              string  `json:"title"`
		Location           *string `json:"location"`
		SecondaryLocations []struct {
			Location *string `json:"location"`
		} `json:"secondaryLocations"`
		IsRemote         *bool      `json:"isRemote"`
		IsListed         *bool      `json:"isListed"`
		JobURL           string     `json:"jobUrl"`
		DescriptionPlain *string    `json:"descriptionPlain"`
		DescriptionHTML  string     `json:"descriptionHtml"`
		PublishedAt      *time.Time `json:"publishedAt"`
		Compensation     *struct {
			SummaryComponents []struct {
				CompensationType string   `json:"compensationType"`
				Interval         string   `json:"interval"`
				CurrencyCode     string   `json:"currencyCode"`
				MinValue         *float64 `json:"minValue"`
				MaxValue         *float64 `json:"maxValue"`
			} `json:"summaryComponents"`
		} `json:"compensation"`
	} `json:"jobs"`
}

func (r ashbyResponse) items(src WatchedSource) []FetchedItem {
	out := make([]FetchedItem, 0, len(r.Jobs))
	for _, j := range r.Jobs {
		if j.IsListed != nil && !*j.IsListed {
			continue
		}
		var description string
		if j.DescriptionPlain != nil {
			description = *j.DescriptionPlain
		} else {
			description = HTMLToText(j.DescriptionHTML)
		}

		var pay *PayRange
		currency := ""
		if j.Compensation != nil {
			for _, c := range j.Compensation.SummaryComponents {
				if c.CompensationType == "Salary" && c.MinValue != nil && c.MaxValue != nil && *c.MinValue > 0 && *c.MaxValue > 0 &&
					(c.Interval == "" || strings.Contains(c.Interval, "YEAR")) {
					pay, currency = &PayRange{Min: *c.MinValue, Max: *c.MaxValue}, c.CurrencyCode
					break
				}
			}
		}
		if pay == nil {
			pay = ExtractPayRange(description)
		}
		lo, hi, defaultCurrency := payPtrs(pay)
		if currency == "" {
			currency = defaultCurrency
		}

		var locations []string
		if j.Location != nil {
			locations = append(locations, *j.Location)
		}
		for _, s := range j.SecondaryLocations {
			if s.Location != nil {
				locations = append(locations, *s.Location)
			}
		}
		out = append(out, FetchedItem{
			ExternalID:  j.ID,
			Company:     strings.TrimSpace(src.Name),
			Title:       strings.TrimSpace(j.Title),
			Locations:   normalizeLocations(locations, j.IsRemote != nil && *j.IsRemote),
			URL:         j.JobURL,
			Description: description,
			CompMin:     lo, CompMax: hi, CompCurrency: currency,
			PostedAt: j.PublishedAt,
		})
	}
	return out
}
