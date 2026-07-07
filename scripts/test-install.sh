#!/usr/bin/env bash
#
# Smoke-test install.sh's platform logic WITHOUT a Docker daemon.
#
# Docker cannot run macOS, so we can't spin up a Mac in a Linux container. This
# script instead runs the installer's shell logic with `docker`/`compose`
# stubbed, and is executed by CI on BOTH ubuntu-latest AND macos-latest (real
# Apple hardware) — catching OS/shell portability regressions (bash 3.2 on
# macOS, BSD vs GNU sed/mktemp, the Darwin data-dir rewrite) on the real OS.
#
# It asserts the OS-appropriate data dir ends up in the generated .env.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/sandboxd-instest.XXXXXX")"
trap 'rm -rf "$tmp"; rm -f "$ROOT/.env"' EXIT

# ── stub docker/compose so the installer never touches a real daemon ──
mkdir -p "$tmp/bin"
cat > "$tmp/bin/docker" <<'STUB'
#!/bin/sh
if [ "$1" = "compose" ]; then
  shift
  [ "$1" = "version" ] && { [ "$2" = "--short" ] && echo "2.0-stub"; exit 0; }
  exit 0
fi
case "$1" in
  info)    exit 0 ;;
  version) echo "27.0-stub" ;;
  image)   echo "400" ;;   # `image inspect --format '{{.Size}}'`
  *)       : ;;
esac
exit 0
STUB
chmod +x "$tmp/bin/docker"

# Throwaway HOME so the macOS branch writes under it, not the runner's real home.
export HOME="$tmp/home"; mkdir -p "$HOME"
rm -f "$ROOT/.env"   # force a fresh .env so the platform branch runs

( cd "$ROOT" && PATH="$tmp/bin:$PATH" bash install.sh ) >/dev/null

# ── assert the OS-appropriate data dir was written ───────────────────
data="$(grep '^SANDBOXD_DATA_DIR=' "$ROOT/.env" | cut -d= -f2-)"
case "$(uname -s)" in
  Darwin) expect="$HOME/.sandboxd/data" ;;
  *)      expect="/var/lib/sandboxed" ;;
esac
if [ "$data" != "$expect" ]; then
  echo "FAIL: SANDBOXD_DATA_DIR=$data (expected $expect on $(uname -s))" >&2
  exit 1
fi
echo "OK: install.sh platform logic on $(uname -s) — data dir = $data"
