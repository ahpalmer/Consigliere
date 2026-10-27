// Package mail is a Radar source over a dedicated, forward-only mailbox of job-alert emails.
package mail

import (
	"html"
	"regexp"
	"strings"

	"github.com/ahpalmer/consigliere/internal/radar"
)

var (
	anchor      = regexp.MustCompile(`(?is)<a\b[^>]*?href\s*=\s*["']([^"']+)["'][^>]*>(.*?)</a\s*>`)
	urlPattern  = regexp.MustCompile(`(?i)https?://[^\s\[\]<>"']+`)
	linkedInJob = regexp.MustCompile(`(?i)linkedin\.com/(?:comm/)?jobs/view/(\d+)`)
)

// ToText converts an HTML email to plain text that keeps each link as "text [url]", so the extractor can cite real
// URLs. Image-only links (logos, tracking pixels) are dropped, and known URLs are shortened to canonical form.
func ToText(htmlBody, plain string) string {
	if strings.TrimSpace(htmlBody) == "" {
		return urlPattern.ReplaceAllStringFunc(plain, Canonicalize)
	}
	withLinks := anchor.ReplaceAllStringFunc(htmlBody, func(m string) string {
		parts := anchor.FindStringSubmatch(m)
		text := strings.TrimSpace(strings.ReplaceAll(radar.HTMLToText(parts[2]), "\n", " "))
		if text == "" {
			return " "
		}
		return " " + text + " [" + Canonicalize(html.UnescapeString(parts[1])) + "] "
	})
	return radar.HTMLToText(withLinks)
}

// URLs returns every URL in the text, canonicalized. Extracted listings must link to one of these.
func URLs(text string) map[string]bool {
	out := map[string]bool{}
	for _, u := range urlPattern.FindAllString(text, -1) {
		out[strings.ToLower(Canonicalize(strings.TrimRight(u, "]).,")))] = true
	}
	return out
}

// Canonicalize strips tracking so the same job always has the same URL. LinkedIn links collapse to /jobs/view/{id}/.
func Canonicalize(u string) string {
	u = strings.TrimSpace(u)
	if id := LinkedInJobID(u); id != "" {
		return "https://www.linkedin.com/jobs/view/" + id + "/"
	}
	return u
}

func LinkedInJobID(u string) string {
	if m := linkedInJob.FindStringSubmatch(u); m != nil {
		return m[1]
	}
	return ""
}
