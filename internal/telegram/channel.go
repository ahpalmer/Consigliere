package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ahpalmer/consigliere/internal/channel"
)

// Telegram's hard limit is 4096 characters per message.
const maxMessageLength = 4000

// Sender is the outbound half. It depends only on the API, so anything can notify the owner.
type Sender struct {
	API   *API
	Owner int64
}

func (s *Sender) Name() string { return "telegram" }

func (s *Sender) Notify(ctx context.Context, text string, buttons ...channel.Button) error {
	if s.Owner == 0 {
		slog.Warn("no owner chat configured; dropping notification")
		return nil
	}
	return s.Send(ctx, s.Owner, text, buttons)
}

// Send splits long text into chunks; buttons go on the last chunk so they sit under the whole message.
func (s *Sender) Send(ctx context.Context, chatID int64, text string, buttons []channel.Button) error {
	chunks := chunk(text, maxMessageLength)
	for i, c := range chunks {
		var keyboard [][]InlineButton
		if i == len(chunks)-1 {
			keyboard = keyboardOf(buttons)
		}
		if err := s.API.SendMessage(ctx, chatID, c, keyboard); err != nil {
			return err
		}
	}
	return nil
}

// SendFile delivers a document to the owner.
func (s *Sender) SendFile(ctx context.Context, name string, data []byte, caption string) error {
	if s.Owner == 0 {
		slog.Warn("no owner chat configured; dropping file")
		return nil
	}
	return s.API.SendDocument(ctx, s.Owner, name, data, caption)
}

// keyboardOf groups buttons into rows by Row, keeping their order within each row.
func keyboardOf(buttons []channel.Button) [][]InlineButton {
	var rows [][]InlineButton
	index := map[int]int{}
	for _, b := range buttons {
		i, ok := index[b.Row]
		if !ok {
			i = len(rows)
			index[b.Row] = i
			rows = append(rows, nil)
		}
		rows[i] = append(rows[i], InlineButton{Text: b.Label, CallbackData: b.Data})
	}
	return rows
}

func chunk(text string, size int) []string {
	if text == "" {
		return []string{"(empty)"}
	}
	var out []string
	for len(text) > 0 {
		if utf8.RuneCountInString(text) <= size {
			return append(out, text)
		}
		cut := 0
		for i := range text {
			if utf8.RuneCountInString(text[:i]) >= size {
				cut = i
				break
			}
		}
		out = append(out, text[:cut])
		text = text[cut:]
	}
	return out
}

// Router handles an allowlisted chat message and returns the reply.
type Router interface {
	Handle(ctx context.Context, chatID int64, text string) (string, error)
}

// Receiver is the inbound half: it long-polls and routes allowlisted messages to the router and button taps to
// callback handlers. Everyone not on the allowlist is ignored (and logged, so the owner can find their own ID).
type Receiver struct {
	API       *API
	Sender    *Sender
	Router    Router
	Callbacks []channel.CallbackHandler
	Allowed   []int64
}

func (r *Receiver) Run(ctx context.Context) error {
	me, err := r.API.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("telegram: %w", err)
	}
	slog.Info("telegram connected", "bot", "@"+me.Username, "allowed_chats", len(r.Allowed))

	// Each update is handled on its own goroutine, so one slow command doesn't freeze the chat.
	// Run waits for in-flight handlers before returning, so shutdown doesn't close resources under them.
	var inflight sync.WaitGroup
	defer inflight.Wait()

	var offset int64
	for ctx.Err() == nil {
		updates, err := r.API.GetUpdates(ctx, offset, 50)
		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}
			slog.Warn("telegram polling failed; retrying", "err", err)
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
			continue
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			inflight.Add(1)
			go func() {
				defer inflight.Done()
				switch {
				case u.Message != nil:
					r.handleMessage(ctx, *u.Message)
				case u.CallbackQuery != nil:
					r.handleCallback(ctx, *u.CallbackQuery)
				}
			}()
		}
	}
	return nil
}

func (r *Receiver) allowed(chatID int64, username string) bool {
	if slices.Contains(r.Allowed, chatID) {
		return true
	}
	slog.Warn("ignoring update from non-allowlisted chat", "chat_id", chatID, "username", username)
	return false
}

func (r *Receiver) handleMessage(ctx context.Context, m Message) {
	if m.Text == "" {
		return
	}
	username := ""
	if m.From != nil {
		username = m.From.Username
	}
	if !r.allowed(m.Chat.ID, username) {
		return
	}
	_ = r.API.SendChatAction(ctx, m.Chat.ID, "typing")
	reply, err := r.Router.Handle(ctx, m.Chat.ID, m.Text)
	if ctx.Err() != nil {
		return // shutting down; the reply can't be sent anyway
	}
	if err != nil {
		slog.Error("failed handling message", "chat_id", m.Chat.ID, "err", err)
		reply = "Something broke: " + err.Error()
	}
	if reply == "" {
		return // the handler already sent its own messages
	}
	if err := r.Sender.Send(ctx, m.Chat.ID, reply, nil); err != nil {
		slog.Error("failed sending reply", "err", err)
	}
}

func (r *Receiver) handleCallback(ctx context.Context, cb CallbackQuery) {
	// Taps arrive from the chat the button was sent to; check the tapping user too.
	if !r.allowed(cb.From.ID, cb.From.Username) {
		return
	}
	toast := "That button no longer does anything."
	h := channel.FindCallbackHandler(r.Callbacks, cb.Data)
	if h != nil {
		var err error
		if toast, err = h.HandleCallback(ctx, cb.Data); err != nil {
			slog.Error("failed handling callback", "data", cb.Data, "err", err)
			toast = "Something broke."
			h = nil
		}
	}
	if err := r.API.AnswerCallbackQuery(ctx, cb.ID, toast); err != nil {
		slog.Warn("answering callback failed", "err", err)
	}
	if refresher, ok := h.(channel.MessageRefresher); ok && cb.Message != nil {
		text, buttons, ok, err := refresher.Refresh(ctx, cb.Data)
		if err != nil {
			slog.Warn("refreshing message failed", "data", cb.Data, "err", err)
			return
		}
		if ok {
			if err := r.API.EditMessageText(ctx, cb.Message.Chat.ID, cb.Message.MessageID, text, keyboardOf(buttons)); err != nil {
				slog.Warn("editing message failed", "err", err)
			}
		}
	}
}
