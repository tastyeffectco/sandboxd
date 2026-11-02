-- Free-form key/value labels on an app (ADR 0003, C7), as a JSON object.
-- The desktop uses them to remember what an app is for (env=live,
-- source=<local app id>), so a reinstalled desktop can rebuild its
-- environments from the server. 0027 is reserved for agent channels.
ALTER TABLE app ADD COLUMN labels TEXT NOT NULL DEFAULT '{}';
