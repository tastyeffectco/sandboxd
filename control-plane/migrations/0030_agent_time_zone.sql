-- The person's time zone (IANA name, e.g. "Europe/Paris"), given to agents as
-- TZ so "every morning at 8" means 8 where the person lives. Set from the
-- console (it defaults to the browser's zone). Additive: '' means UTC.
ALTER TABLE instance_settings ADD COLUMN agent_time_zone TEXT NOT NULL DEFAULT '';
