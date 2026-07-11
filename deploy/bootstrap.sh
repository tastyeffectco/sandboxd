#!/usr/bin/env bash
# sandboxd — one-command VPS bootstrap. Run as root on a fresh Linux server:
#
#   curl -fsSL https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/deploy/bootstrap.sh | sudo bash
#
# Optional environment variables:
#   PREVIEW_DOMAIN=sandboxd.example.com   # serve previews at *.preview.<domain> (default: localhost/IP)
#   SANDBOXD_BRANCH=main                  # branch or tag to install
#
# It installs Docker (if missing), clones sandboxd to /opt/sandboxd, and runs the
# in-repo installer. install.sh is the source of truth — this only provisions the
# host and seeds .env before handing off. Idempotent: safe to re-run (it updates
# the checkout and preserves your .env).
set -euo pipefail

SANDBOXD_BRANCH="${SANDBOXD_BRANCH:-main}"
SRC="/opt/sandboxd"
REPO="https://github.com/tastyeffectco/sandboxd.git"

log() { printf '\033[36m›\033[0m %s\n' "$*"; }
die() { printf '\033[31m✗ %s\033[0m\n' "$*" >&2; exit 1; }

# .env key setter: replace in place, or append if the key is absent.
set_env() {
  local key="$1" val="$2"
  if grep -q "^${key}=" .env; then
    sed -i "s#^${key}=.*#${key}=${val}#" .env
  else
    printf '%s=%s\n' "$key" "$val" >>.env
  fi
}

[ "$(id -u)" -eq 0 ] || die "run as root — pipe into 'sudo bash' (curl … | sudo bash)"

# Prerequisites: curl + git (best-effort on Debian/Ubuntu; other distros usually
# ship them or the Docker script pulls curl in).
if command -v apt-get >/dev/null 2>&1; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq || true
  apt-get install -y -qq curl git ca-certificates openssl >/dev/null 2>&1 || true
fi
command -v curl >/dev/null 2>&1 || die "curl is required"
command -v git >/dev/null 2>&1 || die "git is required"

# Docker Engine + the Compose plugin, via the official convenience script.
if ! command -v docker >/dev/null 2>&1; then
  log "installing Docker…"
  curl -fsSL https://get.docker.com | sh
fi
systemctl enable --now docker >/dev/null 2>&1 || true
docker compose version >/dev/null 2>&1 || die "docker compose plugin missing — Docker install may have failed"

# Fetch or refresh the repo (shallow).
if [ -d "$SRC/.git" ]; then
  log "updating $SRC ($SANDBOXD_BRANCH)…"
  git -C "$SRC" fetch --depth 1 -q origin "$SANDBOXD_BRANCH"
  git -C "$SRC" reset --hard -q FETCH_HEAD
else
  log "cloning sandboxd → $SRC ($SANDBOXD_BRANCH)…"
  git clone --depth 1 --branch "$SANDBOXD_BRANCH" -q "$REPO" "$SRC"
fi
cd "$SRC"

# Seed .env with overrides BEFORE install.sh (which leaves an existing .env
# untouched). .env is gitignored, so the reset above never clobbers it.
[ -f .env ] || cp .env.example .env
if [ -n "${PREVIEW_DOMAIN:-}" ]; then
  log "PREVIEW_DOMAIN=$PREVIEW_DOMAIN"
  set_env PREVIEW_DOMAIN "$PREVIEW_DOMAIN"
fi

log "running installer…"
./install.sh

echo
log "sandboxd is up. Get your console login with:  cd $SRC && ./console-login.sh"
