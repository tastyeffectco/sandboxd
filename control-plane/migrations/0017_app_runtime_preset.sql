-- Phase 7C-1: remember the runtime preset chosen for an app (react-vite,
-- nextjs, node-express, fastapi, worker). Nullable: apps created before this,
-- or without a preset, keep the existing default behavior. The preset's
-- sandbox.yaml is applied by runtimed on first boot; this column is just the
-- app-level default used when a sandbox create omits an explicit preset.
ALTER TABLE app ADD COLUMN runtime_preset TEXT;
