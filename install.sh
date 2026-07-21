#!/usr/bin/env bash
#
# sandboxd — one-click installer (Linux, and macOS via Docker Desktop).
#
#   curl -fsSL https://raw.githubusercontent.com/tastyeffectco/sandboxd/main/install.sh | bash
#   # …or, from a clone:  ./install.sh
#
# Run standalone (curl | bash) it clones the repo first, then installs.
#
# Brings up the whole stack on a single host with nothing but Docker:
#   1. checks Docker + Compose are present
#   2. creates .env from .env.example (if missing)
#   3. builds the sandbox base image and the control-plane image
#   4. creates the data dir
#   5. `docker compose up -d`
#   6. prints how to create your first sandbox
#
# Idempotent: safe to re-run. It never touches anything outside the repo
# and the configured data dir.

set -euo pipefail

# ── pretty output ────────────────────────────────────────────────────
bold() { printf '\033[1m%s\033[0m\n' "$*"; }
info() { printf '  \033[36m›\033[0m %s\n' "$*"; }
ok()   { printf '  \033[32m✓\033[0m %s\n' "$*"; }
die()  { emit_fail "${2:-unexpected}" 1; printf '  \033[31m✗ %s\033[0m\n' "$1" >&2; exit 1; }

# ── telemetry: anonymous install_failed (enum-only, no PII) ──────────
# Emits ONE event when the install aborts, so we can see WHERE the quickstart
# breaks. Strict whitelist: step + error_class (both enums), exit code,
# os/arch/version, a bucketed duration. NEVER raw errors, paths, hostnames, or
# env values. Honors SANDBOXD_TELEMETRY=off; sends $ip empty.
SANDBOXD_TELEMETRY="${SANDBOXD_TELEMETRY:-on}"
STEP="start"
START_TS="$(date +%s 2>/dev/null || echo 0)"
_PH_HOST="https://us.i.posthog.com"
_PH_KEY="phc_vyQtLTZPBHwEBcY8mcfneP43xAFGLzFVic9DhQ7VGrqV"
err_class_for_step() {
  case "$STEP" in
    base_image_build|stack_build) [ "$1" = "137" ] && echo "build_oom" || echo "build_nonzero" ;;
    image_pull)    echo "pull_failed" ;;
    compose_up)    echo "compose_up_failed" ;;
    docker_check)  echo "docker_missing" ;;
    compose_check) echo "compose_missing" ;;
    *)             echo "unexpected" ;;
  esac
}
emit_fail() {
  [ "${SANDBOXD_TELEMETRY}" = "off" ] && return 0
  command -v curl >/dev/null 2>&1 || return 0
  local class="$1" rc="${2:-1}" now dur bucket arch os ver
  now="$(date +%s 2>/dev/null || echo 0)"; dur=$(( now - START_TS ))
  bucket="<1m"; [ "$dur" -ge 60 ] && bucket="1-5m"; [ "$dur" -ge 300 ] && bucket="5-15m"; [ "$dur" -ge 900 ] && bucket=">15m"
  arch="$(uname -m 2>/dev/null || echo unknown)"
  os="$(uname -s 2>/dev/null | tr 'A-Z' 'a-z' || echo unknown)"
  ver="$(git -C "${REPO_ROOT:-.}" describe --tags --always 2>/dev/null || echo unknown)"
  curl -sf -m 5 -X POST "${_PH_HOST}/i/v0/e/" -H 'content-type: application/json' \
    -d "{\"api_key\":\"${_PH_KEY}\",\"event\":\"install_failed\",\"distinct_id\":\"install-$$\",\"properties\":{\"step\":\"${STEP}\",\"error_class\":\"${class}\",\"exit_code\":${rc},\"os\":\"${os}\",\"arch\":\"${arch}\",\"version\":\"${ver}\",\"duration_bucket\":\"${bucket}\",\"\$ip\":\"\"}}" \
    >/dev/null 2>&1 || true
}
# Catch unhandled failures (build/up) that don't go through die(). Guarded
# commands inside if/|| are not trapped, so no spurious events on fallback.
trap 'rc=$?; emit_fail "$(err_class_for_step "$rc")" "$rc"' ERR

# ── install mode: prebuilt images (default) vs build from source ─────
# auto  — pull prebuilt GHCR images; fall back to building if unavailable.
# --build — always compile from source (first-class path for self-hosters).
# --pull  — require prebuilt images (fail if missing).
MODE="auto"
for _a in "$@"; do case "$_a" in --build) MODE=build ;; --pull) MODE=pull ;; esac; done

# ── bootstrap: fetch the repo when run standalone (curl … | bash) ────
# Piped from curl, this script has no repo around it. Detect that, clone
# sandboxd, and re-exec the in-repo installer. Overridable via env:
#   SANDBOXD_REPO_URL, SANDBOXD_REF (branch/tag), SANDBOXD_SRC (checkout dir).
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || echo "$PWD")"
if [ ! -f "$REPO_ROOT/docker-compose.yml" ] || [ ! -d "$REPO_ROOT/control-plane" ]; then
  REPO_URL="${SANDBOXD_REPO_URL:-https://github.com/tastyeffectco/sandboxd.git}"
  REF="${SANDBOXD_REF:-main}"
  SRC="${SANDBOXD_SRC:-$HOME/.sandboxd/src}"
  STEP="git_clone"
  command -v git >/dev/null 2>&1 || die "git is required to install sandboxd — install git and re-run." git_missing
  bold "sandboxd — fetching source"
  if [ -d "$SRC/.git" ]; then
    info "updating existing checkout at $SRC"
    git -C "$SRC" fetch --depth 1 -q origin "$REF" && git -C "$SRC" reset --hard -q FETCH_HEAD
  else
    info "cloning $REPO_URL ($REF) → $SRC"
    mkdir -p "$(dirname "$SRC")"
    git clone --depth 1 --branch "$REF" -q "$REPO_URL" "$SRC"
  fi
  ok "source ready"
  exec bash "$SRC/install.sh" "$@"
fi
cd "$REPO_ROOT"

# ── docker / sudo detection ──────────────────────────────────────────
# Use sudo for docker only if the current user can't reach the daemon.
STEP="docker_check"
DOCKER="docker"
if ! docker info >/dev/null 2>&1; then
  if sudo -n docker info >/dev/null 2>&1 || sudo docker info >/dev/null 2>&1; then
    DOCKER="sudo docker"
    info "using 'sudo docker' (current user can't reach the Docker daemon)"
  else
    die "Docker is not available. Install Docker Engine and ensure the daemon is running." docker_missing
  fi
fi

# Compose v2 (docker compose) preferred; fall back to docker-compose.
STEP="compose_check"
if $DOCKER compose version >/dev/null 2>&1; then
  COMPOSE="$DOCKER compose"
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE="docker-compose"
else
  die "Docker Compose not found. Install the Compose plugin (docker compose)." compose_missing
fi

bold "sandboxd — installer"
ok "Docker:  $($DOCKER version --format '{{.Server.Version}}' 2>/dev/null || echo present)"
ok "Compose: $($COMPOSE version --short 2>/dev/null || echo present)"

# ── .env ─────────────────────────────────────────────────────────────
if [ ! -f .env ]; then
  cp .env.example .env
  ok "created .env from .env.example"
  # macOS (Docker Desktop) shares $HOME by default but NOT /var/lib, so the
  # symmetric data-dir bind mount would fail there. Point it at $HOME instead.
  if [ "$(uname -s)" = "Darwin" ]; then
    MAC_DATA="$HOME/.sandboxd/data"
    # BSD/macOS mktemp needs an explicit template (GNU works without one).
    tmp="$(mktemp "${TMPDIR:-/tmp}/sandboxd-env.XXXXXX")"
    sed -e "s#^SANDBOXD_DATA_DIR=.*#SANDBOXD_DATA_DIR=$MAC_DATA#" \
        -e "s#^SANDBOXD_LOG_DIR=.*#SANDBOXD_LOG_DIR=$MAC_DATA/log#" .env > "$tmp" && mv "$tmp" .env
    info "macOS detected — data dir set to $MAC_DATA (Docker Desktop shares \$HOME)"
  fi
else
  info ".env already exists — leaving it untouched"
fi

# Load .env so we know the data dir / image tag to use below.
set -a; . ./.env; set +a
DATA_DIR="${SANDBOXD_DATA_DIR:-/var/lib/sandboxed}"
LOG_DIR="${SANDBOXD_LOG_DIR:-$DATA_DIR/log}"
# Image coordinates. Prebuilt images live at $REGISTRY/<name>:$IMAGE_TAG; a
# full base ref can be overridden via SANDBOXD_IMAGE (custom registry / local).
IMAGE_TAG="${SANDBOXD_IMAGE_TAG:-0.3.0}"
REGISTRY="${SANDBOXD_REGISTRY:-ghcr.io/tastyeffectco}"
BASE_IMAGE="${SANDBOXD_IMAGE:-$REGISTRY/sandboxd-base:$IMAGE_TAG}"
export SANDBOXD_IMAGE="$BASE_IMAGE" SANDBOXD_IMAGE_TAG="$IMAGE_TAG"

# ── data dir ─────────────────────────────────────────────────────────
# Create it (sudo if we don't own the parent). Workspaces + SQLite + the
# shared access log live here.
if [ ! -d "$DATA_DIR" ]; then
  if mkdir -p "$DATA_DIR" 2>/dev/null; then :; else sudo mkdir -p "$DATA_DIR"; fi
  ok "created data dir $DATA_DIR"
fi
if [ ! -d "$LOG_DIR" ]; then
  if mkdir -p "$LOG_DIR" 2>/dev/null; then :; else sudo mkdir -p "$LOG_DIR"; fi
fi
# Traefik writes the access log here; make sure it can.
( chmod 0777 "$LOG_DIR" 2>/dev/null || sudo chmod 0777 "$LOG_DIR" ) || true
ok "data dir ready: $DATA_DIR"

# ── API auth bootstrap key ───────────────────────────────────────────
# Auth is ON by default: every /v1 call needs a console session or an API key.
# Seed ONE printed bootstrap key so scripts (and the engine) work with zero
# config. The console itself does NOT use this key — it uses a login/session.
# Rotate the key by editing .env (SIGHUP reloads) or minting one in the console.
API_KEY=""
CUR_TOKENS="$(grep -E '^SANDBOXD_API_TOKENS=' .env 2>/dev/null | head -1 | cut -d= -f2-)"
if [ -z "$CUR_TOKENS" ]; then
  if command -v openssl >/dev/null 2>&1; then
    RAND="$(openssl rand -base64 32 | tr '+/' '-_' | tr -d '=')"
  else
    RAND="$(head -c 32 /dev/urandom | base64 | tr '+/' '-_' | tr -d '=')"
  fi
  API_KEY="sk_${RAND}"
  # BSD/macOS mktemp needs an explicit template (GNU works without one).
  tmp="$(mktemp "${TMPDIR:-/tmp}/sandboxd-env.XXXXXX")"; grep -vE '^SANDBOXD_API_TOKENS=' .env > "$tmp" 2>/dev/null || true; mv "$tmp" .env
  printf 'SANDBOXD_API_TOKENS=default=%s\n' "$API_KEY" >> .env
  ok "API bootstrap key generated (shown at the end)"
else
  # Already configured — surface the existing `default=` key if there is one.
  API_KEY="$(printf '%s' "$CUR_TOKENS" | tr ',' '\n' | sed -n 's/^default=//p' | head -1)"
  info "auth: using the SANDBOXD_API_TOKENS already in .env"
fi

# ── optional web console (ON by default) ─────────────────────────────
# The console is the fastest way to *see* sandboxd. On first open it asks you to
# create a password (control-plane login); no secret in .env. Headless with
# SANDBOXD_CONSOLE=0  or  --no-console.
CONSOLE=1
case " $* " in *" --no-console "*) CONSOLE=0 ;; esac
[ "${SANDBOXD_CONSOLE:-1}" = "0" ] && CONSOLE=0
PROFILE_ARGS=""
[ "$CONSOLE" = "1" ] && PROFILE_ARGS="--profile console"

# Stash the console URL + bootstrap key (gitignored, 0600) so ./console-login.sh
# can show them again anytime.
{
  printf 'console_url=http://console.%s%s\n' "${PREVIEW_DOMAIN:-localhost}" "$([ "${HTTP_PORT:-80}" != "80" ] && printf ':%s' "${HTTP_PORT:-80}")"
  printf 'api_key=%s\n' "$API_KEY"
} > .console-login 2>/dev/null && chmod 600 .console-login

# ── build + start the stack ──────────────────────────────────────────
# Compose must read SANDBOXD_API_TOKENS straight from .env. A stale empty value
# inherited into this shell from the earlier `. ./.env` would outrank .env and
# shadow the bootstrap key we just wrote (leaving auth on with no working key).
# Drop it so compose picks up the .env value.
unset SANDBOXD_API_TOKENS
# Stamp the build (sandboxd version / telemetry / settings) from git when present.
export SANDBOXD_VERSION="$(git -C "$REPO_ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)"
export SANDBOXD_GIT_COMMIT="$(git -C "$REPO_ROOT" rev-parse --short=12 HEAD 2>/dev/null || echo unknown)"
# ── acquire images: prebuilt (fast, default) or build from source ────
# auto: try pulling signed prebuilt images; fall back to building if they're
# not published for this version. --pull requires them; --build always compiles.
USE_PULL=0
if [ "$MODE" = "pull" ]; then
  USE_PULL=1
elif [ "$MODE" = "auto" ]; then
  STEP="image_pull"
  info "looking for prebuilt images ($REGISTRY, tag $IMAGE_TAG)…"
  if $DOCKER pull -q "$BASE_IMAGE" >/dev/null 2>&1 && $COMPOSE $PROFILE_ARGS pull -q >/dev/null 2>&1; then
    USE_PULL=1; ok "using prebuilt images (multi-arch, cosign-signed)"
  else
    info "no prebuilt images for this version — building from source"
  fi
fi

if [ "$USE_PULL" = "1" ]; then
  if [ "$MODE" = "pull" ]; then
    STEP="image_pull"
    bold "Pulling prebuilt images…"
    $DOCKER pull "$BASE_IMAGE"
    $COMPOSE $PROFILE_ARGS pull
    ok "images pulled"
  fi
else
  STEP="base_image_build"
  bold "Building the sandbox base image (one-time, a few minutes)…"
  DOCKER="$DOCKER" SANDBOXD_IMAGE="$BASE_IMAGE" bash image/build.sh
  ok "base image: $BASE_IMAGE"
  STEP="stack_build"
  bold "Building the control plane${CONSOLE:+ + console}…"
  $COMPOSE $PROFILE_ARGS build
fi

STEP="compose_up"
bold "Starting the stack…"
$COMPOSE $PROFILE_ARGS up -d
ok "stack is up"
chmod +x console-login.sh 2>/dev/null || true   # so `./console-login.sh` always runs

# ── summary ──────────────────────────────────────────────────────────
API_BIND="${SANDBOXD_API_BIND:-127.0.0.1:9090}"
HTTP_PORT="${HTTP_PORT:-80}"
PREVIEW_DOMAIN="${PREVIEW_DOMAIN:-localhost}"
PORTSUFFIX=""; [ "$HTTP_PORT" != "80" ] && PORTSUFFIX=":$HTTP_PORT"

echo
bold "sandboxd is running 🎉"
cat <<EOF

  Control-plane API : http://${API_BIND}   (auth required — use your API key)
  Preview URLs      : http://s-<id>-<port>.preview.${PREVIEW_DOMAIN}${PORTSUFFIX}

  Create your first sandbox (exposing a dev server on port 3000):

    curl -s -XPOST http://${API_BIND}/sandbox \\
      -H "Authorization: Bearer ${API_KEY:-\$SANDBOXD_API_KEY}" \\
      -H 'content-type: application/json' \\
      -d '{"id":"demo01","ports":[3000]}' | tee /dev/stderr

  Then open:  http://s-demo01-3000.preview.${PREVIEW_DOMAIN}${PORTSUFFIX}

  Logs:   $COMPOSE logs -f sandboxd
  Stop:   $COMPOSE down

  Telemetry : anonymous version + daily heartbeat (no code/PII). Opt out: SANDBOXD_TELEMETRY=off
EOF

if [ "$CONSOLE" = "1" ]; then
  echo
  bold "Web console — open this first 👇"
  printf '\n  Open    : http://console.%s%s\n' "$PREVIEW_DOMAIN" "$PORTSUFFIX"
  printf '  Login   : create your password on first visit (change it anytime in Settings)\n'
  printf '  Then connect an agent in Settings, create an app, and build.\n'
fi
if [ -n "$API_KEY" ]; then
  echo
  bold "API key (for scripts / the engine) 🔑"
  printf '\n  %s\n' "$API_KEY"
  printf '  Send it as:  Authorization: Bearer <key>   (rotate in .env or the console)\n'
fi
printf '\n  Lost these later?  run:  ./console-login.sh\n'
printf '  Update later:      run:  ./upgrade.sh   (backs up first, auto-rollback)\n'
chmod +x upgrade.sh 2>/dev/null || true

# A single, plain (no-color) nudge — suppress with SANDBOXD_NO_SPONSOR=1.
[ -z "${SANDBOXD_NO_SPONSOR:-}" ] && printf '\n  \342\230\205 sandboxd is free & MIT. If it saves you time: https://github.com/sponsors/tastyeffectco\n'

# Founder support hook — the person who just watched the install succeed is at
# peak goodwill. One honest line; suppressed alongside the sponsor nudge.
[ -z "${SANDBOXD_NO_SPONSOR:-}" ] && printf '  Deploying this for real? I help teams get sandboxd production-ready \342\200\224 https://sandboxd.io/support\n'
