package kit

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
	"time"
)

// Letter page with 0.5" margins, in twentieths of a point.
const (
	pageWidth   = 12240
	pageHeight  = 15840
	margin      = 720
	rightTab    = pageWidth - 2*margin
	bulletShift = 270
)

// RenderDOCX lays out a one-page resume: header, skills, the selected bullets per section, education.
// It's a minimal OOXML package (no third-party library), so Word, Google Docs and LibreOffice all open it.
func RenderDOCX(p *Profile, sel Selection) ([]byte, error) {
	var body strings.Builder
	para(&body, paraProps{align: "center", after: 20}, run{text: p.Name, bold: true, size: 32})
	if p.Contact != "" {
		para(&body, paraProps{align: "center", after: 20}, run{text: p.Contact, size: 18})
	}
	if p.Tagline != "" {
		para(&body, paraProps{align: "center", after: 40}, run{text: p.Tagline, italic: true, size: 19})
	}

	if len(p.Skills) > 0 {
		heading(&body, "TECHNICAL SKILLS")
		for _, s := range p.Skills {
			label, rest := splitLead(s)
			para(&body, paraProps{after: 10}, run{text: label, bold: true}, run{text: rest})
		}
	}

	heading(&body, "EXPERIENCE")
	for _, s := range p.Sections {
		ids := sel[s.ID]
		if len(ids) == 0 {
			continue
		}
		if s.Org != "" {
			para(&body, paraProps{before: 60, tabs: true}, run{text: s.Org, bold: true}, run{tab: true}, run{text: s.Dates, bold: true})
		}
		if s.Team != "" {
			para(&body, paraProps{before: 20, tabs: true}, run{text: s.Team, italic: true}, run{tab: true}, run{text: s.TeamDates, italic: true})
		}
		for _, id := range ids {
			text, ok := p.BulletText(id)
			if !ok {
				return nil, fmt.Errorf("selection has unknown bullet %q", id)
			}
			lead, rest := splitLead(text)
			para(&body, paraProps{bullet: true, after: 10}, run{text: "•"}, run{tab: true}, run{text: lead, bold: true}, run{text: rest})
		}
	}

	if len(p.Education) > 0 {
		heading(&body, "EDUCATION")
		for _, e := range p.Education {
			para(&body, paraProps{tabs: true, after: 10}, run{text: e.Text}, run{tab: true}, run{text: e.Dates})
		}
	}

	document := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + body.String() +
		fmt.Sprintf(`<w:sectPr><w:pgSz w:w="%d" w:h="%d"/><w:pgMar w:top="%d" w:right="%d" w:bottom="%d" w:left="%d" w:header="0" w:footer="0" w:gutter="0"/></w:sectPr>`,
			pageWidth, pageHeight, margin, margin, margin, margin) +
		`</w:body></w:document>`

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range []struct{ name, content string }{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", packageRels},
		{"word/_rels/document.xml.rels", documentRels},
		{"word/styles.xml", styles},
		{"word/document.xml", document},
	} {
		// Fixed timestamps keep output byte-for-byte reproducible.
		w, err := zw.CreateHeader(&zip.FileHeader{Name: f.name, Method: zip.Deflate, Modified: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(f.content)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// splitLead splits "Lead-in: rest" so the lead-in can be bold. Lines without a short lead-in come back whole.
func splitLead(s string) (lead, rest string) {
	if i := strings.Index(s, ": "); i > 0 && i <= 90 {
		return s[:i+1], s[i+1:]
	}
	return "", s
}

type paraProps struct {
	align         string
	before, after int
	tabs, bullet  bool
}

type run struct {
	text         string
	bold, italic bool
	size         int // half-points; 0 = style default
	tab          bool
}

func para(sb *strings.Builder, p paraProps, runs ...run) {
	sb.WriteString("<w:p><w:pPr>")
	if p.tabs || p.bullet {
		sb.WriteString("<w:tabs>")
		if p.bullet {
			fmt.Fprintf(sb, `<w:tab w:val="left" w:pos="%d"/>`, bulletShift)
		}
		if p.tabs {
			fmt.Fprintf(sb, `<w:tab w:val="right" w:pos="%d"/>`, rightTab)
		}
		sb.WriteString("</w:tabs>")
	}
	fmt.Fprintf(sb, `<w:spacing w:before="%d" w:after="%d"/>`, p.before, p.after)
	if p.bullet {
		fmt.Fprintf(sb, `<w:ind w:left="%d" w:hanging="%d"/>`, bulletShift, bulletShift)
	}
	if p.align != "" {
		fmt.Fprintf(sb, `<w:jc w:val="%s"/>`, p.align)
	}
	sb.WriteString("</w:pPr>")
	for _, r := range runs {
		if !r.tab && r.text == "" {
			continue
		}
		sb.WriteString("<w:r>")
		if r.bold || r.italic || r.size > 0 {
			sb.WriteString("<w:rPr>")
			if r.bold {
				sb.WriteString("<w:b/>")
			}
			if r.italic {
				sb.WriteString("<w:i/>")
			}
			if r.size > 0 {
				fmt.Fprintf(sb, `<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, r.size, r.size)
			}
			sb.WriteString("</w:rPr>")
		}
		if r.tab {
			sb.WriteString("<w:tab/>")
		} else {
			sb.WriteString(`<w:t xml:space="preserve">`)
			xml.EscapeText(sb, []byte(r.text))
			sb.WriteString("</w:t>")
		}
		sb.WriteString("</w:r>")
	}
	sb.WriteString("</w:p>")
}

func heading(sb *strings.Builder, text string) {
	sb.WriteString(`<w:p><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="000000"/></w:pBdr><w:spacing w:before="120" w:after="40"/></w:pPr>`)
	sb.WriteString(`<w:r><w:rPr><w:b/><w:sz w:val="21"/><w:szCs w:val="21"/></w:rPr><w:t xml:space="preserve">`)
	xml.EscapeText(sb, []byte(text))
	sb.WriteString("</w:t></w:r></w:p>")
}

const contentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/></Types>`

const packageRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`

const documentRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`

// Calibri 10pt, single spacing, no space after paragraphs: dense enough for one page.
const styles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="Calibri" w:cs="Calibri"/><w:sz w:val="20"/><w:szCs w:val="20"/><w:lang w:val="en-US"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults><w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style></w:styles>`
