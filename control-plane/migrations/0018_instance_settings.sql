-- Phase 8B: runtime-editable lifecycle tunables (idle reaper + keepalive), set
-- from the console via PATCH /v1/settings. A single row (id = 1) overlays the
-- env defaults at startup so edits survive restart. Deliberately ONLY safe
-- operational timers — never secrets, auth, networking, or egress (those stay
-- env/file-only). See internal/api/v1_settings.go.
CREATE TABLE instance_settings (
    id                     INTEGER PRIMARY KEY CHECK (id = 1), -- singleton
    idle_reap_enabled      INTEGER NOT NULL,                   -- 0/1
    idle_threshold_seconds INTEGER NOT NULL,
    keepalive_max_seconds  INTEGER NOT NULL,
    updated_at             INTEGER NOT NULL                    -- unix seconds
);
