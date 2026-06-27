-- 0021 — resolved preview web port per sandbox (v0.4.4, A1.5a).
--
-- The port the app's web process serves on, RESOLVED at create time from
-- (1) the cloned/imported workspace sandbox.yaml web.port, else (2) the selected
-- runtime preset's manifest web.port, else (3) 3000 (backward compatible). It
-- drives the Traefik preview router + the preview URL so a non-3000 app (e.g.
-- Astro on 4321) previews correctly. Nullable: existing sandboxes read as NULL
-- and are treated as 3000.
ALTER TABLE sandbox ADD COLUMN web_port INTEGER;
