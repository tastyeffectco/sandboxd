package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// AppConfig is one app-scoped config entry (migrations/0014). For
// sensitive entries ValueCiphertext+ValueNonce are set and
// ValuePlaintext is null; for non-sensitive entries it is the reverse.
// Encryption happens in the API layer — the store only persists bytes.
type AppConfig struct {
	ID              string
	AppID           string
	Key             string
	ValueCiphertext []byte
	ValueNonce      []byte
	ValuePlaintext  sql.NullString
	Sensitive       bool
	AccessPolicy    string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

const appConfigCols = `id, app_id, key, value_ciphertext, value_nonce,
	       value_plaintext, sensitive, access_policy, created_at, updated_at`

func scanAppConfig(sc scanner) (*AppConfig, error) {
	c := &AppConfig{}
	var created, updated int64
	var sensitive int
	err := sc.Scan(&c.ID, &c.AppID, &c.Key, &c.ValueCiphertext, &c.ValueNonce,
		&c.ValuePlaintext, &sensitive, &c.AccessPolicy, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.Sensitive = sensitive == 1
	c.CreatedAt = time.Unix(created, 0).UTC()
	c.UpdatedAt = time.Unix(updated, 0).UTC()
	return c, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// CreateAppConfig inserts a config entry. ErrConflict if (app_id, key) exists.
func (s *Store) CreateAppConfig(ctx context.Context, c *AppConfig) error {
	return s.submit(ctx, func(db *sql.DB) error {
		now := time.Now().Unix()
		_, err := db.ExecContext(ctx, `
			INSERT INTO app_config
			  (id, app_id, key, value_ciphertext, value_nonce, value_plaintext,
			   sensitive, access_policy, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)`,
			c.ID, c.AppID, c.Key, c.ValueCiphertext, c.ValueNonce, c.ValuePlaintext,
			boolToInt(c.Sensitive), c.AccessPolicy, now, now)
		if isUniqueViolation(err) {
			return ErrConflict
		}
		if err == nil {
			c.CreatedAt = time.Unix(now, 0).UTC()
			c.UpdatedAt = c.CreatedAt
		}
		return err
	})
}

// ListAppConfig returns an app's config entries, ordered by key.
func (s *Store) ListAppConfig(ctx context.Context, appID string) ([]*AppConfig, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+appConfigCols+` FROM app_config WHERE app_id = ? ORDER BY key`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AppConfig
	for rows.Next() {
		c, err := scanAppConfig(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetAppConfig returns one entry by (app_id, key), or ErrNotFound.
func (s *Store) GetAppConfig(ctx context.Context, appID, key string) (*AppConfig, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+appConfigCols+` FROM app_config WHERE app_id = ? AND key = ?`, appID, key)
	return scanAppConfig(row)
}

// UpdateAppConfig replaces the value/sensitive/access_policy of an entry.
// The caller supplies the fully-resolved new value fields. ErrNotFound
// when no entry matches.
func (s *Store) UpdateAppConfig(ctx context.Context, appID, key string, c *AppConfig) error {
	return s.submit(ctx, func(db *sql.DB) error {
		res, err := db.ExecContext(ctx, `
			UPDATE app_config
			   SET value_ciphertext = ?, value_nonce = ?, value_plaintext = ?,
			       sensitive = ?, access_policy = ?, updated_at = ?
			 WHERE app_id = ? AND key = ?`,
			c.ValueCiphertext, c.ValueNonce, c.ValuePlaintext,
			boolToInt(c.Sensitive), c.AccessPolicy, time.Now().Unix(), appID, key)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// DeleteAppConfig removes one entry. ErrNotFound when nothing matched.
func (s *Store) DeleteAppConfig(ctx context.Context, appID, key string) error {
	return s.submit(ctx, func(db *sql.DB) error {
		res, err := db.ExecContext(ctx, `DELETE FROM app_config WHERE app_id = ? AND key = ?`, appID, key)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}
