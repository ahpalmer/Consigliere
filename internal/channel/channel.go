// Package channel defines how the agent reaches its owner, independent of the chat app.
package channel

import (
	"context"
	"strings"
)

// Button is a tappable action under a message. Data is routed to the CallbackHandler whose prefix it starts with.
// Buttons with the same Row share a line, in order.
type Button struct {
	Label, Data string
	Row         int
}

// Channel is an outbound path to the owner (Telegram today; WhatsApp, email, etc. later).
type Channel interface {
	Name() string
	// Notify sends a message to the owner, with optional buttons.
	Notify(ctx context.Context, text string, buttons ...Button) error
}

// FileSender is implemented by channels that can deliver documents.
type FileSender interface {
	SendFile(ctx context.Context, name string, data []byte, caption string) error
}

// CallbackHandler handles button taps whose data starts with Prefix.
type CallbackHandler interface {
	Prefix() string
	// HandleCallback returns a short confirmation shown to the owner (a toast in Telegram).
	HandleCallback(ctx context.Context, data string) (string, error)
}

// MessageRefresher is optionally implemented by callback handlers whose buttons change the message they sit on
// (e.g. ticking a checklist). After HandleCallback succeeds, the message is replaced with the returned text and
// buttons; ok=false leaves it as is.
type MessageRefresher interface {
	Refresh(ctx context.Context, data string) (text string, buttons []Button, ok bool, err error)
}

// FindCallbackHandler returns the handler for a button's data, or nil.
func FindCallbackHandler(handlers []CallbackHandler, data string) CallbackHandler {
	for _, h := range handlers {
		if strings.HasPrefix(data, h.Prefix()) {
			return h
		}
	}
	return nil
}
