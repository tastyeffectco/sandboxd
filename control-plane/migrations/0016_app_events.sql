-- Phase 5 (observability foundation): one append-only event timeline for
-- app / sandbox / task / config / snapshot activity, kept in the EXISTING
-- control-plane SQLite DB (deliberately no separate logs DB, ClickHouse,
-- OTEL, or Loki yet — see the phase doc). Designed to export cleanly later:
-- stable machine-readable `type` names, enough IDs to join, and small valid
-- JSON in `payload_json`.
--
-- owner_token is the tenant boundary (= auth.Actor.Name), matching apps and
-- snapshots, so the read API can scope strictly. Append-only in normal
-- operation. `id` is a ULID: unique AND time-sortable, so it doubles as the
-- pagination cursor (newest-first = ORDER BY id DESC) without created_at ties.
-- payload_json must never contain secrets or large logs.
CREATE TABLE app_events (
    id           TEXT PRIMARY KEY,   -- ULID (time-sortable; the page cursor)
    owner_token  TEXT NOT NULL,      -- tenant boundary
    app_id       TEXT,
    sandbox_id   TEXT,
    task_id      TEXT,
    snapshot_id  TEXT,
    type         TEXT NOT NULL,      -- stable, machine-readable (e.g. task.failed)
    severity     TEXT NOT NULL,      -- info | warning | error
    message      TEXT NOT NULL,      -- human-readable; not for programmatic logic
    payload_json TEXT,               -- valid JSON when present; no secrets / large logs
    created_at   TEXT NOT NULL       -- RFC3339 UTC (display/export; ordering uses id)
);
-- Only the two indexes the read API actually uses (app timeline + task
-- timeline). Cursor column ends in `id` (the ULID) so each scoped feed
-- paginates newest-first with a stable cursor. Owner-only / sandbox-only /
-- type-only indexes were dropped: no endpoint queries by those, and each
-- extra index is pure write amplification on this append-only table. Re-add
-- if/when a query needs it (e.g. a tenant-wide feed or by-type export).
CREATE INDEX idx_app_events_app  ON app_events(owner_token, app_id, id);
CREATE INDEX idx_app_events_task ON app_events(owner_token, task_id, id);
