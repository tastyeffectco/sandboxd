package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentcatalog"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// Each agent has one conversation, kept by sandboxd like a chat app. A reply
// runs here, not in the window: closing the window or the app does not stop
// it, and the person finds the whole thread when they come back.

const agentHistoryLimit = 60 // messages sent to the agent with each turn

type liveTurn struct {
	cancel context.CancelFunc
	msg    store.AgentMessage
	steps  []agentStep
}

type agentTurns struct {
	mu   sync.Mutex
	live map[string]*liveTurn // by agent id
}

func (t *agentTurns) get(agentID string) (*liveTurn, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	l, ok := t.live[agentID]
	return l, ok
}

type v1AgentMessage struct {
	ID        string          `json:"id"`
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	Thinking  string          `json:"thinking,omitempty"`
	Steps     json.RawMessage `json:"steps"`
	State     string          `json:"state"`
	Error     string          `json:"error,omitempty"`
	Channel   string          `json:"channel"`
	CreatedAt string          `json:"created_at"`
}

func agentMessageView(m *store.AgentMessage) v1AgentMessage {
	steps := json.RawMessage(m.Steps)
	if !json.Valid(steps) || string(steps) == "null" {
		steps = json.RawMessage("[]")
	}
	return v1AgentMessage{ID: m.ID, Role: m.Role, Content: m.Content, Thinking: m.Thinking, Steps: steps,
		State: m.State, Error: m.Error, Channel: m.Channel, CreatedAt: m.CreatedAt}
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// GET /v1/agent-instances/{id}/messages — the conversation, oldest first,
// including a reply that is still being written.
func (s *Server) v1ListAgentMessages(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownAgentInstance(w, r)
	if !ok {
		return
	}
	rows, err := s.Store.ListAgentMessages(r.Context(), h.ID, 200)
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := make([]v1AgentMessage, 0, len(rows))
	s.turns.mu.Lock()
	live := s.turns.live[h.ID]
	var snapshot *store.AgentMessage
	if live != nil {
		m := live.msg
		snapshot = &m
	}
	s.turns.mu.Unlock()
	for _, m := range rows {
		if snapshot != nil && m.ID == snapshot.ID {
			m = snapshot
		} else if m.State == "working" {
			// The engine restarted while it was answering.
			m.State, m.UpdatedAt = "stopped", now()
			_ = s.Store.UpdateAgentMessage(r.Context(), m)
		}
		out = append(out, agentMessageView(m))
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": out, "working": snapshot != nil})
}

// POST /v1/agent-instances/{id}/messages — send a message; the agent's answer
// is written in the background and appears in GET .../messages.
func (s *Server) v1SendAgentMessage(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownAgentInstance(w, r)
	if !ok {
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "send a message")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" || utf8.RuneCountInString(content) > 8000 {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "a message is 1 to 8000 characters")
		return
	}
	if v := s.agentInstanceView(r.Context(), h); v.Status != "running" {
		writeV1Err(w, http.StatusConflict, "not_running", h.Name+" isn't running yet. Start it, or wait until it's ready.")
		return
	}
	history, err := s.Store.ListAgentMessages(r.Context(), h.ID, agentHistoryLimit)
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	pkg, _ := agentcatalog.Get(h.Package)
	if err := s.refreshAgentConfig(r.Context(), h, pkg); err != nil && s.Log != nil {
		s.Log.Warn("agent chat: refresh config", "agent", h.ID, "err", err.Error())
	}
	apiKey, err := s.Secrets.Open(h.APIKeyCiphertext, h.APIKeyNonce)
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "the agent's API key cannot be read")
		return
	}
	user := &store.AgentMessage{ID: newULID(), AgentID: h.ID, Role: "user", Content: content, Steps: "[]",
		State: "done", Channel: "app", CreatedAt: now()}
	user.UpdatedAt = user.CreatedAt
	reply := &store.AgentMessage{ID: newULID(), AgentID: h.ID, Role: "assistant", Steps: "[]",
		State: "working", Channel: "app", CreatedAt: now()}
	reply.UpdatedAt = reply.CreatedAt
	// IDs order the thread. Two made in the same millisecond can sort either
	// way, and the answer must come after the question.
	for reply.ID <= user.ID {
		reply.ID = newULID()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	s.turns.mu.Lock()
	if s.turns.live == nil {
		s.turns.live = map[string]*liveTurn{}
	}
	if s.turns.live[h.ID] != nil {
		s.turns.mu.Unlock()
		cancel()
		writeV1Err(w, http.StatusConflict, "busy", h.Name+" is still answering. Wait for it, or stop it.")
		return
	}
	live := &liveTurn{cancel: cancel, msg: *reply, steps: []agentStep{}}
	s.turns.live[h.ID] = live
	s.turns.mu.Unlock()

	if err := s.Store.AddAgentMessage(r.Context(), user); err == nil {
		err = s.Store.AddAgentMessage(r.Context(), reply)
	}
	if err != nil {
		s.endTurn(h.ID, cancel)
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	var turn v1AgentStreamReq
	for _, m := range history {
		if m.Role == "user" || (m.Role == "assistant" && m.State == "done" && m.Content != "") {
			turn.Messages = append(turn.Messages, struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}{m.Role, m.Content})
		}
	}
	turn.Messages = append(turn.Messages, struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{"user", content})
	url := fmt.Sprintf("http://%s:%d/v1/chat/completions", agentContainer(h.ID), pkg.APIPort)
	name := h.Name
	go s.runAgentTurn(ctx, cancel, h.ID, name, url, string(apiKey), pkg.ChatModel, turn, live)

	writeJSON(w, http.StatusAccepted, map[string]any{"messages": []v1AgentMessage{agentMessageView(user), agentMessageView(reply)}})
}

func (s *Server) runAgentTurn(ctx context.Context, cancel context.CancelFunc, agentID, name, url, key, model string,
	turn v1AgentStreamReq, live *liveTurn) {
	defer s.endTurn(agentID, cancel)
	lastSave := time.Now()
	persist := func(force bool) {
		if !force && time.Since(lastSave) < 2*time.Second {
			return
		}
		lastSave = time.Now()
		s.turns.mu.Lock()
		m := live.msg
		s.turns.mu.Unlock()
		_ = s.Store.UpdateAgentMessage(context.Background(), &m)
	}
	err := streamAgentChat(ctx, url, key, model, turn, func(e agentEvent) {
		s.turns.mu.Lock()
		switch e.name {
		case "text":
			live.msg.Content += e.data.(map[string]string)["delta"]
		case "thinking":
			live.msg.Thinking += e.data.(map[string]string)["delta"]
		case "step":
			step := e.data.(agentStep)
			found := false
			for i := range live.steps {
				if live.steps[i].ID == step.ID && step.ID != "" {
					live.steps[i].Status = step.Status
					found = true
				}
			}
			if !found {
				live.steps = append(live.steps, step)
			}
			b, _ := json.Marshal(live.steps)
			live.msg.Steps = string(b)
		}
		live.msg.UpdatedAt = now()
		s.turns.mu.Unlock()
		persist(e.name == "step")
	})
	s.turns.mu.Lock()
	switch {
	case err == nil:
		live.msg.State = "done"
	case ctx.Err() == context.Canceled:
		live.msg.State = "stopped"
	default:
		live.msg.State = "error"
		live.msg.Error = plainAgentError(name, err)
		s.Log.Warn("agent turn failed", "agent", agentID, "err", err.Error())
	}
	for i := range live.steps {
		if live.steps[i].Status == "running" {
			live.steps[i].Status = map[bool]string{true: "done", false: "failed"}[live.msg.State == "done"]
		}
	}
	if live.steps == nil {
		live.steps = []agentStep{}
	}
	b, _ := json.Marshal(live.steps)
	live.msg.Steps = string(b)
	live.msg.UpdatedAt = now()
	s.turns.mu.Unlock()
	persist(true)
}

func (s *Server) endTurn(agentID string, cancel context.CancelFunc) {
	cancel()
	s.turns.mu.Lock()
	delete(s.turns.live, agentID)
	s.turns.mu.Unlock()
}

// POST /v1/agent-instances/{id}/messages/stop — stop the reply being written.
func (s *Server) v1StopAgentMessage(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownAgentInstance(w, r)
	if !ok {
		return
	}
	if live, ok := s.turns.get(h.ID); ok {
		live.cancel()
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /v1/agent-instances/{id}/messages — clear the conversation. The
// agent's own memory (what it learned about the person) stays.
func (s *Server) v1ClearAgentMessages(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownAgentInstance(w, r)
	if !ok {
		return
	}
	if _, busy := s.turns.get(h.ID); busy {
		writeV1Err(w, http.StatusConflict, "busy", h.Name+" is still answering. Stop it first.")
		return
	}
	if err := s.Store.DeleteAgentMessages(r.Context(), h.ID); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
