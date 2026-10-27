package telegram

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestChunkSplitsOnRuneBoundaries(t *testing.T) {
	text := strings.Repeat("é", 9) // multi-byte runes
	parts := chunk(text, 4)
	if len(parts) != 3 || strings.Join(parts, "") != text {
		t.Fatalf("%q", parts)
	}
	for _, p := range parts {
		if !utf8.ValidString(p) || utf8.RuneCountInString(p) > 4 {
			t.Fatalf("bad chunk %q", p)
		}
	}
	if got := chunk("short", 4000); len(got) != 1 || got[0] != "short" {
		t.Fatalf("%q", got)
	}
}
