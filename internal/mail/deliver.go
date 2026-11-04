package mail

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	netmail "net/mail"
	"net/textproto"
	"time"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
)

// ownHeader marks messages Consigliere put in the mailbox itself, so the email source skips them.
const ownHeader = "X-Consigliere"

const docxType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

// Deliver puts a message (an apply kit and its resume) in the mailbox's inbox, from and to the mailbox itself. It
// uses Gmail's insert permission: the message is placed, never sent, so it can't reach anyone else.
func (g *Gmail) Deliver(ctx context.Context, subject, text, fileName string, file []byte) error {
	svc, err := g.connect(ctx)
	if err != nil {
		return err
	}
	profile, err := svc.Users.GetProfile("me").Context(ctx).Do()
	if err != nil {
		return explain(err)
	}
	raw, err := buildMessage(profile.EmailAddress, subject, text, fileName, file, time.Now())
	if err != nil {
		return err
	}
	_, err = svc.Users.Messages.Insert("me", &gmail.Message{Raw: base64.URLEncoding.EncodeToString(raw), LabelIds: []string{"INBOX", "UNREAD"}}).
		Context(ctx).Do()
	return explain(err)
}

// explain turns Gmail's refusal into the fix: a sign-in saved before Deliver existed can only read the mailbox.
func explain(err error) error {
	var gerr *googleapi.Error
	if errors.As(err, &gerr) && gerr.Code == http.StatusForbidden {
		return fmt.Errorf("Gmail refused (%s). Run `consigliere auth gmail` again to allow adding messages, then push the secrets to Key Vault (docs/deploy.md)", gerr.Message)
	}
	return err
}

// buildMessage writes a MIME message: the text as the body, plus the file as an attachment when there is one.
func buildMessage(address, subject, text, fileName string, file []byte, now time.Time) ([]byte, error) {
	var body bytes.Buffer
	parts := multipart.NewWriter(&body)
	textPart, err := parts.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"text/plain; charset=utf-8"},
		"Content-Transfer-Encoding": {"base64"},
	})
	if err != nil {
		return nil, err
	}
	if err := writeBase64(textPart, []byte(text)); err != nil {
		return nil, err
	}
	if file != nil {
		filePart, err := parts.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {mime.FormatMediaType(docxType, map[string]string{"name": fileName})},
			"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": fileName})},
			"Content-Transfer-Encoding": {"base64"},
		})
		if err != nil {
			return nil, err
		}
		if err := writeBase64(filePart, file); err != nil {
			return nil, err
		}
	}
	if err := parts.Close(); err != nil {
		return nil, err
	}

	var msg bytes.Buffer
	for _, h := range [][2]string{
		{"From", (&netmail.Address{Name: "Consigliere", Address: address}).String()},
		{"To", address},
		{"Subject", mime.QEncoding.Encode("utf-8", subject)},
		{"Date", now.Format(time.RFC1123Z)},
		{"MIME-Version", "1.0"},
		{ownHeader, "kit"},
		{"Content-Type", mime.FormatMediaType("multipart/mixed", map[string]string{"boundary": parts.Boundary()})},
	} {
		fmt.Fprintf(&msg, "%s: %s\r\n", h[0], h[1])
	}
	msg.WriteString("\r\n")
	msg.Write(body.Bytes())
	return msg.Bytes(), nil
}

// writeBase64 writes data base64-encoded in 76-character lines, as MIME requires.
func writeBase64(w io.Writer, data []byte) error {
	enc := base64.StdEncoding.EncodeToString(data)
	for len(enc) > 76 {
		if _, err := io.WriteString(w, enc[:76]+"\r\n"); err != nil {
			return err
		}
		enc = enc[76:]
	}
	_, err := io.WriteString(w, enc+"\r\n")
	return err
}
