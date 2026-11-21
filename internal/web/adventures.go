package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ahpalmer/consigliere/internal/adventures"
	"github.com/ahpalmer/consigliere/internal/llm"
)

// The Adventures section (M13): the idea bank, planned and past adventures, and generated ideas. Off (404) unless the
// data pack has adventures/settings.yaml.

func (s *Server) adventureRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/adventures", s.adventuresOn(s.listAdventures))
	mux.HandleFunc("POST /api/adventures", s.adventuresOn(s.planAdventure))
	mux.HandleFunc("PUT /api/adventures/{id}", s.adventuresOn(s.updateAdventure))
	mux.HandleFunc("POST /api/adventures/{id}/rate", s.adventuresOn(s.rateAdventure))
	mux.HandleFunc("POST /api/adventures/{id}/skip", s.adventuresOn(s.skipAdventure))
	mux.HandleFunc("DELETE /api/adventures/{id}", s.adventuresOn(s.deleteAdventure))
	mux.HandleFunc("GET /api/adventures/ideas", s.adventuresOn(s.listIdeas))
	mux.HandleFunc("POST /api/adventures/ideas", s.adventuresOn(s.addIdea))
	mux.HandleFunc("PUT /api/adventures/ideas/{id}", s.adventuresOn(s.updateIdea))
	mux.HandleFunc("POST /api/adventures/ideas/{id}/favorite", s.adventuresOn(s.favoriteIdea))
	mux.HandleFunc("DELETE /api/adventures/ideas/{id}", s.adventuresOn(s.archiveIdea))
	mux.HandleFunc("POST /api/adventures/generate", s.adventuresOn(s.generateIdeas))
}

func (s *Server) adventuresEnabled() bool {
	if s.Adventures == nil {
		return false
	}
	_, ok, err := adventures.LoadSettings(s.Pack)
	return ok && err == nil
}

func (s *Server) adventuresOn(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.adventuresEnabled() {
			writeError(w, http.StatusNotFound, "Adventures aren't set up (add "+adventures.SettingsPath+" to the data pack).")
			return
		}
		next(w, r)
	}
}

// adventureError: not found is 404, the owner's own mistakes are 400.
func adventureError(w http.ResponseWriter, err error) {
	var bad *adventures.InvalidError
	switch {
	case errors.Is(err, adventures.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.As(err, &bad):
		writeError(w, http.StatusBadRequest, bad.Error())
	default:
		serverError(w, err)
	}
}

func (s *Server) listAdventures(w http.ResponseWriter, r *http.Request) {
	upcoming, err := s.Adventures.Upcoming(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	history, err := s.Adventures.History(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"today": s.Adventures.Today(), "upcoming": upcoming, "history": history})
}

func (s *Server) planAdventure(w http.ResponseWriter, r *http.Request) {
	var body adventures.Adventure
	if !readJSON(w, r, &body) {
		return
	}
	a, err := s.Adventures.Plan(r.Context(), body)
	if err != nil {
		adventureError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) updateAdventure(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body adventures.Adventure
	if !readJSON(w, r, &body) {
		return
	}
	a, err := s.Adventures.Update(r.Context(), id, body)
	if err != nil {
		adventureError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) rateAdventure(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Rating int    `json:"rating"`
		Review string `json:"review"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	a, err := s.Adventures.Rate(r.Context(), id, body.Rating, body.Review)
	if err != nil {
		adventureError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) skipAdventure(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	a, err := s.Adventures.Skip(r.Context(), id)
	if err != nil {
		adventureError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) deleteAdventure(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.Adventures.Delete(r.Context(), id); err != nil {
		adventureError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) listIdeas(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ideas, err := s.Adventures.Ideas(r.Context(), q.Get("q"), q.Get("favorites") == "1")
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ideas": ideas})
}

func (s *Server) addIdea(w http.ResponseWriter, r *http.Request) {
	var body adventures.Idea
	if !readJSON(w, r, &body) {
		return
	}
	i, err := s.Adventures.AddIdea(r.Context(), body)
	if err != nil {
		adventureError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, i)
}

func (s *Server) updateIdea(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body adventures.Idea
	if !readJSON(w, r, &body) {
		return
	}
	i, err := s.Adventures.UpdateIdea(r.Context(), id, body)
	if err != nil {
		adventureError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, i)
}

func (s *Server) favoriteIdea(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Favorite bool `json:"favorite"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	i, err := s.Adventures.SetFavorite(r.Context(), id, body.Favorite)
	if err != nil {
		adventureError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, i)
}

func (s *Server) archiveIdea(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.Adventures.ArchiveIdea(r.Context(), id); err != nil {
		adventureError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"archived": true})
}

// generateIdeas asks a model for 5 ideas fitting the constraints; they come back unsaved.
func (s *Server) generateIdeas(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Constraints string `json:"constraints"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if s.State.Paused() {
		writeError(w, http.StatusConflict, "Paused. Resume first.")
		return
	}
	settings, _, err := adventures.LoadSettings(s.Pack)
	if err != nil {
		serverError(w, err)
		return
	}
	ideas, err := s.IdeaGenerator.Generate(r.Context(), settings, strings.TrimSpace(body.Constraints))
	switch {
	case errors.Is(err, llm.ErrRefused):
		writeError(w, http.StatusUnprocessableEntity, "The model declined; try different constraints.")
	case err != nil:
		slog.Warn("couldn't generate adventure ideas", "err", err)
		writeError(w, http.StatusBadGateway, "Couldn't come up with ideas just now; try again.")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ideas": ideas})
	}
}
