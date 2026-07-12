// v1_auth.go — the console's login authority. First-run password setup,
// password login → HttpOnly session cookie, logout, and change-password. The
// password is bcrypt-hashed; sessions are opaque random tokens stored as their
// sha256. All resolve to the single shared tenant (store.DefaultTenant).
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/auth"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/console"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

const sessionTTL = 30 * 24 * time.Hour

// minPasswordLen is the floor for a console password (defence-in-depth; the
// primary gate is that the API is unreachable without a credential at all).
const minPasswordLen = 8

// passwordSet reports whether a console password has been configured.
func (s *Server) passwordSet(r *http.Request) bool {
	if s.Store == nil {
		return false
	}
	_, err := s.Store.GetPasswordHash(r.Context())
	return err == nil
}

// GET /v1/auth/status — pre-login probe (exempt from the middleware).
func (s *Server) v1AuthStatus(w http.ResponseWriter, r *http.Request) {
	enabled := true
	if s.Auth != nil {
		enabled = !s.Auth.Snapshot().Disabled
	}
	actor := auth.ActorFrom(r.Context())
	authed := actor.Kind == "user" || actor.Kind == "service"
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":       enabled,
		"authenticated": authed,
		"password_set":  s.passwordSet(r),
	})
}

// POST /v1/auth/setup {password} — first-run create-password. 409 once set.
func (s *Server) v1AuthSetup(w http.ResponseWriter, r *http.Request) {
	if s.Store == nil {
		writeV1Err(w, http.StatusServiceUnavailable, "unavailable", "auth store not configured")
		return
	}
	if s.passwordSet(r) {
		writeV1Err(w, http.StatusConflict, "conflict", "a password is already set; use login or change-password")
		return
	}
	pw, ok := decodePassword(w, r, "password")
	if !ok {
		return
	}
	hash, err := console.HashPassword(pw)
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "could not hash password")
		return
	}
	if err := s.Store.SetPasswordHash(r.Context(), hash); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "could not save password")
		return
	}
	if !s.issueSession(w, r) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /v1/auth/login {password} — verify + set a session cookie.
func (s *Server) v1AuthLogin(w http.ResponseWriter, r *http.Request) {
	if s.Store == nil {
		writeV1Err(w, http.StatusServiceUnavailable, "unavailable", "auth store not configured")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	hash, err := s.Store.GetPasswordHash(r.Context())
	if err != nil || !console.CheckPassword(hash, body.Password) {
		writeV1Err(w, http.StatusUnauthorized, "unauthorized", "invalid password")
		return
	}
	if !s.issueSession(w, r) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /v1/auth/logout — revoke the current session + clear the cookie.
// With ?all=true it revokes EVERY session (sign out everywhere).
func (s *Server) v1AuthLogout(w http.ResponseWriter, r *http.Request) {
	if s.Store != nil {
		if r.URL.Query().Get("all") == "true" {
			_ = s.Store.DeleteAllSessions(r.Context())
		} else if ck, err := r.Cookie(auth.SessionCookie); err == nil && ck.Value != "" {
			_ = s.Store.DeleteSession(r.Context(), console.HashToken(ck.Value))
		}
	}
	http.SetCookie(w, s.clearSessionCookie(r))
	w.WriteHeader(http.StatusNoContent)
}

// POST /v1/auth/password {current_password, new_password} — change password
// (requires a logged-in user). Revokes all other sessions, then re-issues one.
func (s *Server) v1AuthPassword(w http.ResponseWriter, r *http.Request) {
	if s.Store == nil {
		writeV1Err(w, http.StatusServiceUnavailable, "unavailable", "auth store not configured")
		return
	}
	if auth.ActorFrom(r.Context()).Kind != "user" {
		writeV1Err(w, http.StatusForbidden, "forbidden", "change password requires a console login")
		return
	}
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	hash, err := s.Store.GetPasswordHash(r.Context())
	if err != nil || !console.CheckPassword(hash, body.CurrentPassword) {
		writeV1Err(w, http.StatusUnauthorized, "unauthorized", "current password is incorrect")
		return
	}
	if len(body.NewPassword) < minPasswordLen {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "new password must be at least 8 characters")
		return
	}
	newHash, err := console.HashPassword(body.NewPassword)
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "could not hash password")
		return
	}
	if err := s.Store.SetPasswordHash(r.Context(), newHash); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "could not save password")
		return
	}
	_ = s.Store.DeleteAllSessions(r.Context()) // invalidate everyone, including us
	if !s.issueSession(w, r) {                 // re-issue for this browser
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── helpers ──────────────────────────────────────────────────────────

// decodePassword decodes {field: "..."} and validates length. Writes the error
// response and returns ok=false on failure.
func decodePassword(w http.ResponseWriter, r *http.Request, field string) (string, bool) {
	var raw map[string]string
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024))
	if err := dec.Decode(&raw); err != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return "", false
	}
	pw := raw[field]
	if len(pw) < minPasswordLen {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "password must be at least 8 characters")
		return "", false
	}
	return pw, true
}

// issueSession mints a session, stores it, and sets the cookie. Writes an error
// response and returns false on failure.
func (s *Server) issueSession(w http.ResponseWriter, r *http.Request) bool {
	value, hash, err := console.NewSessionValue()
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "could not create session")
		return false
	}
	now := time.Now()
	exp := now.Add(sessionTTL)
	if err := s.Store.CreateSession(r.Context(), hash, store.DefaultTenant, now.Unix(), now.Unix(), exp.Unix()); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "could not persist session")
		return false
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookie,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isHTTPS(r), // only over HTTPS — never break plain-HTTP test deployments
		Expires:  exp,
		MaxAge:   int(sessionTTL / time.Second),
	})
	return true
}

func (s *Server) clearSessionCookie(r *http.Request) *http.Cookie {
	return &http.Cookie{
		Name:     auth.SessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isHTTPS(r),
		MaxAge:   -1,
	}
}

// isHTTPS reports whether the original client request was over TLS, honouring
// the Traefik/nginx X-Forwarded-Proto header (the control plane itself speaks
// plain HTTP behind the proxy).
func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return r.Header.Get("X-Forwarded-Proto") == "https"
}
