// v1_agents_connect.go — Phase 10B (revised): Claude Code credential IMPORT +
// disconnect. The interactive `claude setup-token` automation was removed: with
// claude v2's Ink/TUI flow it can't be driven by stdout-scrape + stdin-paste.
// Import stores an existing credential bundle opaquely (no parsing, no setup-token,
// no PTY — the PTY/xterm Connect flow is a later slice). A2 exposes claude-code only.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/sandboxd/control-plane/internal/agentauth"
)

const connectProvider = "claude-code"

// POST /v1/agents/claude-code/import
// Body: {"credentials": "<contents of ~/.claude/.credentials.json>"}.
// The bytes are stored verbatim (opaque) and never logged/parsed.
func (s *Server) v1AgentImport(w http.ResponseWriter, r *http.Request) {
	if s.AgentAuth == nil {
		writeErr(w, http.StatusServiceUnavailable, "agent auth not configured")
		return
	}
	rel, ok := agentauth.CredentialFile(connectProvider)
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
	if err := s.AgentAuth.ImportCredential(connectProvider, rel, []byte(body.Credentials)); err != nil {
		writeErr(w, http.StatusBadRequest, "could not import credentials")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"provider": connectProvider, "status": "connected"})
}

// POST /v1/agents/claude-code/disconnect — deletes the stored auth dir.
func (s *Server) v1AgentDisconnect(w http.ResponseWriter, _ *http.Request) {
	if s.AgentAuth == nil {
		writeErr(w, http.StatusServiceUnavailable, "agent auth not configured")
		return
	}
	if err := s.AgentAuth.Delete(connectProvider); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not disconnect")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
