package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
	"time"
)

const sessionCookie = "consigliere_session"

// session is the cookie value: derived from the token, so it survives restarts and rotating the token signs
// everyone out.
func (s *Server) session() string {
	mac := hmac.New(sha256.New, []byte(s.Token))
	mac.Write([]byte("dashboard-session-v1"))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Server) signedIn(r *http.Request) bool {
	if s.Token == "" {
		return true
	}
	if bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return equal(bearer, s.Token)
	}
	c, err := r.Cookie(sessionCookie)
	return err == nil && equal(c.Value, s.session())
}

func equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"signInRequired": s.Token != "", "signedIn": s.signedIn(r)})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if s.Token == "" {
		writeJSON(w, http.StatusOK, map[string]bool{"signedIn": true})
		return
	}
	if !equal(body.Token, s.Token) {
		time.Sleep(time.Second) // slows guessing
		writeError(w, http.StatusUnauthorized, "wrong token")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: s.session(), Path: "/", MaxAge: 30 * 24 * 3600,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: secure(r)})
	writeJSON(w, http.StatusOK, map[string]bool{"signedIn": true})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: secure(r)})
	writeJSON(w, http.StatusOK, map[string]bool{"signedIn": false})
}

// secure: the request came over HTTPS, directly or through a proxy such as `tailscale serve`.
func secure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
