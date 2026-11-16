package store

import (
	"context"
	"database/sql"
)

// Assistant is a person's sandboxd agent: the coding agent of a hidden
// project, given sandboxd's tools.
type Assistant struct {
	OwnerToken string
	AppID      string
	SecretHash string
	CreatedAt  string
}

func scanAssistant(row *sql.Row) (*Assistant, error) {
	var a Assistant
	if err := row.Scan(&a.OwnerToken, &a.AppID, &a.SecretHash, &a.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &a, nil
}

// GetAssistant returns the person's sandboxd agent, or ErrNotFound.
func (s *Store) GetAssistant(ctx context.Context, owner string) (*Assistant, error) {
	return scanAssistant(s.db.QueryRowContext(ctx,
		`SELECT owner_token, app_id, secret_hash, created_at FROM assistant WHERE owner_token = ?`, owner))
}

// GetAssistantBySecretHash authenticates the sandboxd agent's tool calls.
func (s *Store) GetAssistantBySecretHash(ctx context.Context, hash string) (*Assistant, error) {
	return scanAssistant(s.db.QueryRowContext(ctx,
		`SELECT owner_token, app_id, secret_hash, created_at FROM assistant WHERE secret_hash = ?`, hash))
}

// PutAssistant records (or replaces) the person's sandboxd agent.
func (s *Store) PutAssistant(ctx context.Context, a *Assistant) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `INSERT INTO assistant (owner_token, app_id, secret_hash, created_at)
			VALUES (?, ?, ?, ?) ON CONFLICT(owner_token) DO UPDATE SET
			app_id = excluded.app_id, secret_hash = excluded.secret_hash, created_at = excluded.created_at`,
			a.OwnerToken, a.AppID, a.SecretHash, a.CreatedAt)
		return err
	})
}
