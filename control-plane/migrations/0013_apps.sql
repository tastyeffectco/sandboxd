-- Phase 1: durable "app" entities above sandboxes. An app owns the
-- user-facing concept (name/description/tags) and outlives the sandbox
-- that is its current running instance. external_* are optional
-- integration tags (an app may map 1:1 to an upstream project); the
-- canonical entity is the app. Additive + backwards-compatible: existing
-- sandboxes have a NULL app_id and the sandbox API is unchanged.
CREATE TABLE app (
    id                  TEXT PRIMARY KEY,         -- ULID
    owner_token         TEXT NOT NULL,            -- tenant (auth.Actor.Name)
    external_user_id    TEXT,                     -- optional integration tag
    external_project_id TEXT,                     -- optional integration tag
    name                TEXT NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    tags                TEXT NOT NULL DEFAULT '[]', -- JSON array of strings
    latest_snapshot_id  TEXT,                     -- recreate-from-snapshot (future)
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
);
CREATE INDEX idx_app_owner ON app(owner_token);

-- The link lives on the sandbox (single source of truth). "Current
-- sandbox" = the app's non-deleted sandbox row; on DELETE the row is
-- removed, so the app simply has no current sandbox.
ALTER TABLE sandbox ADD COLUMN app_id TEXT;
CREATE INDEX idx_sandbox_app_id ON sandbox(app_id);
