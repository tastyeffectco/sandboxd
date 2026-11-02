-- Releases of a published app (ADR 0003, C2/C3).
--
-- A production sandbox runs one release at a time: the code of one commit,
-- unpacked into <workspace>/releases/<id> and built there. mode='production'
-- makes the container run web.start, stay on and restart by itself;
-- public_hosts are the extra hostnames it answers to (JSON array).
ALTER TABLE sandbox ADD COLUMN mode TEXT NOT NULL DEFAULT '';
ALTER TABLE sandbox ADD COLUMN public_hosts TEXT NOT NULL DEFAULT '[]';

CREATE TABLE app_release (
    id           TEXT PRIMARY KEY,
    app_id       TEXT NOT NULL,
    owner_token  TEXT NOT NULL,
    commit_sha   TEXT NOT NULL DEFAULT '',
    -- building | starting | active | previous | failed
    status       TEXT NOT NULL,
    message      TEXT NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL,
    finished_at  INTEGER,
    activated_at INTEGER
);
CREATE INDEX app_release_app ON app_release (app_id, created_at);
