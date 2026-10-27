package mail

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"

	"github.com/ahpalmer/consigliere/internal/config"
)

type Message struct {
	ID, From, Subject string
	ReceivedAt        *time.Time
	HTML, Text        string
}

type Mailbox interface {
	Name() string
	ListMessageIDs(ctx context.Context, query string, max int) ([]string, error)
	Get(ctx context.Context, id string) (Message, error)
}

// Scopes is read-only Gmail access, to the dedicated forwarding mailbox only.
var Scopes = []string{gmail.GmailReadonlyScope}

func OAuthConfig(cfg config.Mail) *oauth2.Config {
	return &oauth2.Config{ClientID: cfg.ClientId, ClientSecret: cfg.ClientSecret, Endpoint: google.Endpoint, Scopes: Scopes}
}

// Gmail connects lazily, so a missing configuration only fails when the email source is actually scanned.
type Gmail struct {
	cfg config.Mail

	once    sync.Once
	service *gmail.Service
	err     error
}

func NewGmail(cfg config.Mail) *Gmail { return &Gmail{cfg: cfg} }

func (g *Gmail) Name() string { return "gmail" }

func (g *Gmail) connect(ctx context.Context) (*gmail.Service, error) {
	g.once.Do(func() {
		if missing := g.cfg.MissingSettings(); len(missing) > 0 {
			hint := "run `consigliere auth gmail`"
			if g.cfg.ClientId == "" || g.cfg.ClientSecret == "" {
				hint = "set the client id/secret with `consigliere secrets set`, then run `consigliere auth gmail`"
			}
			g.err = fmt.Errorf("mail is not configured (missing %s): %s", strings.Join(missing, ", "), hint)
			return
		}
		ts := OAuthConfig(g.cfg).TokenSource(context.Background(), &oauth2.Token{RefreshToken: g.cfg.RefreshToken})
		g.service, g.err = gmail.NewService(ctx, option.WithTokenSource(ts))
	})
	return g.service, g.err
}

func (g *Gmail) ListMessageIDs(ctx context.Context, query string, max int) ([]string, error) {
	svc, err := g.connect(ctx)
	if err != nil {
		return nil, err
	}
	var ids []string
	page := ""
	for len(ids) < max {
		call := svc.Users.Messages.List("me").Q(query).MaxResults(int64(min(100, max-len(ids)))).Context(ctx)
		if page != "" {
			call = call.PageToken(page)
		}
		resp, err := call.Do()
		if err != nil {
			return nil, err
		}
		for _, m := range resp.Messages {
			ids = append(ids, m.Id)
		}
		if resp.NextPageToken == "" {
			break
		}
		page = resp.NextPageToken
	}
	return ids, nil
}

func (g *Gmail) Get(ctx context.Context, id string) (Message, error) {
	svc, err := g.connect(ctx)
	if err != nil {
		return Message{}, err
	}
	m, err := svc.Users.Messages.Get("me", id).Format("full").Context(ctx).Do()
	if err != nil {
		return Message{}, err
	}
	header := func(name string) string {
		if m.Payload != nil {
			for _, h := range m.Payload.Headers {
				if strings.EqualFold(h.Name, name) {
					return h.Value
				}
			}
		}
		return ""
	}
	msg := Message{ID: id, From: header("From"), Subject: header("Subject"), HTML: findBody(m.Payload, "text/html"), Text: findBody(m.Payload, "text/plain")}
	if m.InternalDate > 0 {
		t := time.UnixMilli(m.InternalDate).UTC()
		msg.ReceivedAt = &t
	}
	return msg, nil
}

func findBody(part *gmail.MessagePart, mimeType string) string {
	if part == nil {
		return ""
	}
	if part.MimeType == mimeType && part.Body != nil && part.Body.Data != "" {
		raw, err := base64.URLEncoding.DecodeString(part.Body.Data)
		if err != nil {
			raw, err = base64.RawURLEncoding.DecodeString(part.Body.Data)
		}
		if err == nil {
			return string(raw)
		}
	}
	for _, p := range part.Parts {
		if body := findBody(p, mimeType); body != "" {
			return body
		}
	}
	return ""
}
