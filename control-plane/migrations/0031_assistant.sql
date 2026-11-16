-- The sandboxd agent: one main assistant per person, the coding agent of a
-- hidden project that can use sandboxd's tools (apps, agents, memories).
-- secret_hash authenticates its tools endpoint; the secret itself lives only
-- in that project's MCP settings.
CREATE TABLE IF NOT EXISTS assistant (
    owner_token TEXT PRIMARY KEY,
    app_id      TEXT NOT NULL,
    secret_hash TEXT NOT NULL,
    created_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS assistant_secret ON assistant(secret_hash);
