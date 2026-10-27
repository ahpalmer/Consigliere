package radar

import (
	"crypto/sha256"
	"encoding/hex"
	"html"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	blockTag   = regexp.MustCompile(`(?i)<\s*(br|/p|/div|/li|/h\d|li)\b[^>]*>`)
	anyTag     = regexp.MustCompile(`<[^>]+>`)
	spaces     = regexp.MustCompile("[ \t ]+")
	blankLines = regexp.MustCompile(`\s*\n[\s\n]*`)

	// Amount: "$405,000", "$196,000.00" (cents dropped), or "$211.4K", optionally followed by "/yr".
	// Separator: dash, en/em dash, "to", or "and".
	payRange = regexp.MustCompile(`\$\s?(?P<min>\d{1,3}(?:,\d{3})+|\d{2,3}(?:\.\d+)?\s?[kK])(?:\.\d{2})?(?:\s*/\s*(?:yr|year))?\s*(?:USD)?\s*(?:-|–|—|to|and)\s*\$?\s?(?P<max>\d{1,3}(?:,\d{3})+|\d{2,3}(?:\.\d+)?\s?[kK])(?:\.\d{2})?`)
)

// HTMLToText converts an HTML fragment to readable plain text, keeping paragraph/list breaks.
func HTMLToText(fragment string) string {
	if fragment == "" {
		return ""
	}
	// Some sources (Greenhouse) entity-escape the HTML itself, so decode before stripping tags.
	decoded := html.UnescapeString(fragment)
	withBreaks := blockTag.ReplaceAllString(decoded, "\n")
	stripped := html.UnescapeString(anyTag.ReplaceAllString(withBreaks, " "))
	collapsed := spaces.ReplaceAllString(stripped, " ")
	return strings.TrimSpace(blankLines.ReplaceAllString(collapsed, "\n\n"))
}

// Hash is a stable short hash of the parts. Byte-for-byte the same as the .NET version's, so a database
// shared between the two keeps recognizing known items.
func Hash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\u001f")))
	return hex.EncodeToString(sum[:16])
}

// Normalize lower-cases, trims, and collapses runs of spaces.
func Normalize(s string) string {
	return spaces.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), " ")
}

// ContainsKeyword is a whole-word, case-insensitive match, so "uk" doesn't match "Milwaukee".
func ContainsKeyword(text, keyword string) bool {
	kw := strings.ToLower(strings.TrimSpace(keyword))
	if kw == "" {
		return false
	}
	lower := strings.ToLower(text)
	for start := 0; start <= len(lower)-len(kw); {
		i := strings.Index(lower[start:], kw)
		if i < 0 {
			return false
		}
		i += start
		before, _ := utf8.DecodeLastRuneInString(lower[:i])
		after, _ := utf8.DecodeRuneInString(lower[i+len(kw):])
		if !isWordRune(before) && !isWordRune(after) {
			return true
		}
		_, size := utf8.DecodeRuneInString(lower[i:])
		start = i + size
	}
	return false
}

func isWordRune(r rune) bool {
	return r != utf8.RuneError && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

func ContainsAnyKeyword(text string, keywords []string) bool {
	for _, k := range keywords {
		if ContainsKeyword(text, k) {
			return true
		}
	}
	return false
}

// PayRange is an annual range found in text.
type PayRange struct{ Min, Max float64 }

// ExtractPayRange finds the first annual-looking pay range: "$405,000—$485,000", "$211.4K – $290.6K",
// "$180k to $220k", "between $132,000 and $198,450", "$160K/yr - $200K/yr". Ranges under $20K are ignored
// as hourly or monthly.
func ExtractPayRange(text string) *PayRange {
	for _, m := range payRange.FindAllStringSubmatch(text, -1) {
		lo, okLo := parseMoney(m[payRange.SubexpIndex("min")])
		hi, okHi := parseMoney(m[payRange.SubexpIndex("max")])
		if !okLo || !okHi || lo < 20_000 || hi < lo {
			continue
		}
		return &PayRange{Min: lo, Max: hi}
	}
	return nil
}

func parseMoney(s string) (float64, bool) {
	s = strings.ReplaceAll(strings.ReplaceAll(s, ",", ""), " ", "")
	thousands := strings.HasSuffix(s, "k") || strings.HasSuffix(s, "K")
	if thousands {
		s = s[:len(s)-1]
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	if thousands {
		v *= 1000
	}
	return v, true
}
