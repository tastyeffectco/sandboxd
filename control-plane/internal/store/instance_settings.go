package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// InstanceSettings is the persisted, runtime-editable instance config: lifecycle
// tuning (Phase 8B) plus per-agent default models. Only safe operational values —
// never secrets.
type InstanceSettings struct {
	IdleReapEnabled      bool
	IdleThresholdSeconds int
	KeepaliveMaxSeconds  int
	// AgentDefaultModels maps an agent id (e.g. "opencode") to the default model
	// id used when a task doesn't specify one. Opaque, operator-supplied. Never nil
	// on read (empty map when unset).
	AgentDefaultModels map[string]string
}

// GetInstanceSettings returns the singleton row, or ErrNotFound if unset (the
// caller then falls back to env defaults).
func (s *Store) GetInstanceSettings(ctx context.Context) (*InstanceSettings, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT idle_reap_enabled, idle_threshold_seconds, keepalive_max_seconds, agent_default_models
		   FROM instance_settings WHERE id = 1`)
	var enabled int
	var modelsJSON string
	out := &InstanceSettings{}
	err := row.Scan(&enabled, &out.IdleThresholdSeconds, &out.KeepaliveMaxSeconds, &modelsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	out.IdleReapEnabled = enabled != 0
	out.AgentDefaultModels = map[string]string{}
	if modelsJSON != "" {
		// Tolerate a malformed value rather than failing the whole read.
		_ = json.Unmarshal([]byte(modelsJSON), &out.AgentDefaultModels)
	}
	return out, nil
}

// SaveInstanceSettings upserts the singleton row (all fields).
func (s *Store) SaveInstanceSettings(ctx context.Context, v InstanceSettings) error {
	return s.submit(ctx, func(db *sql.DB) error {
		enabled := 0
		if v.IdleReapEnabled {
			enabled = 1
		}
		models := v.AgentDefaultModels
		if models == nil {
			models = map[string]string{}
		}
		modelsJSON, err := json.Marshal(models)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `
			INSERT INTO instance_settings (id, idle_reap_enabled, idle_threshold_seconds, keepalive_max_seconds, agent_default_models, updated_at)
			VALUES (1, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
			  idle_reap_enabled = excluded.idle_reap_enabled,
			  idle_threshold_seconds = excluded.idle_threshold_seconds,
			  keepalive_max_seconds = excluded.keepalive_max_seconds,
			  agent_default_models = excluded.agent_default_models,
			  updated_at = excluded.updated_at`,
			enabled, v.IdleThresholdSeconds, v.KeepaliveMaxSeconds, string(modelsJSON), time.Now().Unix())
		return err
	})
}
