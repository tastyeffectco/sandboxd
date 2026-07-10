#!/usr/bin/env bash
# Minimal background worker. Edit this file to change behavior — the runtime
# restarts the worker after each coding task (restart_after_task), so changes
# take effect without a manual restart.
while true; do
  echo "heartbeat $(date -u +%H:%M:%S)"
  sleep 5
done
