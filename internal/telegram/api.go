// Package telegram is the Telegram channel: a small Bot API client, an outbound sender, and a long-polling receiver.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// API is the subset of the Bot API the agent uses. Long-polling only: the host needs no inbound ports.
type API struct {
	token string
	base  string
	http  *http.Client
}

func NewAPI(token string) *API {
	// getUpdates holds the connection open for up to 50s, so the client timeout must be longer.
	return &API{token: token, base: "https://api.telegram.org", http: &http.Client{Timeout: 70 * time.Second}}
}

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type Chat struct {
	ID int64 `json:"id"`
}

type Message struct {
	MessageID int64  `json:"message_id"`
	From      *User  `json:"from"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

type InlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

func (a *API) call(ctx context.Context, method string, params, result any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/bot%s/%s", a.base, a.token, method), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.http.Do(req)
	if err != nil {
		// The URL contains the bot token; never let it into logs.
		return fmt.Errorf("telegram %s: request failed", method)
	}
	defer resp.Body.Close()
	var envelope struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("telegram %s: %s", method, resp.Status)
	}
	if !envelope.OK {
		return fmt.Errorf("telegram %s: %s", method, envelope.Description)
	}
	if result != nil {
		return json.Unmarshal(envelope.Result, result)
	}
	return nil
}

func (a *API) GetMe(ctx context.Context) (User, error) {
	var u User
	return u, a.call(ctx, "getMe", map[string]any{}, &u)
}

func (a *API) GetUpdates(ctx context.Context, offset int64, timeoutSeconds int) ([]Update, error) {
	var updates []Update
	return updates, a.call(ctx, "getUpdates", map[string]any{
		"offset": offset, "timeout": timeoutSeconds, "allowed_updates": []string{"message", "callback_query"},
	}, &updates)
}

func (a *API) SendMessage(ctx context.Context, chatID int64, text string, buttons []InlineButton) error {
	params := map[string]any{"chat_id": chatID, "text": text, "link_preview_options": map[string]any{"is_disabled": true}}
	if len(buttons) > 0 {
		params["reply_markup"] = map[string]any{"inline_keyboard": [][]InlineButton{buttons}}
	}
	return a.call(ctx, "sendMessage", params, nil)
}

func (a *API) SendChatAction(ctx context.Context, chatID int64, action string) error {
	return a.call(ctx, "sendChatAction", map[string]any{"chat_id": chatID, "action": action}, nil)
}

func (a *API) AnswerCallbackQuery(ctx context.Context, id, text string) error {
	return a.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": text}, nil)
}
