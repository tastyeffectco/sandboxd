// v1_agents_connect.go — Phase 10B: connect a coding-agent provider by one of
// two owner-supplied methods, generalized across the registry (opencode,
// claude-code, codex):
//
//   - IMPORT (subscription / OAuth): the owner runs `<cli> login` on their own
//     machine and pastes the resulting credential bundle. It is stored verbatim
//     at the provider's HOME-relative credential file — opaque, never parsed.
//   - API KEY: the owner pastes a provider API key. It is stored opaquely in the
//     provider's key file; at task time runtimed injects it as the provider's
//     one allowlisted key env var (see agentauth.APIKeyEnv).
//
// Each connect fully replaces the provider's auth dir, so a provider holds
// exactly one method at a time. No token is ever logged or returned.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/sandboxd/control-plane/internal/agentauth"
)

// provider validates {provider} against the registry, returning it and writing a
// 404 when unknown. Guards every connect endpoint.
func (s *Server) agentProvider(w http.ResponseWriter, r *http.Request) (agentauth.Provider, bool) {
	if s.AgentAuth == nil {
		writeErr(w, http.StatusServiceUnavailable, "agent auth not configured")
		return agentauth.Provider{}, false
	}
	p, ok := agentauth.Get(r.PathValue("provider"))
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown provider")
		return agentauth.Provider{}, false
	}
	return p, true
}

// POST /v1/agents/{provider}/import
// Body: {"credentials": "<contents of the CLI's login credential file>"}.
// Stored verbatim (opaque) at the provider's credential file; never parsed.
func (s *Server) v1AgentImport(w http.ResponseWriter, r *http.Request) {
	p, ok := s.agentProvider(w, r)
	if !ok {
		return
	}
	rel, ok := agentauth.CredentialFile(p.ID)
	if !ok {
		writeErr(w, http.StatusBadRequest, "provider does not support credential import")
		return
	}
	var body struct {
		Credentials string `json:"credentials"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256*1024)).Decode(&body); err != nil || body.Credentials == "" {
		writeErr(w, http.StatusBadRequest, "missing credentials")
		return
	}
	if err := s.AgentAuth.ImportCredential(p.ID, rel, []byte(body.Credentials)); err != nil {
		writeErr(w, http.StatusBadRequest, "could not import credentials")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"provider": p.ID, "status": "connected", "method": "oauth"})
}

// POST /v1/agents/{provider}/api-key
// Body: {"api_key": "<provider api key>"}. Stored opaquely; runtimed injects it
// as the provider's key env var at task time.
func (s *Server) v1AgentAPIKey(w http.ResponseWriter, r *http.Request) {
	p, ok := s.agentProvider(w, r)
	if !ok {
		return
	}
	if _, ok := agentauth.APIKeyEnv(p.ID); !ok {
		writeErr(w, http.StatusBadRequest, "provider does not support API-key auth")
		return
	}
	var body struct {
		APIKey string `json:"api_key"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&body); err != nil || body.APIKey == "" {
		writeErr(w, http.StatusBadRequest, "missing api_key")
		return
	}
	if err := s.AgentAuth.ImportCredential(p.ID, agentauth.APIKeyFile, []byte(body.APIKey)); err != nil {
		writeErr(w, http.StatusBadRequest, "could not store api key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"provider": p.ID, "status": "connected", "method": "api_key"})
}

// POST /v1/agents/{provider}/disconnect — deletes the stored auth dir.
func (s *Server) v1AgentDisconnect(w http.ResponseWriter, r *http.Request) {
	p, ok := s.agentProvider(w, r)
	if !ok {
		return
	}
	if err := s.AgentAuth.Delete(p.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not disconnect")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
