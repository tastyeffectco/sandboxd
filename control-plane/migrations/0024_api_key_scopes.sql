-- Additive: optional per-key scopes, comma-separated ('' = full access, the
-- behaviour of every key created before this migration). See internal/auth/scopes.go.
ALTER TABLE api_key ADD COLUMN scopes TEXT NOT NULL DEFAULT '';
