package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ahpalmer/consigliere/internal/comp"
	"github.com/ahpalmer/consigliere/internal/llm"
)

// Offers on a pursuit (M9): paste an offer, read it into a package, and compare it with the current one from the
// data pack. Comparisons are worked out on every read, so editing comp/current.yaml shows up at once.

func (s *Server) offerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/pursuits/{id}/offers", s.listOffers)
	mux.HandleFunc("POST /api/pursuits/{id}/offers", s.addOffer)
	mux.HandleFunc("PUT /api/offers/{id}", s.updateOffer)
	mux.HandleFunc("DELETE /api/offers/{id}", s.deleteOffer)
}

type offerJSON struct {
	ID        int64     `json:"id"`
	PursuitID int64     `json:"pursuitId"`
	Text      string    `json:"text"`
	Package   string    `json:"package"`
	Company   string    `json:"company"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Null when there's no current package to compare with, or the offer's YAML doesn't parse (Error says why).
	Comparison *comp.Comparison `json:"comparison"`
	Error      string           `json:"error"`
}

// currentPackage is the data pack's comp/current.yaml, with what to tell the owner when it's missing or broken.
func (s *Server) currentPackage() (*comp.Package, string) {
	if s.Pack == nil {
		return nil, "No data pack."
	}
	p, ok, err := comp.LoadCurrent(s.Pack)
	switch {
	case err != nil:
		return nil, err.Error()
	case !ok:
		return nil, "Add your current package to the data pack (" + comp.CurrentPath + ") to compare offers with it."
	}
	return p, ""
}

// payNow is what the owner makes now, to hold job postings against; nil without a usable current package.
func (s *Server) payNow() *comp.Yardstick {
	current, _ := s.currentPackage()
	if current == nil {
		return nil
	}
	y := comp.NowFrom(current, s.Clock.Now().In(s.Pipeline.Location))
	return &y
}

func (s *Server) offerJSON(o *comp.Offer, current *comp.Package, currentError string) offerJSON {
	out := offerJSON{ID: o.ID, PursuitID: o.PursuitID, Text: o.Text, Package: o.Package, CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt}
	incoming, err := comp.Parse(o.Package)
	switch {
	case err != nil:
		out.Error = "The offer's package doesn't check out: " + err.Error()
	case current == nil:
		out.Company, out.Error = incoming.Company, currentError
	default:
		out.Company = incoming.Company
		c := comp.Compare(current, incoming, s.Clock.Now().In(s.Pipeline.Location))
		out.Comparison = &c
	}
	return out
}

func (s *Server) listOffers(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := s.Pipeline.Get(r.Context(), id); err != nil {
		actionError(w, err)
		return
	}
	list, err := s.Offers.List(r.Context(), id)
	if err != nil {
		serverError(w, err)
		return
	}
	current, currentError := s.currentPackage()
	out := []offerJSON{}
	for _, o := range list {
		out = append(out, s.offerJSON(o, current, currentError))
	}
	writeJSON(w, http.StatusOK, map[string]any{"offers": out, "current": current != nil, "currentError": currentError})
}

func (s *Server) addOffer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct{ Text string }
	if !readJSON(w, r, &body) {
		return
	}
	body.Text = strings.TrimSpace(body.Text)
	if body.Text == "" {
		writeError(w, http.StatusBadRequest, "paste the offer")
		return
	}
	if s.State.Paused() {
		writeError(w, http.StatusConflict, "Paused. Resume first.")
		return
	}
	ctx := r.Context()
	if _, err := s.Pipeline.Get(ctx, id); err != nil {
		actionError(w, err)
		return
	}
	pkg, err := s.OfferReader.Extract(ctx, body.Text, s.Clock.Now().In(s.Pipeline.Location))
	switch {
	case errors.Is(err, comp.ErrNotAnOffer):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	case errors.Is(err, llm.ErrRefused):
		writeError(w, http.StatusUnprocessableEntity, "The model declined to read that text.")
		return
	case err != nil:
		slog.Warn("couldn't read an offer", "pursuit", id, "err", err)
		writeError(w, http.StatusBadGateway, "Couldn't read the offer; try again.")
		return
	}
	o, err := s.Offers.Add(ctx, id, body.Text, pkg)
	if err != nil {
		serverError(w, err)
		return
	}
	current, currentError := s.currentPackage()
	writeJSON(w, http.StatusCreated, s.offerJSON(o, current, currentError))
}

func (s *Server) updateOffer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct{ Package string }
	if !readJSON(w, r, &body) {
		return
	}
	o, err := s.Offers.Update(r.Context(), id, body.Package)
	var invalid *comp.InvalidError
	switch {
	case errors.Is(err, comp.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
		return
	case errors.As(err, &invalid):
		writeError(w, http.StatusBadRequest, invalid.Error())
		return
	case err != nil:
		serverError(w, err)
		return
	}
	current, currentError := s.currentPackage()
	writeJSON(w, http.StatusOK, s.offerJSON(o, current, currentError))
}

func (s *Server) deleteOffer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	switch err := s.Offers.Delete(r.Context(), id); {
	case errors.Is(err, comp.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case err != nil:
		serverError(w, err)
	default:
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
	}
}
