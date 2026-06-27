-- 0020 — per-app Git import metadata (v0.4.3, Git A1).
--
-- An app may be imported from a private HTTPS repo. We store ONLY tokenless
-- metadata here: the repo URL (no credentials), the branch, and the id of the
-- owner-scoped git_credential (0019) used to clone. The access token itself is
-- never stored on the app — it stays encrypted in git_credential and is
-- decrypted control-plane-side only at clone time. last_import_at records the
-- most recent successful clone. All nullable: a blank-from-preset app has none.
ALTER TABLE app ADD COLUMN git_repo_url       TEXT;
ALTER TABLE app ADD COLUMN git_branch         TEXT;
ALTER TABLE app ADD COLUMN git_credential_id  TEXT;
ALTER TABLE app ADD COLUMN last_import_at     INTEGER;
