package telegram

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ahpalmer/consigliere/internal/channel"
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

func TestKeyboardGroupsButtonsByRowInOrder(t *testing.T) {
	rows := keyboardOf([]channel.Button{
		{Label: "a", Data: "1"}, {Label: "b", Data: "2"}, {Label: "c", Data: "3", Row: 1}, {Label: "d", Data: "4"},
	})
	if len(rows) != 2 || len(rows[0]) != 3 || rows[0][2].Text != "d" || rows[1][0].CallbackData != "3" {
		t.Fatalf("%+v", rows)
	}
	if keyboardOf(nil) != nil {
		t.Fatal("no buttons should mean no keyboard")
	}
}
