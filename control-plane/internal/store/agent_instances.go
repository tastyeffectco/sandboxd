package store

import (
	"context"
	"database/sql"
	"errors"
)

// Agent is an operator agent (e.g. Hermes) the owner added. Its container is
// derived from ID; it is not stored.
type AgentInstance struct {
	ID, OwnerToken, Package, Name string
	ConnectionID, Model           string
	SecretHash                    string
	APIKeyCiphertext, APIKeyNonce []byte
	TelegramCiphertext            []byte
	TelegramNonce                 []byte
	TelegramAllowedUsers          string
	TelegramBot                   string // the bot's @username, once connected
	WhatsAppNumber                string // the owner's linked number (digits)
	AppsDay                       string // UTC date the counter below belongs to
	AppsToday                     int    // apps this agent created that day
	CreatedAt                     string
}

const agentInstanceColumns = `id, owner_token, package, name, connection_id, model, secret_hash,
	api_key_ciphertext, api_key_nonce, telegram_ciphertext, telegram_nonce, telegram_allowed_users, telegram_bot, whatsapp_number, apps_day, apps_today, created_at`

func scanAgentInstance(row scanner) (*AgentInstance, error) {
	h := &AgentInstance{}
	err := row.Scan(&h.ID, &h.OwnerToken, &h.Package, &h.Name, &h.ConnectionID, &h.Model, &h.SecretHash,
		&h.APIKeyCiphertext, &h.APIKeyNonce, &h.TelegramCiphertext, &h.TelegramNonce, &h.TelegramAllowedUsers, &h.TelegramBot, &h.WhatsAppNumber, &h.AppsDay, &h.AppsToday, &h.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return h, err
}

func (s *Store) CreateAgentInstance(ctx context.Context, h *AgentInstance) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `INSERT INTO agent_instance (`+agentInstanceColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			h.ID, h.OwnerToken, h.Package, h.Name, h.ConnectionID, h.Model, h.SecretHash,
			h.APIKeyCiphertext, h.APIKeyNonce, h.TelegramCiphertext, h.TelegramNonce, h.TelegramAllowedUsers, h.TelegramBot, h.WhatsAppNumber, h.AppsDay, h.AppsToday, h.CreatedAt)
		return err
	})
}

// GetAgentInstanceForOwner returns the owner's agent, or ErrNotFound.
func (s *Store) GetAgentInstanceForOwner(ctx context.Context, id, owner string) (*AgentInstance, error) {
	return scanAgentInstance(s.db.QueryRowContext(ctx,
		`SELECT `+agentInstanceColumns+` FROM agent_instance WHERE id = ? AND owner_token = ?`, id, owner))
}

// GetAgentInstanceBySecretHash authenticates an agent by its secret's hash.
func (s *Store) GetAgentInstanceBySecretHash(ctx context.Context, hash string) (*AgentInstance, error) {
	return scanAgentInstance(s.db.QueryRowContext(ctx,
		`SELECT `+agentInstanceColumns+` FROM agent_instance WHERE secret_hash = ?`, hash))
}

func (s *Store) ListAgentInstancesForOwner(ctx context.Context, owner string) ([]*AgentInstance, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+agentInstanceColumns+` FROM agent_instance WHERE owner_token = ? ORDER BY created_at`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*AgentInstance{}
	for rows.Next() {
		h, err := scanAgentInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// RotateAgentInstanceSecret replaces the agent secret's hash (on every container
// re-creation, so a leaked secret dies with its container).
func (s *Store) RotateAgentInstanceSecret(ctx context.Context, id, hash string) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `UPDATE agent_instance SET secret_hash = ? WHERE id = ?`, hash, id)
		return err
	})
}

// SetAgentInstanceTelegram saves (or, with a nil token, removes) the agent's
// Telegram bot and the users it answers.
func (s *Store) SetAgentInstanceTelegram(ctx context.Context, id string, ciphertext, nonce []byte, bot, users string) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `UPDATE agent_instance SET telegram_ciphertext = ?, telegram_nonce = ?,
			telegram_bot = ?, telegram_allowed_users = ? WHERE id = ?`, ciphertext, nonce, bot, users, id)
		return err
	})
}

// SetAgentInstanceWhatsApp saves the owner's linked WhatsApp number ("" unlinks).
func (s *Store) SetAgentInstanceWhatsApp(ctx context.Context, id, number string) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `UPDATE agent_instance SET whatsapp_number = ? WHERE id = ?`, number, id)
		return err
	})
}

func (s *Store) DeleteAgentInstance(ctx context.Context, id string) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `DELETE FROM agent_instance WHERE id = ?`, id)
		return err
	})
}

// TakeAgentAppSlot counts one app created today against the agent's daily
// limit; false when the limit is reached. day is the UTC date (YYYY-MM-DD).
func (s *Store) TakeAgentAppSlot(ctx context.Context, id, day string, limit int) (bool, error) {
	took := false
	err := s.submit(ctx, func(db *sql.DB) error {
		res, err := db.ExecContext(ctx, `UPDATE agent_instance SET
			apps_today = CASE WHEN apps_day = ? THEN apps_today + 1 ELSE 1 END, apps_day = ?
			WHERE id = ? AND (apps_day <> ? OR apps_today < ?)`, day, day, id, day, limit)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		took = n == 1
		return nil
	})
	return took, err
}
