// Package web serves the dashboard: a JSON API over the pipeline and radar, plus the React app built into dist/.
// It's meant to be reached privately (loopback, or a tailnet); signing in takes the Web:Token secret.
package web

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/ahpalmer/consigliere/internal/agent"
	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/llm"
	"github.com/ahpalmer/consigliere/internal/pipeline"
	"github.com/ahpalmer/consigliere/internal/radar"
)

//go:embed all:dist
var dist embed.FS

type Server struct {
	Addr     string
	Token    string // empty: no sign-in, allowed on loopback only
	Pipeline *pipeline.Pipeline
	Drafts   *pipeline.Drafts
	Catalog  *radar.Catalog
	Feedback *radar.Feedback
	Scoring  *radar.ScoringRunner
	State    *agent.State
	Usage    *llm.UsageTracker
	Clock    clock.Clock
	// Overrides the embedded app (tests).
	Assets fs.FS
}

// CheckAddr refuses to serve without a token anywhere but loopback.
func CheckAddr(addr, token string) error {
	if token == "" && !isLoopback(addr) {
		return errors.New("the dashboard listens beyond loopback (Web:Listen), so it needs a Web:Token secret")
	}
	return nil
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Run serves until ctx ends.
func (s *Server) Run(ctx context.Context) error {
	if err := CheckAddr(s.Addr, s.Token); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 60 * time.Second, IdleTimeout: 2 * time.Minute}
	slog.Info("dashboard listening", "url", "http://"+listener.Addr().String(), "sign_in", s.Token != "")

	errs := make(chan error, 1)
	go func() { errs <- srv.Serve(listener) }()
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
		return nil
	}
}

// Handler is the whole site: API routes behind sign-in, the app everywhere else.
func (s *Server) Handler() http.Handler {
	api := http.NewServeMux()
	s.routes(api)

	mux := http.NewServeMux()
	mux.Handle("/api/", s.guard(api))
	mux.Handle("/", s.app())
	return securityHeaders(mux)
}

// guard applies the API's checks: a known Host when there's no token (DNS rebinding), JSON bodies on writes
// (a cross-site form can't send them, so no CSRF), and sign-in for everything but the session endpoints.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Token == "" && !isLoopback(r.Host) {
			writeError(w, http.StatusForbidden, "unexpected host")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				writeError(w, http.StatusUnsupportedMediaType, "send JSON")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		}
		if r.URL.Path != "/api/session" && r.URL.Path != "/api/login" && !s.signedIn(r) {
			writeError(w, http.StatusUnauthorized, "sign in")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

// app serves the built React app, falling back to index.html so client-side routes survive a reload.
func (s *Server) app() http.Handler {
	assets := s.Assets
	if assets == nil {
		sub, err := fs.Sub(dist, "dist")
		if err != nil {
			panic(err) // the embed pattern guarantees dist exists
		}
		assets = sub
	}
	files := http.FileServerFS(assets)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if info, err := fs.Stat(assets, name); err == nil && !info.IsDir() && name != "index.html" {
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // Vite hashes these names
			}
			files.ServeHTTP(w, r)
			return
		}
		index, err := fs.ReadFile(assets, "index.html")
		if err != nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("The dashboard isn't built into this binary. Run `npm ci && npm run build` in web/, then rebuild.\n"))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
}
