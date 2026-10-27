// Package channel defines how the agent reaches its owner, independent of the chat app.
package channel

import (
	"context"
	"strings"
)

// Button is a tappable action under a message. Data is routed to the CallbackHandler whose prefix it starts with.
type Button struct{ Label, Data string }

// Channel is an outbound path to the owner (Telegram today; WhatsApp, email, etc. later).
type Channel interface {
	Name() string
	// Notify sends a message to the owner, with an optional row of buttons.
	Notify(ctx context.Context, text string, buttons ...Button) error
}

// CallbackHandler handles button taps whose data starts with Prefix.
type CallbackHandler interface {
	Prefix() string
	// HandleCallback returns a short confirmation shown to the owner (a toast in Telegram).
	HandleCallback(ctx context.Context, data string) (string, error)
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
