package store

import (
	"context"
	"database/sql"
)

// AppEvent mirrors an `app_events` row (migrations/0016) — one entry in the
// durable app/sandbox/task/config/snapshot activity timeline. owner_token is
// the tenant boundary; id is a ULID used as the newest-first page cursor.
type AppEvent struct {
	ID          string
	OwnerToken  string
	AppID       sql.NullString
	SandboxID   sql.NullString
	TaskID      sql.NullString
	SnapshotID  sql.NullString
	Type        string
	Severity    string
	Message     string
	PayloadJSON sql.NullString
	CreatedAt   string // RFC3339 UTC
}

const appEventCols = `id, owner_token, app_id, sandbox_id, task_id, snapshot_id,
	type, severity, message, payload_json, created_at`

func scanAppEvent(sc scanner) (*AppEvent, error) {
	e := &AppEvent{}
	if err := sc.Scan(&e.ID, &e.OwnerToken, &e.AppID, &e.SandboxID, &e.TaskID,
		&e.SnapshotID, &e.Type, &e.Severity, &e.Message, &e.PayloadJSON, &e.CreatedAt); err != nil {
		return nil, err
	}
	return e, nil
}

// InsertAppEvent appends one event row. Primitive args (not a struct) so the
// recorder in internal/events can satisfy a minimal interface without
// importing this package — mirrors InsertAudit. Goes through the single
// writer. Empty optional ids/payload are stored as NULL.
func (s *Store) InsertAppEvent(ctx context.Context, id, ownerToken, appID, sandboxID, taskID, snapshotID, typ, severity, message, payloadJSON, createdAt string) error {
	return s.submit(ctx, func(db *sql.DB) error {
		_, err := db.ExecContext(ctx, `
			INSERT INTO app_events (`+appEventCols+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, ownerToken, nullIfEmpty(appID), nullIfEmpty(sandboxID),
			nullIfEmpty(taskID), nullIfEmpty(snapshotID), typ, severity, message,
			nullIfEmpty(payloadJSON), createdAt)
		return err
	})
}

// listAppEventsBy is the shared newest-first, tenant-scoped, cursor-paginated
// read. `col` is the scoping column (app_id / task_id / sandbox_id); when
// scopeVal is "" only owner_token scopes (whole-tenant feed). `before` (a ULID)
// returns events strictly older than it; limit is clamped by the caller.
func (s *Store) listAppEventsBy(ctx context.Context, col, ownerToken, scopeVal, before string, limit int) ([]*AppEvent, error) {
	q := `SELECT ` + appEventCols + ` FROM app_events WHERE owner_token = ?`
	args := []any{ownerToken}
	if scopeVal != "" {
		q += ` AND ` + col + ` = ?`
		args = append(args, scopeVal)
	}
	if before != "" {
		q += ` AND id < ?`
		args = append(args, before)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*AppEvent{}
	for rows.Next() {
		e, err := scanAppEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListAppEvents returns an app's events for a tenant, newest-first, paginated
// by the `before` ULID cursor.
func (s *Store) ListAppEvents(ctx context.Context, ownerToken, appID, before string, limit int) ([]*AppEvent, error) {
	return s.listAppEventsBy(ctx, "app_id", ownerToken, appID, before, limit)
}

// ListTaskEvents returns a task's events for a tenant, newest-first.
func (s *Store) ListTaskEvents(ctx context.Context, ownerToken, taskID, before string, limit int) ([]*AppEvent, error) {
	return s.listAppEventsBy(ctx, "task_id", ownerToken, taskID, before, limit)
}
