#!/usr/bin/env bash
#
# install-v04-ubuntu.sh — stand up a v0.4.0 (Phase 4: snapshots/restore/fork)
# TEST deployment on a FRESH, dedicated Ubuntu server.
#
#   THIS IS FOR ISOLATED v0.4.0 TESTING ONLY.
#   - Run it on a throwaway server, NOT on a production / shared host.
#   - It reuses the repo's normal docker-compose stack (traefik + sandboxd +
#     the optional `console` profile) — it does not invent a parallel system.
#     It only layers v0.4-test config via .env + a generated
#     docker-compose.override.yml, and gates the public console with basic auth.
#
# What it does (and prints before doing):
#   1. checks Ubuntu 22.04/24.04, 2+ vCPU, RAM/disk
#   2. installs Docker + Compose plugin if missing
#   3. fails if ports 80 or 443 are already in use
#   4. detects the public IPv4 (override with PUBLIC_IP=...)
#   5. writes .env (sslip.io preview domain, dedicated data dir, console on)
#   6. writes docker-compose.override.yml (access-log path + console basic auth)
#   7. disables the unauthenticated edge API router (api.yml) — API stays on loopback
#   8. builds the base image + control plane + console, starts the stack
#   9. prints the console URL + credentials and the preview URL pattern
#
# Teardown is printed at the end (and see docs/v0.4.0-test-runbook.md).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

bold() { printf '\033[1m%s\033[0m\n' "$*"; }
info() { printf '  \033[36m›\033[0m %s\n' "$*"; }
ok()   { printf '  \033[32m✓\033[0m %s\n' "$*"; }
warn() { printf '  \033[33m! %s\033[0m\n' "$*"; }
die()  { printf '  \033[31m✗ %s\033[0m\n' "$*" >&2; exit 1; }

# ── config (override via env) ────────────────────────────────────────
DATA_DIR="${SANDBOXD_DATA_DIR:-/var/lib/sandboxd-v04-test}"
LOG_DIR="${SANDBOXD_LOG_DIR:-$DATA_DIR/log}"
BASE_IMAGE="${SANDBOXD_IMAGE:-sandboxd-base:0.4.0-test}"
CONSOLE_USER="${CONSOLE_USER:-demo}"
CONSOLE_PASS="${CONSOLE_PASS:-}"   # generated if empty

# Shared-host-safe by default: bind an UNCOMMON host port so we never collide
# with an existing reverse proxy (Coolify / nginx / another Traefik) on 80/443.
#   - HTTP_PORT=18080  sandboxd's Traefik edge (host port). Set HTTP_PORT=80 for
#                      a dedicated host where sandboxd owns port 80.
#   - API_PORT=19090   control-plane API, published on LOOPBACK ONLY.
# TLS is deferred; a future TLS follow-up would use HTTPS_PORT=18443 (or 443 on
# a dedicated host). We do NOT bind or require 443 here.
HTTP_PORT="${HTTP_PORT:-18080}"
API_PORT="${API_PORT:-19090}"

bold "sandboxd v0.4.0 — isolated test installer"
cat <<EOF

  This will, on THIS host (SHARED-HOST-SAFE defaults):
    • install Docker + Compose if missing
    • require host port ${HTTP_PORT} to be free (does NOT touch 80/443 unless you set HTTP_PORT=80)
    • create data dir:        $DATA_DIR   (dedicated; not production)
    • write:                  .env, docker-compose.override.yml
    • disable edge API route: traefik/dynamic/api.yml -> .v04disabled
    • build images & start:   traefik + sandboxd + console (Docker Compose)
    • expose previews+console via Traefik on sslip.io, host port ${HTTP_PORT} (HTTP; TLS is a follow-up)
    • publish the control-plane API on 127.0.0.1:${API_PORT} (loopback only)

  Branch: $(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo '?') ($(git rev-parse --short HEAD 2>/dev/null || echo '?')) — expected: feat/snapshots-fork
  It does NOT touch production and does NOT merge or push anything.

EOF
if [ "${ASSUME_YES:-}" != "1" ]; then
  read -r -p "  Proceed? [y/N] " ans
  [ "$ans" = "y" ] || [ "$ans" = "Y" ] || die "aborted"
fi

# ── 1. OS / resource sanity ──────────────────────────────────────────
. /etc/os-release 2>/dev/null || true
if [ "${ID:-}" != "ubuntu" ]; then
  warn "this script targets Ubuntu; detected '${ID:-unknown}' — continuing, but unsupported"
elif [ "${VERSION_ID:-}" != "22.04" ] && [ "${VERSION_ID:-}" != "24.04" ]; then
  warn "tested on Ubuntu 22.04/24.04; detected ${VERSION_ID:-?} — continuing"
else
  ok "Ubuntu ${VERSION_ID}"
fi
CPUS="$(nproc 2>/dev/null || echo 1)"
[ "$CPUS" -ge 2 ] || warn "only ${CPUS} vCPU (2+ recommended)"
MEM_MB="$(awk '/MemTotal/ {print int($2/1024)}' /proc/meminfo 2>/dev/null || echo 0)"
[ "$MEM_MB" -ge 3500 ] || warn "only ${MEM_MB}MB RAM (4GB preferred)"
DISK_GB="$(df -BG --output=avail / 2>/dev/null | tail -1 | tr -dc '0-9' || echo 0)"
[ "${DISK_GB:-0}" -ge 30 ] || warn "only ${DISK_GB}GB free on / (30GB+ recommended)"

# ── 2. Docker + Compose ──────────────────────────────────────────────
if ! command -v docker >/dev/null 2>&1; then
  info "Docker not found — installing via get.docker.com"
  curl -fsSL https://get.docker.com | sh || die "Docker install failed"
fi
SUDO=""; docker info >/dev/null 2>&1 || SUDO="sudo"
$SUDO docker info >/dev/null 2>&1 || die "cannot reach the Docker daemon (is it running?)"
if $SUDO docker compose version >/dev/null 2>&1; then
  COMPOSE="$SUDO docker compose"
else
  die "Docker Compose plugin not found. Install it (apt-get install docker-compose-plugin)."
fi
ok "Docker $($SUDO docker version --format '{{.Server.Version}}' 2>/dev/null), Compose $($COMPOSE version --short 2>/dev/null)"

# ── 3. the chosen HTTP_PORT must be free ─────────────────────────────
# Shared-host-safe: we only need our single edge port. We do NOT check or
# require 443 (TLS is deferred). 80 is only relevant if you set HTTP_PORT=80.
port_busy() { $SUDO ss -ltnH "sport = :$1" 2>/dev/null | grep -q . ; }
if port_busy "$HTTP_PORT"; then
  die "host port ${HTTP_PORT} is already in use. Pick a free one and re-run:
       HTTP_PORT=18081 $0
       (Inspect with:  sudo ss -ltnp 'sport = :${HTTP_PORT}')"
fi
ok "host port ${HTTP_PORT} is free"

# ── 4. public IPv4 ───────────────────────────────────────────────────
PUBLIC_IP="${PUBLIC_IP:-}"
if [ -z "$PUBLIC_IP" ]; then
  for u in "https://api.ipify.org" "https://ifconfig.me/ip" "https://checkip.amazonaws.com"; do
    PUBLIC_IP="$(curl -fsS -m 5 "$u" 2>/dev/null | tr -dc '0-9.' || true)"
    [ -n "$PUBLIC_IP" ] && break
  done
fi
[ -z "$PUBLIC_IP" ] && PUBLIC_IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
echo "$PUBLIC_IP" | grep -qE '^[0-9]+(\.[0-9]+){3}$' || die "could not detect a public IPv4. Re-run with PUBLIC_IP=<your.server.ip>"
PREVIEW_DOMAIN="${PUBLIC_IP}.sslip.io"
ok "public IP $PUBLIC_IP  ->  preview domain $PREVIEW_DOMAIN (sslip.io, no DNS setup needed)"

# ── 5. data dir ──────────────────────────────────────────────────────
$SUDO mkdir -p "$DATA_DIR" "$LOG_DIR"
$SUDO chmod 0777 "$LOG_DIR"   # Traefik (host userns) writes the access log here
ok "data dir ready: $DATA_DIR"

# ── 6. console basic-auth credentials ────────────────────────────────
[ -z "$CONSOLE_PASS" ] && CONSOLE_PASS="$(openssl rand -hex 8)"
# htpasswd (apr1) hash for the Traefik basicAuth middleware. Written into a
# Traefik FILE-provider config, so the literal '$' need no compose escaping.
HASH="$(openssl passwd -apr1 "$CONSOLE_PASS")"

# ── 7. write .env ────────────────────────────────────────────────────
cat > .env <<EOF
# Generated by scripts/dev/install-v04-ubuntu.sh — v0.4.0 ISOLATED TEST.
# Not production. Safe to delete with the teardown steps.
PREVIEW_DOMAIN=${PREVIEW_DOMAIN}
PREVIEW_ENTRYPOINT=web
PREVIEW_TLS=false
# Uncommon host port so we don't collide with an existing 80/443 proxy on a
# shared host. Traefik still listens on :80 INSIDE its container; this only
# changes the host-side publish. Set to 80 for a dedicated host.
HTTP_PORT=${HTTP_PORT}

SANDBOXD_IMAGE=${BASE_IMAGE}
SANDBOXD_NETWORK=sandboxd_net
SANDBOXD_DATA_DIR=${DATA_DIR}
SANDBOXD_LOG_DIR=${LOG_DIR}

# API published on loopback only; the console proxies /v1 internally. The
# public edge API router (traefik/dynamic/api.yml) is disabled by this script,
# so the control-plane API is not reachable unauthenticated from the internet.
SANDBOXD_API_BIND=127.0.0.1:${API_PORT}
SANDBOXD_API_AUTH_DISABLED=true
SANDBOXD_API_TOKENS=

# Auto-generates a 0600 keyfile under the data dir.
SANDBOXD_SECRETS_KEY=

SANDBOXD_SET_MEMORY_HIGH=false
SANDBOXD_IDLE_THRESHOLD_SECONDS=2100
EOF
ok "wrote .env"

# ── 8. Traefik basic-auth middleware (file provider) ─────────────────
cat > traefik/dynamic/v04-auth.yml <<EOF
# Generated by install-v04-ubuntu.sh — gates the public console with HTTP
# basic auth. Remove this file (and the override) at teardown.
http:
  middlewares:
    v04-auth:
      basicAuth:
        users:
          - "${CONSOLE_USER}:${HASH}"
EOF
ok "wrote traefik/dynamic/v04-auth.yml (console basic auth)"

# ── 9. compose override: access-log path + console auth middleware ───
cat > docker-compose.override.yml <<'EOF'
# Generated by install-v04-ubuntu.sh — v0.4.0 test overrides layered on
# docker-compose.yml. Remove at teardown.
services:
  traefik:
    # Traefik's access-log path is hard-coded in traefik/traefik.yml to the
    # default data dir; point it at our dedicated log dir so the control
    # plane's activity tailer reads the right file.
    command:
      - "--configfile=/etc/traefik/traefik.yml"
      - "--accesslog.filepath=${SANDBOXD_LOG_DIR}/traefik-access.log"
  console:
    # Re-declare the full label set (compose may replace the list) and add the
    # basic-auth middleware so the public console requires the demo login.
    labels:
      - "sandboxd.managed=true"
      - "traefik.enable=true"
      - "traefik.http.routers.console.rule=Host(`console.${PREVIEW_DOMAIN}`)"
      - "traefik.http.routers.console.entrypoints=web"
      - "traefik.http.routers.console.middlewares=v04-auth@file"
      - "traefik.http.services.console.loadbalancer.server.port=80"
EOF
ok "wrote docker-compose.override.yml"

# ── 10. disable the unauthenticated edge API router ──────────────────
if [ -f traefik/dynamic/api.yml ]; then
  mv traefik/dynamic/api.yml traefik/dynamic/api.yml.v04disabled
  ok "disabled traefik/dynamic/api.yml (edge API router) — API stays on loopback"
fi

# ── 11. build + start ────────────────────────────────────────────────
bold "Building the sandbox base image (one-time, a few minutes)…"
DOCKER="$SUDO docker" SANDBOXD_IMAGE="$BASE_IMAGE" bash image/build.sh "${BASE_IMAGE##*:}"
ok "base image: $BASE_IMAGE"

bold "Building the control plane + console and starting the stack…"
$COMPOSE --profile console build
$COMPOSE --profile console up -d
ok "stack is up"

# ── 12. summary ──────────────────────────────────────────────────────
# Include :HTTP_PORT in URLs unless it's the bare 80 (dedicated-host mode).
PORTSUFFIX=""; [ "$HTTP_PORT" != "80" ] && PORTSUFFIX=":${HTTP_PORT}"
echo
bold "v0.4.0 test stack is running 🎉"
cat <<EOF

  Console  : http://console.${PREVIEW_DOMAIN}${PORTSUFFIX}/
             login: ${CONSOLE_USER} / ${CONSOLE_PASS}
  Previews : http://s-<sandbox-id>-<port>.preview.${PREVIEW_DOMAIN}${PORTSUFFIX}/   (public, no login)
  API      : http://127.0.0.1:${API_PORT}  (loopback only; not exposed to the internet)

  sslip.io resolves any *.${PUBLIC_IP}.sslip.io to ${PUBLIC_IP}, so no DNS setup is needed.
  Mode: $( [ "$HTTP_PORT" = "80" ] && echo "dedicated-host (sandboxd owns port 80)" || echo "shared-host (edge on :${HTTP_PORT}; safe next to Coolify/nginx/another Traefik)" ).
  HTTP only; TLS is an optional follow-up — see the runbook.

  If this host already runs a reverse proxy on 80/443, either open ${HTTP_PORT} in the
  firewall and use the URLs above, OR point that proxy's wildcard preview host and
  console host at http://127.0.0.1:${HTTP_PORT} (see docs/v0.4.0-test-runbook.md).

  Logs     : $COMPOSE logs -f sandboxd
  Status   : $COMPOSE --profile console ps

  Test checklist, modes & TLS notes: docs/v0.4.0-test-runbook.md

  TEARDOWN (removes the test stack + its data + the generated files):
    $COMPOSE --profile console down
    $SUDO rm -rf ${DATA_DIR}
    rm -f docker-compose.override.yml traefik/dynamic/v04-auth.yml .env
    [ -f traefik/dynamic/api.yml.v04disabled ] && mv traefik/dynamic/api.yml.v04disabled traefik/dynamic/api.yml
EOF
