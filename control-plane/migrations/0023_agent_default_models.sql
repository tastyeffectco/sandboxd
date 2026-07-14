-- Per-agent default model id, set from the console (Settings → AI Agents) and
-- used when a task doesn't specify a model. The user supplies the real provider
-- model id (e.g. opencode "glm-5", claude "sonnet"); sandboxd stores it opaquely
-- and passes it through as the agent CLI's --model. JSON object {agent: model_id}.
-- Additive: existing rows default to '{}'. Non-secret — safe to show any operator.
-- See internal/api/v1_tasks.go (model precedence) and v1_settings.go.
ALTER TABLE instance_settings ADD COLUMN agent_default_models TEXT NOT NULL DEFAULT '{}';
