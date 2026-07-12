package store

import (
	"context"
	"database/sql"
	"time"
)

// APIKey is the METADATA view of an API key (migrations/0022). It never carries
// the key_hash — the hash is written by CreateAPIKey and only read back for
// lookup, never selected into this struct.
type APIKey struct {
	ID         string
	Name       string
	Prefix     string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// ── console password (single row id=1) ───────────────────────────────

// GetPasswordHash returns the stored bcrypt hash, or ErrNotFound when the
// console password has not been set yet (first-run).
func (s *Store) GetPasswordHash(ctx context.Context) (string, error) {
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM console_auth WHERE id = 1`).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return hash, err
}

// SetPasswordHash upserts the single console password row.
func (s *Store) SetPasswordHash(ctx context.Context, hash string) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `
			INSERT INTO console_auth (id, password_hash, updated_at) VALUES (1, ?, ?)
			ON CONFLICT(id) DO UPDATE SET password_hash = excluded.password_hash, updated_at = excluded.updated_at`,
			hash, time.Now().Unix())
		return err
	})
}

// ── sessions ─────────────────────────────────────────────────────────

// CreateSession inserts a session keyed by the sha256 hex of the cookie value.
func (s *Store) CreateSession(ctx context.Context, tokenHash, owner string, createdAt, lastUsed, expires int64) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `
			INSERT INTO console_session (token_hash, owner_token, created_at, last_used_at, expires_at)
			VALUES (?,?,?,?,?)`, tokenHash, owner, createdAt, lastUsed, expires)
		return err
	})
}

// LookupSession returns the owner + expiry for a session hash. found=false when
// there is no such session (expiry is enforced by the caller).
func (s *Store) LookupSession(ctx context.Context, tokenHash string) (owner string, expires int64, found bool, err error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT owner_token, expires_at FROM console_session WHERE token_hash = ?`, tokenHash)
	switch err = row.Scan(&owner, &expires); err {
	case nil:
		return owner, expires, true, nil
	case sql.ErrNoRows:
		return "", 0, false, nil
	default:
		return "", 0, false, err
	}
}

// TouchSession bumps last_used_at (best-effort; ignore the returned error at the
// call site).
func (s *Store) TouchSession(ctx context.Context, tokenHash string, lastUsed int64) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx,
			`UPDATE console_session SET last_used_at = ? WHERE token_hash = ?`, lastUsed, tokenHash)
		return err
	})
}

// DeleteSession removes one session (logout).
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `DELETE FROM console_session WHERE token_hash = ?`, tokenHash)
		return err
	})
}

// DeleteAllSessions removes every session (sign out everywhere / password change).
func (s *Store) DeleteAllSessions(ctx context.Context) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `DELETE FROM console_session`)
		return err
	})
}

// ── API keys ─────────────────────────────────────────────────────────

const apiKeyMetaCols = `id, name, prefix, created_at, last_used_at`

func scanAPIKey(sc scanner) (*APIKey, error) {
	k := &APIKey{}
	var created int64
	var lastUsed sql.NullInt64
	if err := sc.Scan(&k.ID, &k.Name, &k.Prefix, &created, &lastUsed); err != nil {
		return nil, err
	}
	k.CreatedAt = time.Unix(created, 0).UTC()
	if lastUsed.Valid {
		t := time.Unix(lastUsed.Int64, 0).UTC()
		k.LastUsedAt = &t
	}
	return k, nil
}

// CreateAPIKey inserts a key. keyHash is the sha256 hex of the plaintext (the
// store never sees plaintext). ErrConflict when the name (or hash) already exists.
func (s *Store) CreateAPIKey(ctx context.Context, id, name, keyHash, prefix string, createdAt int64) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `
			INSERT INTO api_key (id, name, key_hash, prefix, created_at, last_used_at)
			VALUES (?,?,?,?,?,NULL)`, id, name, keyHash, prefix, createdAt)
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return err
	})
}

// ListAPIKeys returns all keys as METADATA ONLY — the key_hash is never selected.
func (s *Store) ListAPIKeys(ctx context.Context) ([]*APIKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+apiKeyMetaCols+` FROM api_key ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*APIKey
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// LookupAPIKey returns the key id for a presented key's sha256 hash. found=false
// when no key matches.
func (s *Store) LookupAPIKey(ctx context.Context, keyHash string) (id string, found bool, err error) {
	row := s.db.QueryRowContext(ctx, `SELECT id FROM api_key WHERE key_hash = ?`, keyHash)
	switch err = row.Scan(&id); err {
	case nil:
		return id, true, nil
	case sql.ErrNoRows:
		return "", false, nil
	default:
		return "", false, err
	}
}

// TouchAPIKey bumps last_used_at (best-effort).
func (s *Store) TouchAPIKey(ctx context.Context, id string, lastUsed int64) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `UPDATE api_key SET last_used_at = ? WHERE id = ?`, lastUsed, id)
		return err
	})
}

// DeleteAPIKey removes a key by id. Returns false when no such key exists (404).
func (s *Store) DeleteAPIKey(ctx context.Context, id string) (bool, error) {
	var deleted bool
	err := s.submit(ctx, func(db *sql.DB) error {
		res, err := db.ExecContext(ctx, `DELETE FROM api_key WHERE id = ?`, id)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		deleted = n > 0
		return nil
	})
	return deleted, err
}
