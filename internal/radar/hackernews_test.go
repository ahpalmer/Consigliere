package radar

import (
	"strings"
	"testing"
)

// Shaped like the Algolia items API: the thread, with each company's post as a top-level comment.
const hnThread = `{"id": 1, "text": null, "created_at": "2026-11-02T15:00:00Z", "children": [
  {"id": 101, "created_at": "2026-11-02T15:05:00Z",
   "text": "Acme Robotics (https://acme.example) | Senior Platform Engineer, SRE | Raleigh, NC | Hybrid | $180k-$220k<p>We run fleets of robots. Go, Kubernetes.</p>",
   "children": [{"id": 201, "text": "Is this still open?", "children": []}]},
  {"id": 102, "created_at": "2026-11-02T16:00:00Z", "text": "Globex | REMOTE (US) | Staff Software Engineer | https://globex.example/jobs<p>Payments.</p>", "children": []},
  {"id": 103, "created_at": "2026-11-02T16:30:00Z", "text": "We're hiring! Email me if you like distributed systems.", "children": []},
  {"id": 104, "created_at": "2026-11-02T17:00:00Z", "text": "Initech | Remote | Onsite in Austin<p>Lots of roles, see site.</p>", "children": []},
  {"id": 105, "created_at": "2026-11-02T17:30:00Z", "text": null, "children": []}
]}`

func TestParsesWhoIsHiringPosts(t *testing.T) {
	items, err := ParseHNThreadJSON([]byte(hnThread))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items %+v", items)
	}
	acme := items[0]
	if acme.Company != "Acme Robotics" || acme.Title != "Senior Platform Engineer, SRE" || acme.ExternalID != "101" ||
		acme.URL != "https://news.ycombinator.com/item?id=101" || *acme.CompMin != 180_000 || *acme.CompMax != 220_000 {
		t.Fatalf("acme %+v", acme)
	}
	if strings.Join(acme.Locations, "; ") != "Raleigh, NC; Hybrid" || !strings.Contains(acme.Description, "robots") {
		t.Fatalf("acme locations %v", acme.Locations)
	}
	globex := items[1]
	if globex.Title != "Staff Software Engineer" || strings.Join(globex.Locations, "; ") != "REMOTE (US)" || globex.CompMin != nil {
		t.Fatalf("globex %+v", globex)
	}
}
