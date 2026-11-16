-- Files the person attached to a message, as a JSON array of
-- {id, name, type, size}. The files live in the agent's own uploads folder.
ALTER TABLE agent_message ADD COLUMN attachments TEXT NOT NULL DEFAULT '[]';
