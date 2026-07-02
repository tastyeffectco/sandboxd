package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// App is a durable product entity above sandboxes (migrations/0013).
// It owns the user-facing concept and outlives the sandbox that is its
// current running instance. external_* are optional integration tags.
type App struct {
	ID                string
	OwnerToken        string
	ExternalUserID    sql.NullString
	ExternalProjectID sql.NullString
	Name              string
	Description       string
	Tags              []string
	LatestSnapshotID  sql.NullString
	RuntimePreset     sql.NullString // runtime preset id (0017); "" = none
	// Git import metadata (0020); empty for a blank-from-preset app. The repo
	// URL is tokenless — no token is ever stored on the app.
	GitRepoURL      sql.NullString
	GitBranch       sql.NullString
	GitCredentialID sql.NullString
	LastImportAt    sql.NullInt64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// AppPatch carries the fields a PATCH may change; nil means "leave as-is".
type AppPatch struct {
	Name        *string
	Description *string
	Tags        *[]string
}

func marshalTags(tags []string) string {
	if tags == nil {
		tags = []string{}
	}
	b, err := json.Marshal(tags)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func scanApp(sc scanner) (*App, error) {
	a := &App{}
	var tags string
	var created, updated int64
	err := sc.Scan(&a.ID, &a.OwnerToken, &a.ExternalUserID, &a.ExternalProjectID,
		&a.Name, &a.Description, &tags, &a.LatestSnapshotID, &created, &updated, &a.RuntimePreset,
		&a.GitRepoURL, &a.GitBranch, &a.GitCredentialID, &a.LastImportAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(tags), &a.Tags)
	if a.Tags == nil {
		a.Tags = []string{}
	}
	a.CreatedAt = time.Unix(created, 0).UTC()
	a.UpdatedAt = time.Unix(updated, 0).UTC()
	return a, nil
}

const appSelectCols = `id, owner_token, external_user_id, external_project_id,
	       name, description, tags, latest_snapshot_id, created_at, updated_at, runtime_preset,
	       git_repo_url, git_branch, git_credential_id, last_import_at`

// SetAppImported stamps last_import_at after a successful Git clone.
func (s *Store) SetAppImported(ctx context.Context, id string, at int64) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `UPDATE app SET last_import_at = ? WHERE id = ?`, at, id)
		return err
	})
}

// CreateApp inserts a new app. The caller sets ID (ULID) and OwnerToken.
func (s *Store) CreateApp(ctx context.Context, a *App) error {
	return s.submit(ctx, func(db *sql.DB) error {
		now := time.Now().Unix()
		_, err := db.ExecContext(ctx, `
			INSERT INTO app (id, owner_token, external_user_id, external_project_id,
			                 name, description, tags, latest_snapshot_id,
			                 created_at, updated_at, runtime_preset,
			                 git_repo_url, git_branch, git_credential_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			a.ID, a.OwnerToken, a.ExternalUserID, a.ExternalProjectID,
			a.Name, a.Description, marshalTags(a.Tags), a.LatestSnapshotID, now, now, a.RuntimePreset,
			a.GitRepoURL, a.GitBranch, a.GitCredentialID)
		if err != nil {
			if isUniqueViolation(err) {
				return ErrConflict
			}
			return err
		}
		a.CreatedAt = time.Unix(now, 0).UTC()
		a.UpdatedAt = a.CreatedAt
		return nil
	})
}

// GetAppForOwner returns an app scoped to the tenant. A missing or
// cross-tenant app is ErrNotFound (don't leak existence across tenants).
func (s *Store) GetAppForOwner(ctx context.Context, id, ownerToken string) (*App, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+appSelectCols+` FROM app WHERE id = ? AND owner_token = ?`, id, ownerToken)
	return scanApp(row)
}

// GetApp returns an app by id without an owner filter. Owner-agnostic on
// purpose: used by background paths (e.g. the task watcher) that resolve an
// app's owner_token for event recording and have no tenant in scope. NOT for
// tenant-facing reads — those must use GetAppForOwner.
func (s *Store) GetApp(ctx context.Context, id string) (*App, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+appSelectCols+` FROM app WHERE id = ?`, id)
	return scanApp(row)
}

// ListAppsForOwner lists a tenant's apps, optionally filtered by the
// external_user_id integration tag (empty = no constraint).
func (s *Store) ListAppsForOwner(ctx context.Context, ownerToken, externalUserID string) ([]*App, error) {
	q := `SELECT ` + appSelectCols + ` FROM app WHERE owner_token = ?`
	args := []any{ownerToken}
	if externalUserID != "" {
		q += ` AND external_user_id = ?`
		args = append(args, externalUserID)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*App
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpdateApp applies a partial update scoped to the tenant. Returns
// ErrNotFound when no app matches (id, ownerToken).
func (s *Store) UpdateApp(ctx context.Context, id, ownerToken string, patch AppPatch) error {
	return s.submit(ctx, func(db *sql.DB) error {
		sets := []string{}
		args := []any{}
		if patch.Name != nil {
			sets = append(sets, "name = ?")
			args = append(args, *patch.Name)
		}
		if patch.Description != nil {
			sets = append(sets, "description = ?")
			args = append(args, *patch.Description)
		}
		if patch.Tags != nil {
			sets = append(sets, "tags = ?")
			args = append(args, marshalTags(*patch.Tags))
		}
		// Always bump updated_at; an empty patch is a touch.
		sets = append(sets, "updated_at = ?")
		args = append(args, time.Now().Unix())
		q := "UPDATE app SET " + joinComma(sets) + " WHERE id = ? AND owner_token = ?"
		args = append(args, id, ownerToken)
		res, err := db.ExecContext(ctx, q, args...)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// CurrentSandboxForApp returns the app's current (non-deleted) sandbox,
// or ErrNotFound when the app has none. A DELETE removes the sandbox
// row, so any remaining row is the current instance.
func (s *Store) CurrentSandboxForApp(ctx context.Context, appID string) (*Sandbox, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+sandboxSelectCols+` FROM sandbox WHERE app_id = ? ORDER BY created_at DESC LIMIT 1`, appID)
	sb, err := scanSandbox(row)
	if err != nil {
		return nil, err
	}
	sb.Ports, _ = s.portsFor(ctx, sb.ID)
	return sb, nil
}

// SandboxIDsForApp returns every sandbox id linked to the app (current +
// historical), newest first. Used by the full app-delete cascade to purge each.
func (s *Store) SandboxIDsForApp(ctx context.Context, appID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM sandbox WHERE app_id = ? ORDER BY created_at DESC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SnapshotImagePathsForApp returns the on-disk image_path of every snapshot
// captured from the app (source_app_id). The caller removes the files; the rows
// themselves are dropped by DeleteApp.
func (s *Store) SnapshotImagePathsForApp(ctx context.Context, appID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT image_path FROM snapshot WHERE source_app_id = ? AND image_path <> ''`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}

// DeleteApp removes the app row and every app-scoped DB row (config, events, and
// snapshot records captured from it) in one transaction. Sandbox rows and files
// are torn down separately by the purge path before this is called; this is the
// final metadata cleanup so nothing dangling references the app. Idempotent:
// deleting an already-absent app affects zero rows and returns nil.
func (s *Store) DeleteApp(ctx context.Context, appID string) error {
	return s.submit(ctx, func(db *sql.DB) error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, stmt := range []string{
			`DELETE FROM app_config WHERE app_id = ?`,
			`DELETE FROM app_events WHERE app_id = ?`,
			`DELETE FROM snapshot   WHERE source_app_id = ?`,
			`DELETE FROM app        WHERE id = ?`,
		} {
			if _, err := tx.ExecContext(ctx, stmt, appID); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}
