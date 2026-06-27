package store

import (
	"context"
	"database/sql"
	"time"
)

// GitCredential is the METADATA view of an owner-scoped Git credential
// (migrations/0019). It deliberately has NO secret field — the encrypted token
// (secret_enc/secret_nonce) is written by Create and is never selected back
// into this struct. Encryption happens in the API layer; the store only
// persists bytes.
type GitCredential struct {
	ID         string
	OwnerToken string
	Name       string
	Host       string
	Username   string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// gitCredentialMetaCols intentionally OMITS secret_enc/secret_nonce.
const gitCredentialMetaCols = `id, owner_token, name, host, username, created_at, updated_at`

func scanGitCredential(sc scanner) (*GitCredential, error) {
	g := &GitCredential{}
	var created, updated int64
	if err := sc.Scan(&g.ID, &g.OwnerToken, &g.Name, &g.Host, &g.Username, &created, &updated); err != nil {
		return nil, err
	}
	g.CreatedAt = time.Unix(created, 0).UTC()
	g.UpdatedAt = time.Unix(updated, 0).UTC()
	return g, nil
}

// CreateGitCredential inserts an owner-scoped credential. secretEnc/secretNonce
// are the sealed token (the store never sees plaintext). ErrConflict when
// (owner_token, name) already exists.
func (s *Store) CreateGitCredential(ctx context.Context, g *GitCredential, secretEnc, secretNonce []byte) error {
	return s.submit(ctx, func(db *sql.DB) error {
		now := time.Now().Unix()
		_, err := db.ExecContext(ctx, `
			INSERT INTO git_credential
			  (id, owner_token, name, host, username, secret_enc, secret_nonce, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			g.ID, g.OwnerToken, g.Name, g.Host, g.Username, secretEnc, secretNonce, now, now)
		if isUniqueViolation(err) {
			return ErrConflict
		}
		if err == nil {
			g.CreatedAt = time.Unix(now, 0).UTC()
			g.UpdatedAt = g.CreatedAt
		}
		return err
	})
}

// ListGitCredentials returns an owner's credentials as METADATA ONLY — the
// secret columns are never selected.
func (s *Store) ListGitCredentials(ctx context.Context, owner string) ([]*GitCredential, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+gitCredentialMetaCols+` FROM git_credential WHERE owner_token = ? ORDER BY created_at DESC`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*GitCredential
	for rows.Next() {
		g, err := scanGitCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GetGitCredentialSecret returns the SEALED token bytes (ciphertext + nonce) for
// an owner's credential. Control-plane-only: this is the decrypt seam the Git
// import slice (A1) will use server-side; it is never exposed via the API and
// the plaintext is never delivered toward a sandbox. found=false when there is
// no such credential for that owner.
func (s *Store) GetGitCredentialSecret(ctx context.Context, owner, id string) (secretEnc, secretNonce []byte, found bool, err error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT secret_enc, secret_nonce FROM git_credential WHERE owner_token = ? AND id = ?`, owner, id)
	switch err = row.Scan(&secretEnc, &secretNonce); err {
	case nil:
		return secretEnc, secretNonce, true, nil
	case sql.ErrNoRows:
		return nil, nil, false, nil
	default:
		return nil, nil, false, err
	}
}

// DeleteGitCredential deletes an owner's credential by id. Returns false when no
// such credential exists FOR THAT OWNER (=> 404); an owner can never delete
// another owner's credential.
func (s *Store) DeleteGitCredential(ctx context.Context, owner, id string) (bool, error) {
	var deleted bool
	err := s.submit(ctx, func(db *sql.DB) error {
		res, err := db.ExecContext(ctx,
			`DELETE FROM git_credential WHERE owner_token = ? AND id = ?`, owner, id)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		deleted = n > 0
		return nil
	})
	return deleted, err
}
