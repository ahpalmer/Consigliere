// Package mcpserver is Consigliere over MCP (M15): tools, resources and prompts for one bot, served by the separate
// consigliere-mcp program. Everything goes through the bot's dashboard API with its token; this package never opens
// the database or the data pack, so it can do nothing the dashboard can't.
package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// API calls one bot's dashboard API.
type API struct {
	// The dashboard's address, e.g. http://127.0.0.1:8080 or the bot's tailnet name.
	Base string
	// Web:Token; empty only for a dashboard on loopback with no sign-in.
	Token string
	HTTP  *http.Client
}

// APIError is the dashboard refusing a request, with its own message.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	switch e.Status {
	case http.StatusUnauthorized:
		return "the dashboard refused the token; check CONSIGLIERE_TOKEN"
	case http.StatusNotFound:
		return "not found"
	}
	return e.Message
}

// Get fetches a path with optional query values.
func (a *API) Get(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return a.Do(ctx, http.MethodGet, path, nil)
}

// Do sends a request (body as JSON when not nil) and returns the reply's JSON.
func (a *API) Do(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(a.Base, "/")+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if method != http.MethodGet {
		// The dashboard takes writes only as JSON (its CSRF guard), even with no body.
		req.Header.Set("Content-Type", "application/json")
	}
	if a.Token != "" {
		req.Header.Set("Authorization", "Bearer "+a.Token)
	}
	client := a.HTTP
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Minute} // re-scoring and reading offers wait on a model
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach the dashboard at %s: %w", a.Base, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		if json.Unmarshal(raw, &e) != nil || e.Error == "" {
			e.Error = resp.Status
		}
		return nil, &APIError{Status: resp.StatusCode, Message: e.Error}
	}
	if !json.Valid(raw) {
		return nil, errors.New("the dashboard didn't answer with JSON; is the URL right?")
	}
	return raw, nil
}
