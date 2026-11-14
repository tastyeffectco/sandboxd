-- Agent channels: the Telegram bot the agent answers as, and the owner's
-- WhatsApp number it is linked to (and answers only). The WhatsApp login
-- itself stays in the agent's state directory.
ALTER TABLE agent_instance ADD COLUMN telegram_bot TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_instance ADD COLUMN whatsapp_number TEXT NOT NULL DEFAULT '';
