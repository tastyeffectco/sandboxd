-- Phase 4 (v0.4.0): link each snapshot to the app it was captured from.
-- The snapshot row already records source_sandbox_id, but an app's sandbox
-- is ephemeral (it is deleted/replaced on restore and purged on teardown),
-- so per-app snapshot history must hang off the durable app id, not the
-- sandbox. source_app_id is nullable: snapshots taken before this migration,
-- or of a sandbox with no app, simply have no app linkage.
ALTER TABLE snapshot ADD COLUMN source_app_id TEXT;
CREATE INDEX idx_snapshot_app ON snapshot(source_app_id);
