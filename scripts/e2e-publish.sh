#!/usr/bin/env bash
# End-to-end test of publishing (ADR 0003) against a real Docker daemon and
# the real base image: build an app, save it, take its bundle, publish it to
# another app on the same engine (standing in for a server), then publish a
# new version, a broken one (rolled back), go back, and survive a crash.
# Data under data/ must survive every switch.
#
# Inputs (env): SANDBOXD_IMAGE (built base image), SANDBOXD_BIN (sandboxd binary).
# Needs root: sandboxd hands release files to the sandbox user.
#
#   sudo -E SANDBOXD_IMAGE=sandboxd-base:ci SANDBOXD_BIN=/tmp/sandboxd bash scripts/e2e-publish.sh
set -uo pipefail

IMG="${SANDBOXD_IMAGE:?set SANDBOXD_IMAGE}"
BIN="${SANDBOXD_BIN:?set SANDBOXD_BIN}"
HERE="$(cd "$(dirname "$0")/.." && pwd)"
DD="$(mktemp -d)"
NET="sbxpub-$$"
API="http://127.0.0.1:9192"
SBX_PID=""
declare -a SBXS=()

log() { echo "── $*"; }
fail() {
  echo "❌ FAIL: $*"
  echo "--- sandboxd log (tail)"; tail -40 "$DD/sandboxd.log" 2>/dev/null
  exit 1
}

cleanup() {
  for id in "${SBXS[@]:-}"; do
    [ -n "$id" ] && curl -s -m60 -o /dev/null -XPOST "$API/sandbox/$id/purge" >/dev/null 2>&1 || true
  done
  [ -n "$SBX_PID" ] && kill "$SBX_PID" >/dev/null 2>&1 || true
  docker ps -aq --filter label=sandboxd.release | xargs -r docker rm -f >/dev/null 2>&1 || true
  docker network rm "$NET" >/dev/null 2>&1 || true
  rm -rf "$DD" || true
}
trap cleanup EXIT

j() { python3 -c "import sys,json; d=json.load(sys.stdin); print(eval(sys.argv[1]))" "$1"; }
api() { curl -s -m120 "$@"; }
ipof() { docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "s-$1" 2>/dev/null; }

mkdir -p "$DD/log" "$DD/library"
docker network create "$NET" >/dev/null
SANDBOXD_ADDR=127.0.0.1:9192 SANDBOXD_IMAGE="$IMG" SANDBOXD_DATA_DIR="$DD" \
  SANDBOXD_LOG_DIR="$DD/log" SANDBOXD_NETWORK="$NET" SANDBOXD_USERNS=host \
  SANDBOXD_LIBRARY_DIR="$DD/library" SANDBOXD_MIGRATIONS="$HERE/control-plane/migrations" \
  SANDBOXD_DB="$DD/sandboxd.db" SANDBOXD_IDLE_THRESHOLD_SECONDS=3600 \
  SANDBOXD_API_AUTH_DISABLED=true SANDBOXD_TELEMETRY=off \
  PREVIEW_DOMAIN=e2e.test SANDBOXD_RATELIMIT_MIDDLEWARE=sandbox-ratelimit@file \
  "$BIN" >"$DD/sandboxd.log" 2>&1 &
SBX_PID=$!
for _ in $(seq 1 60); do curl -s -m2 "$API/healthz" 2>/dev/null | grep -q ok && break; sleep 0.5; done
curl -s -m3 "$API/healthz" | grep -q ok || fail "sandboxd never became healthy"
log "sandboxd healthy"

# 1. The app you build: a tiny Node server with a visit counter under data/.
DEV_APP="$(api -XPOST "$API/v1/apps" -H 'content-type: application/json' -d '{"name":"Shop"}' | j 'd["id"]')"
[ -n "$DEV_APP" ] || fail "create dev app"
DEV="$(api -XPOST "$API/v1/apps/$DEV_APP/sandbox" -H 'content-type: application/json' -d '{"template":"blank"}' | j 'd["id"]')"
[ -n "$DEV" ] || fail "create dev sandbox"
SBXS+=("$DEV")

put() { # path content
  code="$(curl -s -m30 -o /dev/null -w '%{http_code}' -XPUT "$API/v1/sandboxes/$DEV/files?path=$1" --data-binary "$2")"
  [ "${code:0:1}" = 2 ] || fail "write $1: HTTP $code"
}
save() { # message
  docker exec -u sandbox "s-$DEV" bash -lc "cd ~/workspace/app && { [ -d .git ] || git init -q; } && git add -A && git -c user.email=e2e@example.com -c user.name=e2e commit -qm '$1'" \
    || fail "commit $1"
}
publish() { # -> release id; waits until it is no longer building/starting
  hdr="$DD/hdr"; tgz="$DD/bundle.tgz"
  code="$(curl -s -m60 -D "$hdr" -o "$tgz" -w '%{http_code}' "$API/v1/apps/$DEV_APP/bundle")"
  [ "$code" = 200 ] || fail "bundle: HTTP $code"
  commit="$(grep -i '^x-sandboxd-commit:' "$hdr" | tr -d '\r' | awk '{print $2}')"
  rid="$(api -XPOST "$API/v1/apps/$LIVE_APP/releases?commit=$commit" -H 'content-type: application/gzip' --data-binary "@$tgz" | j 'd["id"]')"
  [ -n "$rid" ] || fail "publish returned no release"
  for _ in $(seq 1 200); do
    st="$(api "$API/v1/apps/$LIVE_APP/releases/$rid" | j 'd["status"]')"
    case "$st" in building|starting) sleep 2 ;; *) break ;; esac
  done
  echo "$rid"
}
status_of() { api "$API/v1/apps/$LIVE_APP/releases/$1" | j 'd["status"]'; }
page() { curl -s -m5 "http://$(ipof "$PROD"):3000/"; }
wait_page() { # expected-substring
  for _ in $(seq 1 60); do page | grep -q "$1" && return 0; sleep 1; done
  echo "got: $(page)"; return 1
}

put sandbox.yaml 'version: 1
web:
  command: "node server.js"
  start: "node server.js"
  port: 3000
build:
  command: ""
  release: "echo release-built > built.txt"
data: [data]
'
put server.js "const fs = require('fs'), http = require('http');
fs.mkdirSync('data', { recursive: true });
http.createServer((req, res) => {
  const f = 'data/visits.txt';
  const n = (parseInt(fs.existsSync(f) ? fs.readFileSync(f, 'utf8') : '0') || 0) + 1;
  fs.writeFileSync(f, String(n));
  res.end(fs.readFileSync('version.txt', 'utf8').trim() + ' visits=' + n + ' env=' + process.env.NODE_ENV);
}).listen(3000, '0.0.0.0');"
put version.txt v1
put BRAIN.md "agent notes, never published"
save v1

# 2. The "server" side: a separate app labelled as the live copy.
LIVE_APP="$(api -XPOST "$API/v1/apps" -H 'content-type: application/json' \
  -d "{\"name\":\"Shop\",\"labels\":{\"env\":\"live\",\"source\":\"$DEV_APP\"}}" | j 'd["id"]')"
[ -n "$LIVE_APP" ] || fail "create live app"
[ "$(api "$API/v1/apps?label=env=live" | j 'len(d["apps"])')" = 1 ] || fail "label filter"

log "publish v1"
R1="$(publish)"
[ "$(status_of "$R1")" = active ] || fail "v1 not active: $(api "$API/v1/apps/$LIVE_APP/releases/$R1")"
PROD="$(api "$API/v1/apps/$LIVE_APP/releases" | j 'd["sandbox_id"]')"
URL="$(api "$API/v1/apps/$LIVE_APP/releases" | j 'd["url"]')"
SBXS+=("$PROD")
[ "$URL" = "http://shop.e2e.test" ] || fail "url: $URL"
wait_page "v1 visits=" || fail "v1 not served"
page | grep -q "env=production" || fail "NODE_ENV is not production: $(page)"
page >/dev/null
docker exec "s-$PROD" cat /home/sandbox/workspace/app/built.txt | grep -q release-built || fail "build.release did not run"
docker exec "s-$PROD" test -e /home/sandbox/workspace/app/BRAIN.md && fail "BRAIN.md was published"
labels="$(docker inspect -f '{{json .Config.Labels}}' "s-$PROD")"
echo "$labels" | grep -q 'Host(`shop.e2e.test`)' || fail "no public router: $labels"
echo "$labels" | grep -q 'sandbox-ratelimit@file' || fail "no rate limit: $labels"
[ "$(docker inspect -f '{{.HostConfig.RestartPolicy.Name}}' "s-$PROD")" = unless-stopped ] || fail "restart policy"
code="$(curl -s -o /dev/null -w '%{http_code}' -XPOST "$API/v1/sandboxes/$PROD/tasks" -H 'content-type: application/json' -d '{"prompt":"change it"}')"
[ "${code:0:1}" = 2 ] && fail "a coding task was accepted on the published app"
log "v1 live ✓ (production start, data, labels, restart policy, no tasks)"

log "publish v2"
put version.txt v2; save v2
R2="$(publish)"
[ "$(status_of "$R2")" = active ] || fail "v2 not active"
wait_page "v2 visits=" || fail "v2 not served"
n="$(page | sed -E 's/.*visits=([0-9]+).*/\1/')"
[ "$n" -ge 4 ] || fail "visits did not survive the new version (visits=$n)"
log "v2 live, data kept ✓"

log "publish a broken v3 (must roll back)"
put server.js "process.exit(1)"; save v3
R3="$(publish)"
[ "$(status_of "$R3")" = failed ] || fail "broken v3: $(status_of "$R3")"
api "$API/v1/apps/$LIVE_APP/releases/$R3" | j 'd["message"]' | grep -q "put back" || fail "rollback message"
wait_page "v2 visits=" || fail "v2 not back after the failed v3"
log "rolled back ✓"

log "go back to v1"
api -XPOST "$API/v1/apps/$LIVE_APP/releases/$R1/activate" >/dev/null
for _ in $(seq 1 120); do [ "$(status_of "$R1")" = active ] && break; sleep 2; done
[ "$(status_of "$R1")" = active ] || fail "go back"
wait_page "v1 visits=" || fail "v1 not served after going back"
log "went back ✓"

log "a crash is restarted by Docker"
# Kill the container's main process from the host (a real crash; `docker
# kill`/`stop` count as stopping it on purpose).
kill -9 "$(docker inspect -f '{{.State.Pid}}' "s-$PROD")"
sleep 2
wait_page "v1 visits=" || fail "not restarted after a crash"
log "restarted ✓"

echo "✅ publish e2e passed"
