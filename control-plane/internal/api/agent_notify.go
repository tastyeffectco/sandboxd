package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// An agent's scheduled jobs and background work run without anyone in the
// chat. They reach the person through the message_user tool, which adds the
// message to the agent's conversation in sandboxd, whichever agent it is.

const (
	agentNotesPerHour = 30
	agentNoteMax      = 8000
)

type agentNotes struct {
	mu   sync.Mutex
	sent map[string][]time.Time
}

// take reports whether the agent may send one more message now.
func (n *agentNotes) take(agentID string, at time.Time) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.sent == nil {
		n.sent = map[string][]time.Time{}
	}
	recent := n.sent[agentID][:0]
	for _, t := range n.sent[agentID] {
		if at.Sub(t) < time.Hour {
			recent = append(recent, t)
		}
	}
	if len(recent) >= agentNotesPerHour {
		n.sent[agentID] = recent
		return false
	}
	n.sent[agentID] = append(recent, at)
	return true
}

func (s *Server) agentMessageUser(ctx context.Context, h *store.AgentInstance, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New("write the message in text")
	}
	if r := []rune(text); len(r) > agentNoteMax {
		text = string(r[:agentNoteMax]) + "…"
	}
	if !s.agentNotes.take(h.ID, time.Now()) {
		return "", errors.New("too many messages this hour; send one summary later")
	}
	m := &store.AgentMessage{ID: newULID(), AgentID: h.ID, Role: "assistant", Content: text,
		Steps: "[]", State: "done", Channel: "agent", CreatedAt: now()}
	m.UpdatedAt = m.CreatedAt
	if err := s.Store.AddAgentMessage(ctx, m); err != nil {
		return "", err
	}
	return "Sent. The user sees it in their chat with you.", nil
}

// agentInbox takes what an agent's scheduled job produced, from the agent's
// own webhook delivery (OpenClaw's automations), and puts it in the chat.
// It reads {"text"} and the common names agents use for a job's output.
func (s *Server) agentInbox(w http.ResponseWriter, r *http.Request, h *store.AgentInstance) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 256<<10))
	if err != nil {
		http.Error(w, "unreadable body", http.StatusBadRequest)
		return
	}
	text := inboxText(body)
	if text == "" {
		// Nothing to say (OpenClaw skips empty results; be safe anyway).
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if _, err := s.agentMessageUser(r.Context(), h, text); err != nil {
		http.Error(w, err.Error(), http.StatusTooManyRequests)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func inboxText(body []byte) string {
	var event map[string]any
	if json.Unmarshal(body, &event) != nil {
		return strings.TrimSpace(string(body))
	}
	str := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := event[k].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}
	text := str("text", "message", "notify", "summary", "output", "result")
	if failure := str("error"); failure != "" {
		text = strings.TrimSpace("A scheduled job failed: " + failure + "\n\n" + text)
	}
	if name := str("jobName", "name"); name != "" && text != "" {
		text = "**" + name + "**\n\n" + text
	}
	return text
}
