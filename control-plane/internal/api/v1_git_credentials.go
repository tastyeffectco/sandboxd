// v1_git_credentials.go — Git A0 (v0.4.2): an owner-scoped, encrypted-at-rest
// Git credential store. Create/list/delete METADATA only — the access token is
// write-only: sealed with secrets.Cipher, never returned by any endpoint, never
// logged, and (in A0) not used by anything. Git import/clone/push are later
// v0.4.x slices.
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/audit"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/auth"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

type v1GitCredential struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Host      string `json:"host"`
	Username  string `json:"username"`
	TokenSet  bool   `json:"token_set"` // always true; signals a secret exists, never the value
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

func toV1GitCredential(g *store.GitCredential) v1GitCredential {
	return v1GitCredential{
		ID:        g.ID,
		Name:      g.Name,
		Host:      g.Host,
		Username:  g.Username,
		TokenSet:  true,
		CreatedAt: g.CreatedAt.Format(time.RFC3339),
		UpdatedAt: g.UpdatedAt.Format(time.RFC3339),
	}
}

// POST /v1/git-credentials — store an encrypted token. Returns metadata only.
func (s *Server) v1CreateGitCredential(w http.ResponseWriter, r *http.Request) {
	if s.Store == nil || s.Secrets == nil {
		writeV1Err(w, http.StatusServiceUnavailable, "unavailable", "git credential store not configured")
		return
	}
	var body struct {
		Name     string `json:"name"`
		Host     string `json:"host"`
		Username string `json:"username"`
		Token    string `json:"token"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	name := strings.TrimSpace(body.Name)
	host := strings.TrimSpace(body.Host)
	username := strings.TrimSpace(body.Username)
	switch {
	case !validLabel(name, 64):
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "name must be 1-64 printable characters")
		return
	case !validToken(body.Token):
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "token must be 1-4096 chars with no NUL/CR/LF")
		return
	case host != "" && !validLabel(host, 253):
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "host must be a hostname or empty")
		return
	case username != "" && !validLabel(username, 128):
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "username must be printable or empty")
		return
	}

	ct, nonce, err := s.Secrets.Seal([]byte(body.Token))
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "could not encrypt token")
		return
	}
	g := &store.GitCredential{
		ID:         newULID(),
		OwnerToken: tenantToken(r),
		Name:       name,
		Host:       host,
		Username:   username,
	}
	if err := s.Store.CreateGitCredential(r.Context(), g, ct, nonce); err != nil {
		if err == store.ErrConflict {
			writeV1Err(w, http.StatusConflict, "conflict", "a credential with that name already exists")
			return
		}
		writeV1Err(w, http.StatusInternalServerError, "internal", "could not store credential")
		return
	}
	s.auditGit(r, "git_credential.created", g.ID, g.Name)
	writeJSON(w, http.StatusCreated, toV1GitCredential(g))
}

// GET /v1/git-credentials — owner-scoped list, metadata only (no token).
func (s *Server) v1ListGitCredentials(w http.ResponseWriter, r *http.Request) {
	if s.Store == nil {
		writeV1Err(w, http.StatusServiceUnavailable, "unavailable", "git credential store not configured")
		return
	}
	creds, err := s.Store.ListGitCredentials(r.Context(), tenantToken(r))
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "could not list credentials")
		return
	}
	out := make([]v1GitCredential, 0, len(creds))
	for _, g := range creds {
		out = append(out, toV1GitCredential(g))
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": out})
}

// DELETE /v1/git-credentials/{id} — owner-scoped delete.
func (s *Server) v1DeleteGitCredential(w http.ResponseWriter, r *http.Request) {
	if s.Store == nil {
		writeV1Err(w, http.StatusServiceUnavailable, "unavailable", "git credential store not configured")
		return
	}
	id := r.PathValue("id")
	deleted, err := s.Store.DeleteGitCredential(r.Context(), tenantToken(r), id)
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "could not delete credential")
		return
	}
	if !deleted {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such credential")
		return
	}
	s.auditGit(r, "git_credential.deleted", id, "")
	w.WriteHeader(http.StatusNoContent)
}

// auditGit records a credential change WITHOUT the token (id/name only).
func (s *Server) auditGit(r *http.Request, action, id, name string) {
	if s.Audit == nil {
		return
	}
	actor := auth.ActorFrom(r.Context())
	detail := map[string]any{}
	if name != "" {
		detail["name"] = name
	}
	s.Audit.Write(r.Context(), audit.Entry{
		ActorKind: actor.Kind,
		ActorName: actor.Name,
		Action:    action,
		Target:    id,
		Detail:    detail, // never the token
	})
}

// validLabel: 1..max printable, non-control characters.
func validLabel(s string, max int) bool {
	if s == "" || len(s) > max {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// validToken: 1..4096 chars, no NUL/CR/LF (defuses argv/askpass injection when
// the token is used by the Git import slice later).
func validToken(s string) bool {
	if s == "" || len(s) > 4096 {
		return false
	}
	return !strings.ContainsAny(s, "\x00\r\n")
}
