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
	CreatedAt         time.Time
	UpdatedAt         time.Time
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
		&a.Name, &a.Description, &tags, &a.LatestSnapshotID, &created, &updated, &a.RuntimePreset)
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
	       name, description, tags, latest_snapshot_id, created_at, updated_at, runtime_preset`

// CreateApp inserts a new app. The caller sets ID (ULID) and OwnerToken.
func (s *Store) CreateApp(ctx context.Context, a *App) error {
	return s.submit(ctx, func(db *sql.DB) error {
		now := time.Now().Unix()
		_, err := db.ExecContext(ctx, `
			INSERT INTO app (id, owner_token, external_user_id, external_project_id,
			                 name, description, tags, latest_snapshot_id,
			                 created_at, updated_at, runtime_preset)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			a.ID, a.OwnerToken, a.ExternalUserID, a.ExternalProjectID,
			a.Name, a.Description, marshalTags(a.Tags), a.LatestSnapshotID, now, now, a.RuntimePreset)
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
