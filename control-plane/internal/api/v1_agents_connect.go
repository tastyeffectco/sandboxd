// v1_agents_connect.go — Phase 10B A2: console-driven "Connect Claude Code".
// sandboxd runs `claude setup-token` in an ephemeral auth container, bridges the
// login URL out and the pasted code in, and on success promotes the opaque auth
// material into the store. The URL/code/state are sensitive-ish session data:
// they are returned to the console but never logged, persisted, or emitted in
// events. A2 exposes claude-code only.
package api

import (
	"encoding/json"
	"net/http"
)

const connectProvider = "claude-code"

// POST /v1/agents/claude-code/connect
func (s *Server) v1AgentConnect(w http.ResponseWriter, _ *http.Request) {
	if s.AgentSessions == nil {
		writeErr(w, http.StatusServiceUnavailable, "agent auth not configured")
		return
	}
	sess, err := s.AgentSessions.Connect(connectProvider)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "could not start login")
		return
	}
	state, url, _, _ := s.AgentSessions.Get(sess.ID)
	out := map[string]any{"session_id": sess.ID, "status": state}
	if url != "" {
		out["url"] = url
	}
	writeJSON(w, http.StatusCreated, out)
}

// GET /v1/agents/claude-code/connect/{id}
func (s *Server) v1AgentConnectStatus(w http.ResponseWriter, r *http.Request) {
	if s.AgentSessions == nil {
		writeErr(w, http.StatusServiceUnavailable, "agent auth not configured")
		return
	}
	state, url, errMsg, ok := s.AgentSessions.Get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "no such session")
		return
	}
	out := map[string]any{"session_id": r.PathValue("id"), "status": state}
	if url != "" {
		out["url"] = url
	}
	if errMsg != "" {
		out["error"] = errMsg
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /v1/agents/claude-code/connect/{id}/code
func (s *Server) v1AgentConnectCode(w http.ResponseWriter, r *http.Request) {
	if s.AgentSessions == nil {
		writeErr(w, http.StatusServiceUnavailable, "agent auth not configured")
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil || body.Code == "" {
		writeErr(w, http.StatusBadRequest, "missing code")
		return
	}
	if err := s.AgentSessions.SubmitCode(r.PathValue("id"), body.Code); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	state, _, errMsg, _ := s.AgentSessions.Get(r.PathValue("id"))
	out := map[string]any{"session_id": r.PathValue("id"), "status": state}
	if errMsg != "" {
		out["error"] = errMsg
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /v1/agents/claude-code/disconnect
func (s *Server) v1AgentDisconnect(w http.ResponseWriter, _ *http.Request) {
	if s.AgentSessions == nil {
		writeErr(w, http.StatusServiceUnavailable, "agent auth not configured")
		return
	}
	if err := s.AgentSessions.Disconnect(connectProvider); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not disconnect")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
