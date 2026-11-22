-- The AI every new chat starts with, chosen in Settings → AI: which coding
-- agent builds, and with which connection and model. JSON
-- {"agent","provider","model"}; '' means the built-in free AI.
ALTER TABLE instance_settings ADD COLUMN agent_default_ai TEXT NOT NULL DEFAULT '';
