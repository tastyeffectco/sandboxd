#!/usr/bin/env bash
# Show the sandboxd web console URL + login. Run it anytime:
#     ./console-login.sh
set -eu
cd "$(dirname "$0")"

if [ ! -f .env ]; then
  echo "No .env here — run ./install.sh first." >&2
  exit 1
fi
# shellcheck disable=SC1091
set -a; . ./.env; set +a

DOMAIN="${PREVIEW_DOMAIN:-localhost}"
PORT="${HTTP_PORT:-80}"
SUFFIX=""; [ "$PORT" != "80" ] && SUFFIX=":$PORT"
URL="http://console.${DOMAIN}${SUFFIX}"

echo
echo "  ┌─ sandboxd web console ─────────────────────────────"
printf "  │  Open      %s\n" "$URL"

if [ -f .console-login ]; then
  printf "  │  Username  %s\n" "$(sed -n '1p' .console-login)"
  printf "  │  Password  %s\n" "$(sed -n '2p' .console-login)"
else
  CBA="${CONSOLE_BASIC_AUTH:-}"
  if [ -z "$CBA" ] || [ "${CBA#locked:}" != "$CBA" ]; then
    printf "  │  The console isn't set up yet. Run ./install.sh (it's on by default).\n"
  else
    printf "  │  Username  %s\n" "${CBA%%:*}"
    printf "  │  Password  (you set this — see .env → CONSOLE_BASIC_AUTH)\n"
  fi
fi

echo "  └────────────────────────────────────────────────────"
echo
echo "  Change it anytime:  set a new CONSOLE_PASS and re-run ./install.sh"
echo
