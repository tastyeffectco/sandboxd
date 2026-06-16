-- Persist a task's timeout_s so the boot-time reconciler can re-attach
-- a watcher with a streaming window that outlives the task (a long
-- timeout_s would otherwise revert to the default after a restart).
-- 0 = the runtimed default (10m).
ALTER TABLE task ADD COLUMN timeout_s INTEGER NOT NULL DEFAULT 0;
