package kit

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ahpalmer/consigliere/internal/datapack"
	"github.com/ahpalmer/consigliere/internal/llm"
)

const testProfile = `
name: SAM SAMPLE
contact: "sam@example.com"
tagline: "Platform engineer"
skills: ["Languages: Go, C# & SQL"]
sections:
  - id: acme
    org: "Acme — Engineer"
    dates: "2022 – Present"
    min: 2
    max: 3
    bullets:
      - { id: a1, text: "Built the thing: shipped it to 10 teams." }
      - { id: a2, text: "Cut costs: saved 30%." }
      - { id: a3, text: "Ran on-call: fixed outages." }
      - { id: a4, text: "Mentored: two interns." }
      - { id: a5, text: "Unreviewed claim: cured everything.", draft: true }
  - id: army
    org: "Army — Officer"
    dates: "2015 – 2022"
    min: 1
    max: 1
    bullets:
      - { id: r1, text: "Led a platoon: 26 soldiers." }
education:
  - { text: "State U — B.S.", dates: "2015" }
`

func testPack(t *testing.T, files map[string]string) *datapack.Pack {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pack, err := datapack.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return pack
}

func loadTestProfile(t *testing.T) *Profile {
	t.Helper()
	p, ok, err := LoadProfile(testPack(t, map[string]string{ProfilePath: testProfile}))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	return p
}

func TestChooseOnlyKeepsReviewedBulletsFromTheRightSection(t *testing.T) {
	p := loadTestProfile(t)
	sel, warnings := p.Choose(Selection{
		"acme":  {"a3", "invented-1", "a5", "r1", "a3", "a1", "a2", "a4"},
		"bogus": {"a1"},
	})
	if got := sel["acme"]; !slices.Equal(got, []string{"a3", "a1", "a2"}) { // proposed order, deduped, capped at max 3
		t.Fatalf("acme = %v", got)
	}
	if got := sel["army"]; !slices.Equal(got, []string{"r1"}) { // omitted section topped up to min
		t.Fatalf("army = %v", got)
	}
	for _, want := range []string{`unknown bullet id "invented-1"`, `"a5": still marked draft`, `"r1": it belongs to section army`, `"a1": it belongs to section acme, not bogus`} {
		if !slices.ContainsFunc(warnings, func(w string) bool { return strings.Contains(w, want) }) {
			t.Errorf("missing warning %q in %v", want, warnings)
		}
	}
}

func TestChooseTopsUpToMinimumWithoutDrafts(t *testing.T) {
	p := loadTestProfile(t)
	sel, _ := p.Choose(Selection{"acme": {"a5"}})
	if got := sel["acme"]; !slices.Equal(got, []string{"a1", "a2"}) {
		t.Fatalf("acme = %v", got)
	}
}

func TestLoadProfileRejectsDuplicateIDs(t *testing.T) {
	bad := strings.Replace(testProfile, "id: a2", "id: a1", 1)
	if _, _, err := LoadProfile(testPack(t, map[string]string{ProfilePath: bad})); err == nil || !strings.Contains(err.Error(), "unique id") {
		t.Fatalf("err = %v", err)
	}
}

func docxText(t *testing.T, data []byte) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	var document string
	for _, f := range zr.File {
		names = append(names, f.Name)
		if f.Name == "word/document.xml" {
			rc, _ := f.Open()
			raw, _ := io.ReadAll(rc)
			rc.Close()
			document = string(raw)
		}
	}
	for _, want := range []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml", "word/styles.xml"} {
		if !slices.Contains(names, want) {
			t.Fatalf("missing %s in %v", want, names)
		}
	}
	return document
}

func TestRenderDOCXContainsOnlySelectedBullets(t *testing.T) {
	p := loadTestProfile(t)
	data, err := RenderDOCX(p, Selection{"acme": {"a2", "a1"}, "army": {"r1"}})
	if err != nil {
		t.Fatal(err)
	}
	doc := docxText(t, data)
	for _, want := range []string{"SAM SAMPLE", "Cut costs:", " saved 30%.", "Built the thing:", "Led a platoon:", "Go, C# &amp; SQL", "State U — B.S."} {
		if !strings.Contains(doc, want) {
			t.Errorf("document lacks %q", want)
		}
	}
	for _, unwanted := range []string{"Ran on-call", "cured everything"} {
		if strings.Contains(doc, unwanted) {
			t.Errorf("document has unselected %q", unwanted)
		}
	}
	if strings.Index(doc, "Cut costs") > strings.Index(doc, "Built the thing") {
		t.Error("bullets not in selected order")
	}
	if _, err := RenderDOCX(p, Selection{"acme": {"nope"}}); err == nil {
		t.Fatal("rendered an unknown bullet")
	}
}

const linkedInExport = "\xef\xbb\xbfNotes:\n" +
	`"When exporting your connection data, you may notice that some of the email addresses are missing."` + "\n\n" +
	"First Name,Last Name,URL,Email Address,Company,Position,Connected On\n" +
	"Dana,Lee,https://www.linkedin.com/in/dana,,Google LLC,Staff SRE,03 Jan 2024\n" +
	"Ari,Kim,https://www.linkedin.com/in/ari,,Amazon Web Services (AWS),SDE II,10 Feb 2023\n" +
	"Pat,Ng,https://www.linkedin.com/in/pat,,Sierra Nevada Corporation,Engineer,11 Mar 2022\n" +
	`Lou,"O'Brien, Jr.",https://www.linkedin.com/in/lou,,Google,"Manager, Infra",12 Apr 2021` + "\n"

func TestParseConnectionsAndMatchCompanies(t *testing.T) {
	all, err := ParseConnections(strings.NewReader(linkedInExport))
	if err != nil || len(all) != 4 {
		t.Fatalf("%d %v", len(all), err)
	}
	names := func(cs []Connection) []string {
		var out []string
		for _, c := range cs {
			out = append(out, c.Name)
		}
		return out
	}
	if got := names(AtCompany(all, "Google")); !slices.Equal(got, []string{"Dana Lee", "Lou O'Brien, Jr."}) {
		t.Fatalf("google: %v", got)
	}
	// Longer names are only "similar": right for Amazon/AWS, wrong for Sierra/Sierra Nevada, so the owner judges.
	if got := AtCompany(all, "Amazon"); len(got) != 0 {
		t.Fatalf("amazon exact: %v", names(got))
	}
	if got := names(SimilarCompany(all, "Amazon")); !slices.Equal(got, []string{"Ari Kim"}) {
		t.Fatalf("amazon similar: %v", got)
	}
	if got := AtCompany(all, "Sierra"); len(got) != 0 {
		t.Fatalf("Sierra must not match Sierra Nevada exactly: %v", names(got))
	}
	if got := SimilarCompany(all, "Google"); len(got) != 0 {
		t.Fatalf("exact matches aren't 'similar': %v", names(got))
	}
	if _, err := ParseConnections(strings.NewReader("a,b\n1,2\n")); err == nil {
		t.Fatal("accepted a CSV without the export's columns")
	}
}

func TestPeopleSearchLinksAreSearchURLsOnly(t *testing.T) {
	links := PeopleSearchLinks("Google")
	if len(links) != 2 {
		t.Fatal(links)
	}
	for _, l := range links {
		if !strings.HasPrefix(l.URL, "https://www.linkedin.com/search/results/people/?keywords=") || !strings.Contains(l.URL, "%22Google%22") {
			t.Errorf("%s", l.URL)
		}
	}
}

func TestNetworksMatchCompanyAndGeneral(t *testing.T) {
	n, err := ParseNetworks("companies:\n  Google: [VetNet]\n  Amazon Web Services: [Warriors]\ngeneral: [ACP]\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := n.For("Google LLC"); !slices.Equal(got, []string{"VetNet"}) {
		t.Fatal(got)
	}
	if got := n.For("Amazon Web Services, Inc."); !slices.Equal(got, []string{"Warriors"}) {
		t.Fatal(got)
	}
	if got := n.For("Amazon"); len(got) != 0 { // exact names only; list each form in networks.yaml
		t.Fatal(got)
	}
	if got := n.For("Netflix"); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestLimitRunesPrefersSentenceEnds(t *testing.T) {
	if s, cut := limitRunes("Short.", 300); cut || s != "Short." {
		t.Fatal(s)
	}
	long := "First sentence is here. " + strings.Repeat("word ", 80)
	s, cut := limitRunes(long, 40)
	if !cut || s != "First sentence is here." {
		t.Fatalf("%q", s)
	}
	s, _ = limitRunes(strings.Repeat("é", 400), 300)
	if n := len([]rune(s)); n != 300 {
		t.Fatal(n)
	}
}

func TestFileName(t *testing.T) {
	if got := fileName("ANDREW PALMER", "GitHub, Inc."); got != "Andrew_Palmer_Resume_-_GitHub_Inc.docx" {
		t.Fatal(got)
	}
}

type fakeLLM struct {
	structured string
	requests   []llm.StructuredRequest
}

func (f *fakeLLM) Structured(_ context.Context, req llm.StructuredRequest) (string, error) {
	f.requests = append(f.requests, req)
	return f.structured, nil
}

func (f *fakeLLM) Generate(context.Context, llm.GenerateRequest) (string, error) { return "draft", nil }

func TestBuildKit(t *testing.T) {
	pack := testPack(t, map[string]string{
		ProfilePath:     testProfile,
		ConnectionsPath: linkedInExport,
		NetworksPath:    "companies:\n  Google: [VetNet]\ngeneral: [ACP mentorship]\n",
		SummaryPath:     "Platform engineer.",
	})
	draft, _ := json.Marshal(map[string]any{
		"sections":         []map[string]any{{"section": "acme", "bullets": []string{"a2", "made-up"}}},
		"focus":            []string{"Fleet scale"},
		"gaps":             []string{},
		"connection_note":  "Hi [Name], " + strings.Repeat("really ", 60) + "keen.",
		"referral_request": "Hi [Name], would you refer me?",
	})
	fake := &fakeLLM{structured: string(draft)}
	k := &Kit{Pack: pack, LLM: fake, Owner: "Sam"}
	result, err := k.Build(context.Background(), Job{Company: "Google", Title: "SRE", URL: "https://example.com/job", Description: "Run fleets."})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Apply kit: SRE · Google", "People you know at Google (2)", "Dana Lee — Staff SRE", "Veterans at Google",
		"Google: VetNet", "ACP mentorship", "Fleet scale", "Referral request:", `dropped unknown bullet id "made-up"`, "trimmed", "1 draft bullets"} {
		if !strings.Contains(result.Text, want) {
			t.Errorf("kit text lacks %q:\n%s", want, result.Text)
		}
	}
	if result.FileName != "Sam_Sample_Resume_-_Google.docx" {
		t.Error(result.FileName)
	}
	doc := docxText(t, result.File)
	if !strings.Contains(doc, "Cut costs") || !strings.Contains(doc, "Built the thing") || strings.Contains(doc, "cured everything") {
		t.Error("resume selection wrong")
	}

	// The model can't even propose drafts: they're absent from the schema's enum and from the bullet bank it sees.
	req := fake.requests[0]
	if strings.Contains(req.System[1].Text, "a5") {
		t.Error("draft bullet shown to the model")
	}
	schema, _ := json.Marshal(req.Schema)
	if strings.Contains(string(schema), `"a5"`) || !strings.Contains(string(schema), `"a1"`) {
		t.Errorf("schema enum wrong: %s", schema)
	}
}

func TestSamplePackProfileIsValid(t *testing.T) {
	pack, err := datapack.Open("../../samples/data-pack")
	if err != nil {
		t.Fatal(err)
	}
	p, ok, err := LoadProfile(pack)
	if err != nil || !ok || p.DraftCount() != 1 {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	text, _, _ := pack.ReadText(ConnectionsPath)
	all, err := ParseConnections(strings.NewReader(text))
	if err != nil || len(AtCompany(all, "Initech")) != 1 {
		t.Fatalf("%v %v", all, err)
	}
}

func TestBuildKitWithoutOptionalFiles(t *testing.T) {
	draft := `{"focus":[],"gaps":[],"connection_note":"Hi [Name], short.","referral_request":""}`
	k := &Kit{Pack: testPack(t, map[string]string{}), LLM: &fakeLLM{structured: draft}, Owner: "Sam"}
	result, err := k.Build(context.Background(), Job{Company: "Acme", Title: "Engineer"})
	if err != nil {
		t.Fatal(err)
	}
	if result.File != nil || !strings.Contains(result.Text, "export as profile/connections.csv") || !strings.Contains(result.Text, "add profile/bullets.yaml") {
		t.Fatal(result.Text)
	}
}
