package radar

import "testing"

func TestApplyURLOpensTheForm(t *testing.T) {
	for in, want := range map[string]string{
		"https://jobs.lever.co/shieldai/0f1e2d3c":                "https://jobs.lever.co/shieldai/0f1e2d3c/apply",
		"https://jobs.lever.co/shieldai/0f1e2d3c/apply":          "https://jobs.lever.co/shieldai/0f1e2d3c/apply",
		"https://jobs.ashbyhq.com/sierra/1234-abcd":              "https://jobs.ashbyhq.com/sierra/1234-abcd/application",
		"https://job-boards.greenhouse.io/cloudflare/jobs/123":   "https://job-boards.greenhouse.io/cloudflare/jobs/123#app",
		"https://boards.greenhouse.io/epicgames/jobs/9?gh_jid=9": "https://boards.greenhouse.io/epicgames/jobs/9?gh_jid=9#app",
		"https://www.linkedin.com/jobs/view/4012345678/":         "https://www.linkedin.com/jobs/view/4012345678/",
		"https://careers.example.com/jobs/42?gh_jid=42":          "https://careers.example.com/jobs/42?gh_jid=42",
		"": "",
	} {
		if got := ApplyURL(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
