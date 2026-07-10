-- App-scoped config and secrets, owned by the control plane (not Docker
-- env, workspace files, or task logs). Sensitive values are stored as
-- AES-256-GCM ciphertext + nonce and are write-only over the API;
-- non-sensitive config may keep a plaintext value. access_policy governs
-- who may later request the value through the broker; default is the
-- safest: control_plane_only (never leaves sandboxd).
CREATE TABLE app_config (
    id               TEXT PRIMARY KEY,            -- ULID
    app_id           TEXT NOT NULL,
    key              TEXT NOT NULL,
    value_ciphertext BLOB,                        -- set when sensitive
    value_nonce      BLOB,                        -- set when sensitive
    value_plaintext  TEXT,                        -- set when not sensitive
    sensitive        INTEGER NOT NULL DEFAULT 0,
    access_policy    TEXT NOT NULL DEFAULT 'control_plane_only',
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL,
    UNIQUE (app_id, key)
);
CREATE INDEX idx_app_config_app ON app_config(app_id);
