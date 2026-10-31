package store

import (
	"context"
	"database/sql"
	"errors"
)

// Helper is an operator agent (e.g. Hermes) the owner added. Its container is
// derived from ID; it is not stored.
type Helper struct {
	ID, OwnerToken, Package, Name string
	ConnectionID, Model           string
	SecretHash                    string
	APIKeyCiphertext, APIKeyNonce []byte
	TelegramCiphertext            []byte
	TelegramNonce                 []byte
	TelegramAllowedUsers          string
	AppsDay                       string // UTC date the counter below belongs to
	AppsToday                     int    // apps this helper created that day
	CreatedAt                     string
}

const helperColumns = `id, owner_token, package, name, connection_id, model, secret_hash,
	api_key_ciphertext, api_key_nonce, telegram_ciphertext, telegram_nonce, telegram_allowed_users, apps_day, apps_today, created_at`

func scanHelper(row scanner) (*Helper, error) {
	h := &Helper{}
	err := row.Scan(&h.ID, &h.OwnerToken, &h.Package, &h.Name, &h.ConnectionID, &h.Model, &h.SecretHash,
		&h.APIKeyCiphertext, &h.APIKeyNonce, &h.TelegramCiphertext, &h.TelegramNonce, &h.TelegramAllowedUsers, &h.AppsDay, &h.AppsToday, &h.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return h, err
}

func (s *Store) CreateHelper(ctx context.Context, h *Helper) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `INSERT INTO helper (`+helperColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			h.ID, h.OwnerToken, h.Package, h.Name, h.ConnectionID, h.Model, h.SecretHash,
			h.APIKeyCiphertext, h.APIKeyNonce, h.TelegramCiphertext, h.TelegramNonce, h.TelegramAllowedUsers, h.AppsDay, h.AppsToday, h.CreatedAt)
		return err
	})
}

// GetHelperForOwner returns the owner's helper, or ErrNotFound.
func (s *Store) GetHelperForOwner(ctx context.Context, id, owner string) (*Helper, error) {
	return scanHelper(s.db.QueryRowContext(ctx,
		`SELECT `+helperColumns+` FROM helper WHERE id = ? AND owner_token = ?`, id, owner))
}

// GetHelperBySecretHash authenticates a helper by its secret's hash.
func (s *Store) GetHelperBySecretHash(ctx context.Context, hash string) (*Helper, error) {
	return scanHelper(s.db.QueryRowContext(ctx,
		`SELECT `+helperColumns+` FROM helper WHERE secret_hash = ?`, hash))
}

func (s *Store) ListHelpersForOwner(ctx context.Context, owner string) ([]*Helper, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+helperColumns+` FROM helper WHERE owner_token = ? ORDER BY created_at`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Helper{}
	for rows.Next() {
		h, err := scanHelper(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// RotateHelperSecret replaces the helper secret's hash (on every container
// re-creation, so a leaked secret dies with its container).
func (s *Store) RotateHelperSecret(ctx context.Context, id, hash string) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `UPDATE helper SET secret_hash = ? WHERE id = ?`, hash, id)
		return err
	})
}

func (s *Store) DeleteHelper(ctx context.Context, id string) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `DELETE FROM helper WHERE id = ?`, id)
		return err
	})
}

// TakeHelperAppSlot counts one app created today against the helper's daily
// limit; false when the limit is reached. day is the UTC date (YYYY-MM-DD).
func (s *Store) TakeHelperAppSlot(ctx context.Context, id, day string, limit int) (bool, error) {
	took := false
	err := s.submit(ctx, func(db *sql.DB) error {
		res, err := db.ExecContext(ctx, `UPDATE helper SET
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
