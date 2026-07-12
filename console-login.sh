#!/usr/bin/env bash
# Show the sandboxd web console URL + the API bootstrap key. Run it anytime:
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

# Prefer the stashed value; fall back to parsing SANDBOXD_API_TOKENS.
API_KEY=""
if [ -f .console-login ]; then
  API_KEY="$(sed -n 's/^api_key=//p' .console-login | head -1)"
fi
if [ -z "$API_KEY" ]; then
  API_KEY="$(printf '%s' "${SANDBOXD_API_TOKENS:-}" | tr ',' '\n' | sed -n 's/^default=//p' | head -1)"
fi

echo
echo "  ┌─ sandboxd web console ─────────────────────────────"
printf "  │  Open       %s\n" "$URL"
printf "  │  Login      create your password on first visit\n"
printf "  │             (change it later in Settings → Security)\n"
if [ -n "$API_KEY" ]; then
  printf "  │\n"
  printf "  │  API key    %s\n" "$API_KEY"
  printf "  │             Authorization: Bearer <key>   (for scripts / the engine)\n"
fi
echo "  └────────────────────────────────────────────────────"
echo
echo "  Rotate the API key: edit SANDBOXD_API_TOKENS in .env (SIGHUP reloads),"
echo "  or mint one in the console → Settings → Security."
echo
