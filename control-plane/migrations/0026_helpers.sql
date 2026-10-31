-- Helpers: operator agents (e.g. Hermes) run as their own workloads. The
-- helper secret authenticates the helper to sandboxd's proxy (model access and
-- app tools); only its SHA-256 is stored. Channel tokens are encrypted with
-- the instance secrets key, as is the key sandboxd uses to call the helper's
-- own API (console chat).
CREATE TABLE helper (
    id TEXT PRIMARY KEY,
    owner_token TEXT NOT NULL,
    package TEXT NOT NULL,
    name TEXT NOT NULL,
    connection_id TEXT NOT NULL,
    model TEXT NOT NULL,
    secret_hash TEXT NOT NULL UNIQUE,
    api_key_ciphertext BLOB,
    api_key_nonce BLOB,
    telegram_ciphertext BLOB,
    telegram_nonce BLOB,
    telegram_allowed_users TEXT NOT NULL DEFAULT '',
    apps_day TEXT NOT NULL DEFAULT '',
    apps_today INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);
CREATE INDEX helper_owner ON helper(owner_token);
