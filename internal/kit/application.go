package kit

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"

	"github.com/ahpalmer/consigliere/internal/datapack"
)

// ApplicationPath holds the owner's standard answers for application forms (work authorization, start date, ...).
const ApplicationPath = "profile/application.yaml"

// Field is one standard application-form answer, shown with a copy button.
type Field struct {
	Label string `yaml:"label" json:"label"`
	Value string `yaml:"value" json:"value"`
}

var phoneLike = regexp.MustCompile(`^\+?[\d\s().-]{7,}$`)

// Applicant lists the answers every application form asks for: name and contact details from the resume's
// contact line, then profile/application.yaml's fields (empty values skipped). A field in application.yaml
// replaces a derived one with the same label.
func Applicant(pack *datapack.Pack) ([]Field, error) {
	var fields []Field
	profile, ok, err := LoadProfile(pack)
	if err != nil {
		return nil, err
	}
	if ok {
		fields = contactFields(profile.Name, profile.Contact)
	}

	text, ok, err := pack.ReadText(ApplicationPath)
	if err != nil || !ok {
		return fields, err
	}
	var file struct {
		Fields []Field `yaml:"fields"`
	}
	if err := yaml.Unmarshal([]byte(text), &file); err != nil {
		return nil, fmt.Errorf("parse %s: %w", ApplicationPath, err)
	}
	for _, f := range file.Fields {
		f.Label, f.Value = strings.TrimSpace(f.Label), strings.TrimSpace(f.Value)
		if f.Label == "" || f.Value == "" {
			continue
		}
		replaced := false
		for i := range fields {
			if strings.EqualFold(fields[i].Label, f.Label) {
				fields[i], replaced = f, true
			}
		}
		if !replaced {
			fields = append(fields, f)
		}
	}
	return fields, nil
}

// contactFields splits "ANDREW PALMER" and "(555) 123-4567 · me@example.com · linkedin.com/in/me" into form fields.
func contactFields(name, contact string) []Field {
	var fields []Field
	words := strings.Fields(name)
	for i, w := range words {
		r := []rune(strings.ToLower(w))
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	if len(words) > 0 {
		fields = append(fields, Field{"Full name", strings.Join(words, " ")}, Field{"First name", words[0]})
		if len(words) > 1 {
			fields = append(fields, Field{"Last name", words[len(words)-1]})
		}
	}
	for _, part := range strings.FieldsFunc(contact, func(r rune) bool { return r == '·' || r == '|' || r == '•' }) {
		part = strings.TrimSpace(part)
		lower := strings.ToLower(part)
		switch {
		case part == "":
		case strings.Contains(part, "@") && !strings.Contains(part, "/"):
			fields = append(fields, Field{"Email", part})
		case phoneLike.MatchString(part):
			fields = append(fields, Field{"Phone", part})
		case strings.Contains(lower, "linkedin.com"):
			fields = append(fields, Field{"LinkedIn", withScheme(part)})
		case strings.Contains(lower, "github.com"):
			fields = append(fields, Field{"GitHub", withScheme(part)})
		case strings.Contains(part, "."):
			fields = append(fields, Field{"Website", withScheme(part)})
		default:
			fields = append(fields, Field{"Location", part})
		}
	}
	return fields
}

// Forms validate URL fields, so "linkedin.com/in/me" becomes "https://linkedin.com/in/me".
func withScheme(u string) string {
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	return "https://" + u
}
