-- 0019 — owner-scoped Git credentials (v0.4.2, Git A0).
--
-- Stores a private-repo access token (PAT) ENCRYPTED AT REST: only the
-- AES-GCM ciphertext + nonce are persisted (sealed by the API layer with
-- the same secrets.Cipher used for app config). The plaintext token is never
-- stored, returned by the API, or shown in the console.
--
-- Scope is owner_token (the API tenant; in auth-disabled/default mode this is
-- effectively instance-local, not true multi-user isolation). `host` is the
-- real field (e.g. github.com) for repo-URL matching when Git import lands in
-- a later v0.4.x release; there is no `provider` concept yet. These credentials
-- are NOT used by anything until Git import (A1).
CREATE TABLE git_credential (
  id           TEXT PRIMARY KEY,            -- ULID
  owner_token  TEXT NOT NULL,               -- tenant scope (= tenantToken)
  name         TEXT NOT NULL,               -- human label, unique per owner
  host         TEXT NOT NULL DEFAULT '',    -- e.g. github.com (repo matching, later)
  username     TEXT NOT NULL DEFAULT '',    -- optional (PAT often pairs with x-access-token)
  secret_enc   BLOB NOT NULL,               -- secrets.Cipher.Seal(token) ciphertext
  secret_nonce BLOB NOT NULL,               -- AES-GCM nonce
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL
);
CREATE INDEX        idx_git_credential_owner      ON git_credential(owner_token);
CREATE UNIQUE INDEX idx_git_credential_owner_name ON git_credential(owner_token, name);
