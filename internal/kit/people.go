package kit

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ahpalmer/consigliere/internal/radar"
)

// Spreadsheet apps often save CSVs with a byte order mark, which would otherwise stick to the first column name.
const utf8BOM = "\xef\xbb\xbf"

// Connection is one row of LinkedIn's Connections.csv export (Settings → Data privacy → Get a copy of your data).
type Connection struct {
	Name, URL, Company, Position, ConnectedOn string
}

// ParseConnections reads the export. LinkedIn prefixes the header with a few lines of notes, so the header is found
// by its column names rather than assumed to be line one.
func ParseConnections(r io.Reader) ([]Connection, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	var (
		out    []Connection
		column map[string]int
	)
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if column == nil {
			candidate := map[string]int{}
			for i, name := range record {
				candidate[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(name, utf8BOM)))] = i
			}
			if _, ok := candidate["company"]; ok {
				if _, ok := candidate["first name"]; ok {
					column = candidate
				}
			}
			continue
		}
		get := func(name string) string {
			if i, ok := column[name]; ok && i < len(record) {
				return strings.TrimSpace(record[i])
			}
			return ""
		}
		c := Connection{
			Name:        strings.TrimSpace(get("first name") + " " + get("last name")),
			URL:         get("url"),
			Company:     get("company"),
			Position:    get("position"),
			ConnectedOn: get("connected on"),
		}
		if c.Name != "" && c.Company != "" {
			out = append(out, c)
		}
	}
	if column == nil {
		return nil, fmt.Errorf("no header with First Name and Company columns; is this LinkedIn's Connections.csv?")
	}
	return out, nil
}

// AtCompany returns connections at exactly this company (after dropping punctuation and Inc/LLC).
func AtCompany(all []Connection, company string) []Connection {
	return matching(all, company, func(a, b string) bool { return a == b })
}

// SimilarCompany returns connections whose company name extends this one or vice versa ("Amazon" ~ "Amazon Web
// Services", but also "Sierra" ~ "Sierra Nevada"), for the owner to judge.
func SimilarCompany(all []Connection, company string) []Connection {
	return matching(all, company, func(a, b string) bool { return strings.HasPrefix(a, b+" ") || strings.HasPrefix(b, a+" ") })
}

func matching(all []Connection, company string, match func(a, b string) bool) []Connection {
	target := radar.NormalizeCompany(company)
	if target == "" {
		return nil
	}
	var out []Connection
	for _, c := range all {
		if name := radar.NormalizeCompany(c.Company); name != "" && match(name, target) {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Networks is profile/networks.yaml: veteran groups per company plus programs that apply anywhere.
type Networks struct {
	Companies map[string][]string `yaml:"companies"`
	General   []string            `yaml:"general"`
}

func ParseNetworks(text string) (Networks, error) {
	var n Networks
	if err := yaml.Unmarshal([]byte(text), &n); err != nil {
		return n, fmt.Errorf("parse %s: %w", NetworksPath, err)
	}
	return n, nil
}

// For returns the company-specific groups for company.
func (n Networks) For(company string) []string {
	target := radar.NormalizeCompany(company)
	var out []string
	for name, groups := range n.Companies {
		if radar.NormalizeCompany(name) == target {
			out = append(out, groups...)
		}
	}
	sort.Strings(out)
	return out
}

// SearchLink is a people search the owner opens in their own LinkedIn session. Nothing is fetched or automated.
type SearchLink struct{ Label, URL string }

// PeopleSearchLinks builds keyword searches for veterans and MSSA alumni at a company.
func PeopleSearchLinks(company string) []SearchLink {
	company = strings.TrimSpace(company)
	if company == "" {
		return nil
	}
	quoted := `"` + strings.ReplaceAll(company, `"`, "") + `"`
	search := func(keywords string) string {
		return "https://www.linkedin.com/search/results/people/?keywords=" + url.QueryEscape(keywords)
	}
	return []SearchLink{
		{Label: "Veterans at " + company, URL: search(quoted + ` AND (veteran OR Army OR "Marine Corps" OR "Air Force" OR Navy OR "Coast Guard")`)},
		{Label: "MSSA alumni at " + company, URL: search(quoted + ` AND ("Microsoft Software & Systems Academy" OR MSSA)`)},
	}
}
