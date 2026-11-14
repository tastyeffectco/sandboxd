package store

import (
	"context"
	"database/sql"
)

// AgentMessage is one message in an agent's conversation. State is "working"
// while the agent is still answering, then "done", "stopped" or "error".
type AgentMessage struct {
	ID, AgentID, Role        string
	Content, Thinking, Steps string
	State, Error, Channel    string
	CreatedAt, UpdatedAt     string
}

const agentMessageColumns = `id, agent_id, role, content, thinking, steps, state, error, channel, created_at, updated_at`

func (s *Store) AddAgentMessage(ctx context.Context, m *AgentMessage) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `INSERT INTO agent_message (`+agentMessageColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			m.ID, m.AgentID, m.Role, m.Content, m.Thinking, m.Steps, m.State, m.Error, m.Channel, m.CreatedAt, m.UpdatedAt)
		return err
	})
}

// UpdateAgentMessage saves what an answer has become so far.
func (s *Store) UpdateAgentMessage(ctx context.Context, m *AgentMessage) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `UPDATE agent_message SET content = ?, thinking = ?, steps = ?, state = ?, error = ?, updated_at = ?
			WHERE id = ? AND agent_id = ?`, m.Content, m.Thinking, m.Steps, m.State, m.Error, m.UpdatedAt, m.ID, m.AgentID)
		return err
	})
}

// ListAgentMessages returns the agent's latest messages, oldest first.
func (s *Store) ListAgentMessages(ctx context.Context, agentID string, limit int) ([]*AgentMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+agentMessageColumns+` FROM (
		SELECT `+agentMessageColumns+` FROM agent_message WHERE agent_id = ? ORDER BY id DESC LIMIT ?) ORDER BY id`, agentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AgentMessage
	for rows.Next() {
		m := &AgentMessage{}
		if err := rows.Scan(&m.ID, &m.AgentID, &m.Role, &m.Content, &m.Thinking, &m.Steps, &m.State, &m.Error, &m.Channel, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAgentMessages(ctx context.Context, agentID string) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `DELETE FROM agent_message WHERE agent_id = ?`, agentID)
		return err
	})
}
