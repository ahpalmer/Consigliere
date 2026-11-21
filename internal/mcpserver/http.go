package mcpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// HTTP serves MCP over streamable HTTP (phase 4), next to a bot: on its VM, on loopback, published to the tailnet by
// `tailscale serve`. It holds no secret. Each caller sends the bot's dashboard token as its own bearer token; it's
// checked with the dashboard on every request, and the tools call the dashboard with it.
type HTTP struct {
	// The dashboard, e.g. http://127.0.0.1:8080.
	Base    string
	Version string
	// As in Server; zero for the defaults.
	PollEvery, MaxWait time.Duration
	Client             *http.Client
}

func (h *HTTP) Handler() http.Handler {
	// Stateless: every request gets a server built for its caller's token, so nothing is kept between requests.
	// Localhost protection is off because `tailscale serve` hands requests over on loopback with the tailnet name as
	// Host; the bearer token, which a rebinding web page can't send, is what keeps others out.
	mcpHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		token, _ := bearer(r)
		s := &Server{API: &API{Base: h.Base, Token: token, HTTP: h.Client}, PollEvery: h.PollEvery, MaxWait: h.MaxWait}
		return s.MCP(h.Version)
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: false, DisableLocalhostProtection: true, PropagateRequestCancellation: true})
	protected := http.NewCrossOriginProtection().Handler(mcpHandler)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearer(r)
		if !ok || !h.signedIn(r, token) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="consigliere"`)
			http.Error(w, "send the bot's dashboard token as a bearer token", http.StatusUnauthorized)
			return
		}
		protected.ServeHTTP(w, r)
	})
}

// Serve serves /mcp (and /healthz) on addr until ctx ends.
func (h *HTTP) Serve(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.Handle("/mcp", h.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	errs := make(chan error, 1)
	go func() { errs <- srv.ListenAndServe() }()
	slog.Info("serving MCP over HTTP", "addr", addr, "path", "/mcp", "dashboard", h.Base)
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdown)
	}
}

func bearer(r *http.Request) (string, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	token = strings.TrimSpace(token)
	return token, ok && token != ""
}

// signedIn asks the dashboard whether the token is its own.
func (h *HTTP) signedIn(r *http.Request, token string) bool {
	api := &API{Base: h.Base, Token: token, HTTP: h.Client}
	raw, err := api.Get(r.Context(), "/api/session", nil)
	if err != nil {
		return false
	}
	var s struct {
		SignedIn bool `json:"signedIn"`
	}
	return json.Unmarshal(raw, &s) == nil && s.SignedIn
}
