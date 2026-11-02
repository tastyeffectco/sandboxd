package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Release statuses (0029).
const (
	ReleaseBuilding = "building" // unpacking and running build.release
	ReleaseStarting = "starting" // switched to; waiting for the app to answer
	ReleaseActive   = "active"   // the version being served
	ReleasePrevious = "previous" // served before; can be gone back to
	ReleaseFailed   = "failed"   // never served (or rolled back); see message
)

// Release is one published version of an app.
type Release struct {
	ID          string
	AppID       string
	OwnerToken  string
	Commit      string
	Status      string
	Message     string
	CreatedAt   time.Time
	FinishedAt  sql.NullInt64
	ActivatedAt sql.NullInt64
}

const releaseCols = `id, app_id, owner_token, commit_sha, status, message, created_at, finished_at, activated_at`

func scanRelease(sc scanner) (*Release, error) {
	r := &Release{}
	var created int64
	err := sc.Scan(&r.ID, &r.AppID, &r.OwnerToken, &r.Commit, &r.Status, &r.Message, &created, &r.FinishedAt, &r.ActivatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.CreatedAt = time.Unix(created, 0).UTC()
	return r, nil
}

// CreateRelease inserts a release; the caller sets ID, AppID, OwnerToken, Commit and Status.
func (s *Store) CreateRelease(ctx context.Context, r *Release) error {
	return s.submit(ctx, func(db *sql.DB) error {
		now := time.Now().Unix()
		_, err := db.ExecContext(ctx, `INSERT INTO app_release (id, app_id, owner_token, commit_sha, status, message, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, r.ID, r.AppID, r.OwnerToken, r.Commit, r.Status, r.Message, now)
		if err == nil {
			r.CreatedAt = time.Unix(now, 0).UTC()
		}
		return err
	})
}

// SetReleaseStatus records a release's progress. Finished statuses stamp
// finished_at; active stamps activated_at.
func (s *Store) SetReleaseStatus(ctx context.Context, id, status, message string) error {
	return s.submit(ctx, func(db *sql.DB) error {
		now := time.Now().Unix()
		q := `UPDATE app_release SET status = ?, message = ?`
		args := []any{status, message}
		switch status {
		case ReleaseActive:
			q += `, finished_at = COALESCE(finished_at, ?), activated_at = ?`
			args = append(args, now, now)
		case ReleaseFailed:
			q += `, finished_at = COALESCE(finished_at, ?)`
			args = append(args, now)
		}
		q += ` WHERE id = ?`
		_, err := db.ExecContext(ctx, q, append(args, id)...)
		return err
	})
}

// ActivateRelease makes id the app's active release and demotes any other
// active one to previous, in one transaction.
func (s *Store) ActivateRelease(ctx context.Context, appID, id string) error {
	return s.submit(ctx, func(db *sql.DB) error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		now := time.Now().Unix()
		if _, err := tx.ExecContext(ctx, `UPDATE app_release SET status = ? WHERE app_id = ? AND status = ? AND id != ?`,
			ReleasePrevious, appID, ReleaseActive, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE app_release SET status = ?, message = '', activated_at = ?,
			finished_at = COALESCE(finished_at, ?) WHERE id = ? AND app_id = ?`, ReleaseActive, now, now, id, appID); err != nil {
			return err
		}
		return tx.Commit()
	})
}

// GetRelease returns one of a tenant's releases.
func (s *Store) GetRelease(ctx context.Context, appID, id, ownerToken string) (*Release, error) {
	return scanRelease(s.db.QueryRowContext(ctx,
		`SELECT `+releaseCols+` FROM app_release WHERE id = ? AND app_id = ? AND owner_token = ?`, id, appID, ownerToken))
}

// ListReleases returns an app's releases, newest first.
func (s *Store) ListReleases(ctx context.Context, appID, ownerToken string) ([]*Release, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+releaseCols+` FROM app_release WHERE app_id = ? AND owner_token = ? ORDER BY created_at DESC, id DESC`, appID, ownerToken)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Release
	for rows.Next() {
		r, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteRelease removes a release row (its folder is the caller's to remove).
func (s *Store) DeleteRelease(ctx context.Context, id string) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `DELETE FROM app_release WHERE id = ?`, id)
		return err
	})
}

// DeleteAppReleases removes all of an app's release rows (app delete).
func (s *Store) DeleteAppReleases(ctx context.Context, appID string) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `DELETE FROM app_release WHERE app_id = ?`, appID)
		return err
	})
}

// FailUnfinishedReleases marks releases left building/starting by a restart
// of sandboxd as failed. Called once at boot.
func (s *Store) FailUnfinishedReleases(ctx context.Context) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `UPDATE app_release SET status = ?, message = ?, finished_at = ?
			WHERE status IN (?, ?)`, ReleaseFailed, "sandboxd restarted while this version was being published",
			time.Now().Unix(), ReleaseBuilding, ReleaseStarting)
		return err
	})
}

// SetSandboxPublicHosts replaces a sandbox's extra hostnames.
func (s *Store) SetSandboxPublicHosts(ctx context.Context, id string, hosts []string) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `UPDATE sandbox SET public_hosts = ?, updated_at = ? WHERE id = ?`,
			marshalHosts(hosts), time.Now().Unix(), id)
		return err
	})
}

// PublicHostInUse reports whether any sandbox other than exceptID answers to host.
func (s *Store) PublicHostInUse(ctx context.Context, host, exceptID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sandbox, json_each(sandbox.public_hosts)
		WHERE json_each.value = ? AND sandbox.id != ?`, host, exceptID).Scan(&n)
	return n > 0, err
}

func marshalHosts(h []string) string {
	if h == nil {
		h = []string{}
	}
	b, err := json.Marshal(h)
	if err != nil {
		return "[]"
	}
	return string(b)
}
