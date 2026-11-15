package api

import (
	"context"
	"errors"
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
