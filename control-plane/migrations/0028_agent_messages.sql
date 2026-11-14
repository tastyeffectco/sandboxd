-- One conversation per agent, kept by sandboxd like a chat app: the window
-- can close and the reply keeps going; every channel lands in the same thread.
-- steps is a JSON array of the tools the agent ran for that message.
CREATE TABLE agent_message (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    thinking TEXT NOT NULL DEFAULT '',
    steps TEXT NOT NULL DEFAULT '[]',
    state TEXT NOT NULL DEFAULT 'done',
    error TEXT NOT NULL DEFAULT '',
    channel TEXT NOT NULL DEFAULT 'app',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX agent_message_agent ON agent_message(agent_id, id);
