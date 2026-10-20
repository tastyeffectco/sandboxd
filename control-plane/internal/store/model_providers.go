package store

import (
	"context"
	"database/sql"
	"errors"
)

// ModelProvider stores public configuration and encrypted credential bytes.
// Validation and encryption belong to the modelprovider service.
type ModelProvider struct {
	ID, Revision, ConfigJSON string
	KeyCiphertext, KeyNonce  []byte
}

func scanModelProvider(row scanner) (*ModelProvider, error) {
	p := &ModelProvider{}
	err := row.Scan(&p.ID, &p.Revision, &p.ConfigJSON, &p.KeyCiphertext, &p.KeyNonce)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

func (s *Store) GetModelProvider(ctx context.Context, id string) (*ModelProvider, error) {
	return scanModelProvider(s.db.QueryRowContext(ctx,
		`SELECT id, revision, config_json, key_ciphertext, key_nonce FROM model_provider WHERE id = ?`, id))
}

func (s *Store) ListModelProviders(ctx context.Context) ([]*ModelProvider, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, revision, config_json, key_ciphertext, key_nonce FROM model_provider ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ModelProvider{}
	for rows.Next() {
		p, err := scanModelProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SaveModelProvider replaces the complete connection. A fresh revision revokes
// previously issued task capabilities when the endpoint or credential changes.
func (s *Store) SaveModelProvider(ctx context.Context, p *ModelProvider) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `INSERT INTO model_provider
			(id, revision, config_json, key_ciphertext, key_nonce) VALUES (?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,
			config_json=excluded.config_json, key_ciphertext=excluded.key_ciphertext,
			key_nonce=excluded.key_nonce`,
			p.ID, p.Revision, p.ConfigJSON, p.KeyCiphertext, p.KeyNonce)
		return err
	})
}

func (s *Store) DeleteModelProvider(ctx context.Context, id string) error {
	return s.submit(ctx, func(db *sql.DB) error {
		result, err := db.ExecContext(ctx, `DELETE FROM model_provider WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}
