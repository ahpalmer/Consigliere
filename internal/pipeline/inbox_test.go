package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeInbox struct {
	subject, text, fileName string
	file                    []byte
	err                     error
}

func (f *fakeInbox) Deliver(_ context.Context, subject, text, fileName string, file []byte) error {
	f.subject, f.text, f.fileName, f.file = subject, text, fileName, file
	return f.err
}

func TestAFinishedKitAlsoGoesToTheInbox(t *testing.T) {
	f := newFixture(t)
	inbox := &fakeInbox{}
	f.cmd.Inbox = inbox
	kit := Draft{Status: DraftReady, Title: "Apply kit: SRE · Acme", Text: "✍️ Apply kit: SRE · Acme", FileName: "Resume_-_Acme.docx"}

	if err := f.cmd.sendKit(f.ctx, kit, []byte("docx")); err != nil {
		t.Fatal(err)
	}
	if inbox.subject != kit.Title || !strings.HasPrefix(inbox.text, kit.Text) || !strings.Contains(inbox.text, "Attached:") ||
		inbox.fileName != kit.FileName || string(inbox.file) != "docx" {
		t.Fatalf("delivered %+v", inbox)
	}
	if got := f.channel.last(t).text; got != kit.Text {
		t.Fatalf("chat got %q", got)
	}
}

func TestAnInboxFailureIsReportedInChatAndTheKitStillArrives(t *testing.T) {
	f := newFixture(t)
	f.cmd.Inbox = &fakeInbox{err: errors.New("Gmail refused (insufficient scopes)")}
	kit := Draft{Status: DraftReady, Title: "Apply kit: SRE · Acme", Text: "✍️ Apply kit: SRE · Acme"}

	if err := f.cmd.sendKit(f.ctx, kit, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.channel.sent) != 2 || f.channel.sent[0].text != kit.Text || !strings.Contains(f.channel.sent[1].text, "didn't reach your inbox: Gmail refused") {
		t.Fatalf("sent %+v", f.channel.sent)
	}
}

func TestAFailedKitIsNotDelivered(t *testing.T) {
	f := newFixture(t)
	inbox := &fakeInbox{}
	f.cmd.Inbox = inbox
	if err := f.cmd.sendKit(f.ctx, Draft{Status: DraftFailed, Error: "boom"}, nil); err != nil {
		t.Fatal(err)
	}
	if inbox.subject != "" {
		t.Fatalf("delivered a failed kit: %+v", inbox)
	}
}
