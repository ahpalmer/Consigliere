package web

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ahpalmer/consigliere/internal/devbox"
)

// The Code page: Claude Code sessions on the dev box (docs/dev-box.md). Only for a bot whose data pack sets up a
// dev box; everyone else gets 404s and no Code link.

func (s *Server) codeEnabled() bool { return s.Code != nil && s.Code.Enabled() }

func (s *Server) codeStatus(w http.ResponseWriter, r *http.Request) {
	if !s.codeEnabled() {
		writeError(w, http.StatusNotFound, "no dev box set up")
		return
	}
	st, err := s.Code.Status(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) startCodeSession(w http.ResponseWriter, r *http.Request) {
	if !s.codeEnabled() {
		writeError(w, http.StatusNotFound, "no dev box set up")
		return
	}
	var body struct {
		Mode string `json:"mode"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.Mode != devbox.Continue && body.Mode != devbox.New {
		writeError(w, http.StatusBadRequest, `mode must be "continue" or "new"`)
		return
	}
	started, err := s.Code.Start(r.Context(), r.PathValue("repo"), body.Mode)
	var refused *devbox.AgentError
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, started)
	case errors.Is(err, devbox.ErrUnknownRepo):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.As(err, &refused) && refused.Status < 500:
		writeError(w, http.StatusConflict, refused.Message)
	default:
		// The dev box or Azure failed; their messages hold no secrets and say what to fix.
		slog.Warn("couldn't open a dev box session", "repo", r.PathValue("repo"), "err", err)
		writeError(w, http.StatusBadGateway, err.Error())
	}
}
