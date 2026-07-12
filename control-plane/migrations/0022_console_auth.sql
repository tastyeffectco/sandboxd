-- Console login + API keys: the control plane becomes the auth authority.
--   console_auth    : single-row (id=1) bcrypt hash of the console password.
--   console_session : opaque HttpOnly session cookies (sha256 of the value).
--   api_key         : programmatic keys; each shown once, stored as a sha256.
-- All resolve to the single shared tenant "default" (store.DefaultTenant) in
-- the default single-tenant deployment; per-key isolation is a later toggle.

CREATE TABLE console_auth (
  id            INTEGER PRIMARY KEY CHECK (id = 1),
  password_hash TEXT NOT NULL,
  updated_at    INTEGER NOT NULL
);

CREATE TABLE console_session (
  token_hash   TEXT PRIMARY KEY,     -- sha256 hex of the opaque cookie value
  owner_token  TEXT NOT NULL,        -- tenant (= store.DefaultTenant today)
  created_at   INTEGER NOT NULL,
  last_used_at INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL
);
CREATE INDEX console_session_expires_idx ON console_session(expires_at);

CREATE TABLE api_key (
  id           TEXT PRIMARY KEY,     -- ULID
  name         TEXT NOT NULL,        -- display label
  key_hash     TEXT NOT NULL UNIQUE, -- sha256 hex of the presented key
  prefix       TEXT NOT NULL,        -- e.g. "sk_AbC12…" for the UI
  created_at   INTEGER NOT NULL,
  last_used_at INTEGER
);
CREATE UNIQUE INDEX api_key_name_idx ON api_key(name);
