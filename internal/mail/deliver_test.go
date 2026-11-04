package mail

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	netmail "net/mail"
	"strings"
	"testing"
	"time"

	"github.com/ahpalmer/consigliere/internal/radar"
)

func TestBuildMessageHasTheKitAsBodyAndTheResumeAttached(t *testing.T) {
	file := bytes.Repeat([]byte("PK\x03\x04 resume "), 40)
	raw, err := buildMessage("box@example.com", "Apply kit: SRE · Nestlé", "✍️ Apply kit\nline two", "Andrew_Resume_-_Nestlé.docx", file,
		time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	msg, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	subject, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if subject != "Apply kit: SRE · Nestlé" || msg.Header.Get("To") != "box@example.com" || msg.Header.Get(ownHeader) != "kit" {
		t.Fatalf("headers %v (subject %q)", msg.Header, subject)
	}
	if from, err := netmail.ParseAddress(msg.Header.Get("From")); err != nil || from.Name != "Consigliere" || from.Address != "box@example.com" {
		t.Fatalf("from %v %v", from, err)
	}

	_, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	parts := multipart.NewReader(msg.Body, params["boundary"])
	decoded := func(p *multipart.Part) []byte {
		data, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, p))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	body, err := parts.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if got := string(decoded(body)); got != "✍️ Apply kit\nline two" {
		t.Fatalf("body %q", got)
	}
	attachment, err := parts.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if attachment.FileName() != "Andrew_Resume_-_Nestlé.docx" || !bytes.Equal(decoded(attachment), file) {
		t.Fatalf("attachment %q", attachment.FileName())
	}
	if _, err := parts.NextPart(); err != io.EOF {
		t.Fatalf("expected two parts, got %v", err)
	}
	for _, line := range strings.Split(string(raw), "\r\n") {
		if len(line) > 998 {
			t.Fatalf("line longer than MIME allows: %d", len(line))
		}
	}
}

func TestSkipsItsOwnMessagesWithoutCallingTheLLM(t *testing.T) {
	src, mb, ex, db := newSource(t)
	mb.messages["k1"] = Message{ID: "k1", From: "Consigliere <box@gmail.com>", Own: true, Text: "✍️ Apply kit"}

	items, err := src.Fetch(context.Background(), radar.WatchedSource{})
	if err != nil || len(items) != 0 || ex.calls != 0 {
		t.Fatalf("items %d calls %d err %v", len(items), ex.calls, err)
	}
	var note string
	db.QueryRow(`SELECT "Note" FROM "ProcessedMessages" WHERE "Id" = 'k1'`).Scan(&note)
	if note != "consigliere's own message" {
		t.Fatalf("note %q", note)
	}
}
