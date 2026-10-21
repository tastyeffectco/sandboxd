#!/usr/bin/env bash
# Exercise installer policy in a disposable fixture; no Docker, downloads,
# services, real .env changes, HOME overrides or host data-directory changes.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d "${TMPDIR:-/tmp}/sandboxd-instest.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/bin" "$TMP/repo/control-plane" "$TMP/repo/scripts"
cp "$ROOT/install.sh" "$TMP/repo/install.sh"
touch "$TMP/repo/docker-compose.yml" "$TMP/repo/console-login.sh" "$TMP/repo/upgrade.sh"
cat > "$TMP/bin/docker" <<'STUB'
#!/bin/sh
case "$1" in info) exit 0 ;; version) echo 27.0-stub ;; esac
if [ "$1" = compose ] && [ "$2" = version ]; then echo 2.0-stub; fi
exit 0
STUB
# The Mac path rewrite must be tested as a string without creating directories
# in the real user's home. All filesystem setup for the fixture is already done.
cat > "$TMP/bin/mkdir" <<'STUB'
#!/bin/sh
exit 0
STUB
cp "$TMP/bin/mkdir" "$TMP/bin/chmod"
chmod +x "$TMP/bin/"*
cat > "$TMP/repo/scripts/install-images.sh" <<'STUB'
acquire_images() {
  SANDBOXD_IMAGE=base:test
  SANDBOXD_CONTROL_PLANE_IMAGE=api:test
  SANDBOXD_CONSOLE_IMAGE=console:test
}
STUB
cat > "$TMP/repo/scripts/install-desktop.sh" <<'STUB'
printf 'called\n' >> "$DESKTOP_LOG"
STUB
export DESKTOP_LOG="$TMP/desktop.log"
run_case() {
  desktop_mode="$1"; shift
  rm -f "$TMP/repo/.env" "$DESKTOP_LOG"
  printf 'SANDBOXD_DATA_DIR=%s/data\nSANDBOXD_LOG_DIR=%s/log\nSANDBOXD_API_TOKENS=default=test\n' "$TMP" "$TMP" > "$TMP/repo/.env.example"
  (cd "$TMP/repo" && PATH="$TMP/bin:$PATH" SANDBOXD_TELEMETRY=off SANDBOXD_NO_SPONSOR=1 SANDBOXD_DESKTOP="$desktop_mode" bash install.sh "$@") > "$TMP/output"
}
run_case 1
case "$(uname -s)" in
  Darwin)
    [ "$(sed -n 's/^SANDBOXD_DATA_DIR=//p' "$TMP/repo/.env")" = "$HOME/.sandboxd/data" ]
    [ "$(cat "$DESKTOP_LOG")" = called ] ;;
  *) [ ! -e "$DESKTOP_LOG" ] ;;
esac
run_case 1 --no-desktop
[ ! -e "$DESKTOP_LOG" ]
run_case 0
[ ! -e "$DESKTOP_LOG" ]
printf 'PASS: %s installer platform defaults and desktop opt-outs (isolated fixture)\n' "$(uname -s)"
