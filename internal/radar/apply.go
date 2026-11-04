package radar

import (
	"net/url"
	"strings"
)

// ApplyURL is the link that opens a posting's application form rather than its description, where the board
// has a separate form page: Lever's /apply, Ashby's /application, Greenhouse's #app anchor. Other links (company
// career sites, LinkedIn) are returned as they are.
func ApplyURL(posting string) string {
	u, err := url.Parse(strings.TrimSpace(posting))
	if err != nil || u.Host == "" {
		return posting
	}
	host := strings.ToLower(u.Host)
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch {
	case host == "jobs.lever.co" && len(segments) == 2:
		u.Path = "/" + strings.Join(segments, "/") + "/apply"
	case host == "jobs.ashbyhq.com" && len(segments) == 2:
		u.Path = "/" + strings.Join(segments, "/") + "/application"
	case (host == "boards.greenhouse.io" || host == "job-boards.greenhouse.io") && u.Fragment == "":
		u.Fragment = "app"
	default:
		return posting
	}
	return u.String()
}
