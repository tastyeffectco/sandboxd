package api

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// v1ClaudeLogin relays a project's Claude sign-in to Claude Code in its
// sandbox: GET status, POST start (returns Anthropic's sign-in link), POST
// /code (the code the user copied), DELETE sign out. The login is made and kept
// by Claude Code in the workspace; the control plane never sees the token.
func (s *Server) v1ClaudeLogin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sb, err := s.Store.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such sandbox")
		return
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if sb.Status != "running" {
		writeV1Err(w, http.StatusConflict, "conflict", "start the project to sign in with Claude")
		return
	}
	path := ""
	var body []byte
	if strings.HasSuffix(r.URL.Path, "/code") {
		path = "/code"
		if body, err = io.ReadAll(http.MaxBytesReader(w, r.Body, 4096)); err != nil {
			writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid request body")
			return
		}
	}
	code, out, err := s.runtimeClientFor(id).ClaudeLogin(r.Context(), r.Method, path, body)
	if err != nil {
		writeV1Err(w, http.StatusBadGateway, "sandbox_unavailable", "the project is not ready yet; try again in a moment")
		return
	}
	if code >= 400 {
		relayV1Error(w, code, out)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(out)
}
