package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// InstanceSettings is the persisted, runtime-editable lifecycle tuning (Phase
// 8B). Only safe operational timers — never secrets.
type InstanceSettings struct {
	IdleReapEnabled      bool
	IdleThresholdSeconds int
	KeepaliveMaxSeconds  int
}

// GetInstanceSettings returns the singleton row, or ErrNotFound if unset (the
// caller then falls back to env defaults).
func (s *Store) GetInstanceSettings(ctx context.Context) (*InstanceSettings, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT idle_reap_enabled, idle_threshold_seconds, keepalive_max_seconds
		   FROM instance_settings WHERE id = 1`)
	var enabled int
	out := &InstanceSettings{}
	err := row.Scan(&enabled, &out.IdleThresholdSeconds, &out.KeepaliveMaxSeconds)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	out.IdleReapEnabled = enabled != 0
	return out, nil
}

// SaveInstanceSettings upserts the singleton row.
func (s *Store) SaveInstanceSettings(ctx context.Context, v InstanceSettings) error {
	return s.submit(ctx, func(db *sql.DB) error {
		enabled := 0
		if v.IdleReapEnabled {
			enabled = 1
		}
		_, err := db.ExecContext(ctx, `
			INSERT INTO instance_settings (id, idle_reap_enabled, idle_threshold_seconds, keepalive_max_seconds, updated_at)
			VALUES (1, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
			  idle_reap_enabled = excluded.idle_reap_enabled,
			  idle_threshold_seconds = excluded.idle_threshold_seconds,
			  keepalive_max_seconds = excluded.keepalive_max_seconds,
			  updated_at = excluded.updated_at`,
			enabled, v.IdleThresholdSeconds, v.KeepaliveMaxSeconds, time.Now().Unix())
		return err
	})
}
