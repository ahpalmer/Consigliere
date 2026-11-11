package kit

import (
	"net/url"
	"strings"
)

// ResearchLinks are pages about a company for the owner to open in their own browser: reviews, pay reports and
// interview questions on Glassdoor. Only links: Glassdoor's terms forbid automated access, so nothing is fetched.
func ResearchLinks(company string) []SearchLink {
	company = strings.TrimSpace(company)
	if company == "" {
		return nil
	}
	return []SearchLink{{
		Label: company + " on Glassdoor: reviews, salaries, interview questions",
		URL:   "https://www.glassdoor.com/Reviews/company-reviews.htm?sc.keyword=" + url.QueryEscape(company),
	}}
}
