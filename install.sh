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
# Idempotent engine installation. On macOS, also installs the optional desktop
# client under ~/Applications by default; disable with --no-desktop.
#
# Servers (the desktop's "add a server", or by hand):
#   --front=auto|caddy|existing|manual   who owns ports 80/443 and https:
#       caddy     sandboxd runs Caddy (needs 80 and 443 free)
#       existing  your nginx or Caddy forwards to sandboxd (frontdoor helper)
#       manual    you wire up the front door yourself
#       auto      caddy when 80 and 443 are free, else existing
#   --domain=<D>                 app addresses under D (default <ip>.sslip.io)
#   --desktop-key="<ssh key>"    let the desktop in as the limited `sandboxd`
#                                user (API tunnel + front-door commands only)
# Without --front nothing changes: Traefik stays the front door on :80.
# With --front the last line printed is machine-readable:
#   SANDBOXD_INSTALL_RESULT {"front":…,"domain":…,"proxy":…,"api_token":…,"api":…}

set -euo pipefail

# ── pretty output ────────────────────────────────────────────────────
bold() { printf '\033[1m%s\033[0m\n' "$*"; }
info() { printf '  \033[36m›\033[0m %s\n' "$*"; }
ok()   { printf '  \033[32m✓\033[0m %s\n' "$*"; }
die()  { printf '  \033[31m✗ %s\033[0m\n' "$*" >&2; exit 1; }

# --build always builds locally; --pull requires verified release images.
MODE=auto
FRONT=""; FRONT_DOMAIN=""; DESKTOP_KEY=""
for arg in "$@"; do
  case "$arg" in
    --build) MODE=build ;;
    --pull) MODE=pull ;;
    --no-console|--no-desktop) ;;
    --front=*) FRONT="${arg#--front=}" ;;
    --domain=*) FRONT_DOMAIN="${arg#--domain=}" ;;
    --desktop-key=*) DESKTOP_KEY="${arg#--desktop-key=}" ;;
    *) die "Unknown option: $arg (use --build, --pull, --no-console, --no-desktop, --front=, --domain= or --desktop-key=)" ;;
  esac
done
case "$FRONT" in
  ""|auto|caddy|existing|manual) ;;
  *) die "Unknown front door: --front=$FRONT (use auto, caddy, existing or manual)" ;;
esac
if [ -z "$FRONT" ] && { [ -n "$FRONT_DOMAIN" ] || [ -n "$DESKTOP_KEY" ]; }; then
  die "--domain and --desktop-key need --front (for example --front=auto)"
fi

# ── anonymous install-failure beacon ─────────────────────────────────
# If the installer aborts, send ONE event: {stage} + a random id — nothing
# else (no hostname, IP, paths or output). Same opt-out as the daemon:
# SANDBOXD_TELEMETRY=off or DO_NOT_TRACK=1. See docs/telemetry.md.
STAGE=prereqs
tel_off() {
  case "$(printf '%s' "${SANDBOXD_TELEMETRY:-}" | tr 'A-Z' 'a-z')" in off|0|false|no) return 0;; esac
  case "$(printf '%s' "${DO_NOT_TRACK:-}" | tr 'A-Z' 'a-z')" in 1|true|yes) return 0;; esac
  return 1
}
tel_fail() {
  tel_off && return 0
  id="$(cat /proc/sys/kernel/random/uuid 2>/dev/null || echo unknown)"
  curl -s -m 5 -o /dev/null -X POST "${SANDBOXD_POSTHOG_HOST:-https://us.i.posthog.com}/i/v0/e/" \
    -H 'Content-Type: application/json' \
    -d "{\"api_key\":\"${SANDBOXD_POSTHOG_KEY:-phc_pma2C4Wg9EKf4KARJbnU5ZdcNDJGnA5oDtsypKofF2YY}\",\"event\":\"install_failed\",\"distinct_id\":\"$id\",\"properties\":{\"stage\":\"$1\",\"\$ip\":\"\"}}" \
    2>/dev/null || true
}
trap 'rc=$?; [ "$rc" -ne 0 ] && tel_fail "$STAGE"; exit $rc' EXIT

# ── bootstrap: fetch the repo when run standalone (curl … | bash) ────
# Piped from curl, this script has no repo around it. Detect that, clone
# sandboxd, and re-exec the in-repo installer. Overridable via env:
#   SANDBOXD_REPO_URL, SANDBOXD_REF (branch/tag), SANDBOXD_SRC (checkout dir).
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || echo "$PWD")"
if [ ! -f "$REPO_ROOT/docker-compose.yml" ] || [ ! -d "$REPO_ROOT/control-plane" ]; then
  REPO_URL="${SANDBOXD_REPO_URL:-https://github.com/tastyeffectco/sandboxd.git}"
  REF="${SANDBOXD_REF:-main}"
  SRC="${SANDBOXD_SRC:-$HOME/.sandboxd/src}"
  command -v git >/dev/null 2>&1 || die "git is required to install sandboxd — install git and re-run."
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
STAGE=docker
# Use sudo for docker only if the current user can't reach the daemon.
DOCKER="docker"
if ! docker info >/dev/null 2>&1; then
  if sudo -n docker info >/dev/null 2>&1 || sudo docker info >/dev/null 2>&1; then
    DOCKER="sudo docker"
    info "using 'sudo docker' (current user can't reach the Docker daemon)"
  else
    die "Docker is not available. Install Docker Engine and ensure the daemon is running."
  fi
fi

# Compose v2 (docker compose) preferred; fall back to docker-compose.
if $DOCKER compose version >/dev/null 2>&1; then
  COMPOSE="$DOCKER compose"
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE="docker-compose"
else
  die "Docker Compose not found. Install the Compose plugin (docker compose)."
fi

bold "sandboxd — installer"
ok "Docker:  $($DOCKER version --format '{{.Server.Version}}' 2>/dev/null || echo present)"
ok "Compose: $($COMPOSE version --short 2>/dev/null || echo present)"

# ── .env ─────────────────────────────────────────────────────────────
STAGE=env
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

# ── front door (servers only: --front) ───────────────────────────────
# Decided before .env is loaded so compose and this script both see the
# result. Without --front none of this runs.
FRONT_PROXY=none
if [ -n "$FRONT" ]; then
  STAGE=front
  [ "$(uname -s)" = Linux ] || die "--front is for Linux servers."
  # shellcheck source=scripts/install-front.sh
  . "$REPO_ROOT/scripts/install-front.sh"
  PREV_FRONT="$(sed -n 's/^SANDBOXD_FRONT=//p' .env | tail -n1)"
  if [ "$FRONT" = auto ] && [ -n "$PREV_FRONT" ]; then
    FRONT="$PREV_FRONT"   # re-run: keep the front door chosen last time
    info "front door: keeping '$FRONT' from the previous install"
  fi
  FRONT_PROXY="$(front_detect_proxy)"
  FRONT="$(front_resolve "$FRONT")"
  if [ -n "$FRONT_DOMAIN" ]; then
    FRONT_DOMAIN="$(printf '%s' "$FRONT_DOMAIN" | tr 'A-Z' 'a-z')"
    front_valid_domain "$FRONT_DOMAIN" || die "--domain=$FRONT_DOMAIN is not a valid domain name."
  else
    PREV_DOMAIN="$(sed -n 's/^PREVIEW_DOMAIN=//p' .env | tail -n1)"
    case "$PREV_DOMAIN" in
      ""|localhost) PUBLIC_IP="$(front_public_ip)" || die "Could not find this server's public IP address. Pass --domain=<your domain>."
                    FRONT_DOMAIN="$(front_sslip_domain "$PUBLIC_IP")" ;;
      *) FRONT_DOMAIN="$PREV_DOMAIN" ;;
    esac
  fi
  front_write_env .env "$FRONT" "$FRONT_DOMAIN"
  ok "front door: $FRONT (web server found: $FRONT_PROXY) · apps at https://<app>.$FRONT_DOMAIN"
fi

# Load .env so we know the data dir / image tag to use below.
set -a; . ./.env; set +a

# Pre-rename installs keep their data at /var/lib/sandboxed. If .env does not pin
# SANDBOXD_DATA_DIR and the old directory exists, pin it now — never move data.
if [ -z "${SANDBOXD_DATA_DIR:-}" ] && [ -d /var/lib/sandboxed ] && [ ! -d /var/lib/sandboxd ]; then
  SANDBOXD_DATA_DIR=/var/lib/sandboxed; export SANDBOXD_DATA_DIR
  if [ -f .env ] && ! grep -q '^SANDBOXD_DATA_DIR=' .env; then
    printf 'SANDBOXD_DATA_DIR=/var/lib/sandboxed\nSANDBOXD_LOG_DIR=/var/lib/sandboxed/log\n' >> .env
  fi
  echo "  › data stays at /var/lib/sandboxed (pre-rename install); new installs use /var/lib/sandboxd"
fi
DATA_DIR="${SANDBOXD_DATA_DIR:-/var/lib/sandboxd}"
LOG_DIR="${SANDBOXD_LOG_DIR:-$DATA_DIR/log}"
BASE_IMAGE="${SANDBOXD_IMAGE:-sandboxd-base:0.3.0}"

# ── data dir ─────────────────────────────────────────────────────────
STAGE=data-dir
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
STAGE=api-key
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
STAGE=console
# The console is the fastest way to *see* sandboxd. On first open it asks you to
# create a password (control-plane login); no secret in .env. Headless with
# SANDBOXD_CONSOLE=0  or  --no-console.
CONSOLE=1
case " $* " in *" --no-console "*) CONSOLE=0 ;; esac
[ "${SANDBOXD_CONSOLE:-1}" = "0" ] && CONSOLE=0
PROFILE_ARGS=""
[ "$CONSOLE" = "1" ] && PROFILE_ARGS="--profile console"
# The managed Caddy front door (recorded in .env by --front=caddy).
if [ "${SANDBOXD_FRONT:-}" = caddy ]; then
  PROFILE_ARGS="$PROFILE_ARGS --profile caddy"
  for d in "$DATA_DIR/caddy/data" "$DATA_DIR/caddy/config"; do
    [ -d "$d" ] || mkdir -p "$d" 2>/dev/null || sudo mkdir -p "$d"
  done
fi
# Public URLs: an explicit scheme means a front door owns the public port, so
# Traefik's own port never appears in them (mirrors the control plane).
URL_SCHEME="${PREVIEW_URL_SCHEME:-http}"
PORTSUFFIX=""
[ -z "${PREVIEW_URL_SCHEME:-}" ] && [ "${HTTP_PORT:-80}" != "80" ] && PORTSUFFIX=":${HTTP_PORT:-80}"

# Stash the console URL + bootstrap key (gitignored, 0600) so ./console-login.sh
# can show them again anytime.
{
  printf 'console_url=%s://%s%s\n' "$URL_SCHEME" "${CONSOLE_HOST:-console.${PREVIEW_DOMAIN:-localhost}}" "$PORTSUFFIX"
  printf 'api_key=%s\n' "$API_KEY"
} > .console-login 2>/dev/null && chmod 600 .console-login

# ── build + start the stack ──────────────────────────────────────────
STAGE=stack
# Compose must read SANDBOXD_API_TOKENS straight from .env. A stale empty value
# inherited into this shell from the earlier `. ./.env` would outrank .env and
# shadow the bootstrap key we just wrote (leaving auth on with no working key).
# Drop it so compose picks up the .env value.
unset SANDBOXD_API_TOKENS
# Stamp the build (sandboxd version / telemetry / settings) from git when present.
export SANDBOXD_VERSION="$(git -C "$REPO_ROOT" describe --tags --match 'v[0-9]*' --always --dirty 2>/dev/null || echo dev)"
# Record the checkout path for console-driven upgrades (POST /v1/upgrade), and
# build the tiny upgrader image (docker CLI + compose + git) that runs them.
grep -q '^SANDBOXD_INSTALL_METHOD=' .env 2>/dev/null || printf 'SANDBOXD_INSTALL_METHOD=%s\n' "${SANDBOXD_INSTALL_METHOD:-install.sh}" >> .env
if ! grep -q '^SANDBOXD_SRC_DIR=.' .env 2>/dev/null; then
  tmp="$(mktemp "${TMPDIR:-/tmp}/sandboxd-env.XXXXXX")"; grep -vE '^SANDBOXD_SRC_DIR=' .env > "$tmp" 2>/dev/null || true
  printf 'SANDBOXD_SRC_DIR=%s\n' "$REPO_ROOT" >> "$tmp"; mv "$tmp" .env
fi
$DOCKER build -q -t "sandboxd-upgrader:${SANDBOXD_VERSION}" image/upgrader >/dev/null && {
  tmp="$(mktemp "${TMPDIR:-/tmp}/sandboxd-env.XXXXXX")"; grep -vE '^SANDBOXD_UPGRADER_IMAGE=' .env > "$tmp" 2>/dev/null || true
  printf 'SANDBOXD_UPGRADER_IMAGE=sandboxd-upgrader:%s\n' "$SANDBOXD_VERSION" >> "$tmp"; mv "$tmp" .env; } && ok "upgrader image: sandboxd-upgrader:${SANDBOXD_VERSION}"
export SANDBOXD_GIT_COMMIT="$(git -C "$REPO_ROOT" rev-parse --short=12 HEAD 2>/dev/null || echo unknown)"
# Acquire all application images before starting any service.
. "$REPO_ROOT/scripts/install-images.sh"
acquire_images
# Persist the selected names for later compose and upgrade invocations.
tmp="$(mktemp "${TMPDIR:-/tmp}/sandboxd-env.XXXXXX")"
grep -vE '^SANDBOXD_(IMAGE|CONTROL_PLANE_IMAGE|CONSOLE_IMAGE)=' .env > "$tmp" || true
printf 'SANDBOXD_IMAGE=%s\nSANDBOXD_CONTROL_PLANE_IMAGE=%s\nSANDBOXD_CONSOLE_IMAGE=%s\n' \
  "$SANDBOXD_IMAGE" "$SANDBOXD_CONTROL_PLANE_IMAGE" "$SANDBOXD_CONSOLE_IMAGE" >> "$tmp"
mv "$tmp" .env
STAGE=stack
bold "Starting the stack…"
# Fetch the proxy only if missing; application tags must not move between
# signature verification and startup.
$COMPOSE pull --policy missing traefik
[ "${SANDBOXD_FRONT:-}" = caddy ] && $COMPOSE --profile caddy pull --policy missing caddy
$COMPOSE $PROFILE_ARGS up -d --pull never
ok "stack is up"
chmod +x console-login.sh 2>/dev/null || true   # so `./console-login.sh` always runs

# Optional native client: on by default for Mac desktop installs. Headless Linux
# stays unchanged. The Windows entrypoint installs its own native client.
DESKTOP="${SANDBOXD_DESKTOP:-1}"
case " $* " in *" --no-desktop "*) DESKTOP=0 ;; esac
if [ "$(uname -s)" = Darwin ] && [ "$DESKTOP" != 0 ]; then
  STAGE=desktop
  if bash "$REPO_ROOT/scripts/install-desktop.sh"; then
    ok "desktop client ready — connect to http://${SANDBOXD_API_BIND:-127.0.0.1:9090}"
  else
    info "The engine is running, but desktop installation did not finish."
    info "Retry: bash scripts/install-desktop.sh — see docs/desktop.md for release availability."
  fi
fi

# ── limited desktop user + front-door helper (servers only) ─────────
if [ -n "$FRONT" ]; then
  STAGE=frontdoor
  front_install_user "$REPO_ROOT/scripts/frontdoor" "$REPO_ROOT/.env" "$DESKTOP_KEY"
fi

# ── summary ──────────────────────────────────────────────────────────
STAGE=done
API_BIND="${SANDBOXD_API_BIND:-127.0.0.1:9090}"
HTTP_PORT="${HTTP_PORT:-80}"
PREVIEW_DOMAIN="${PREVIEW_DOMAIN:-localhost}"
CONSOLE_HOST="${CONSOLE_HOST:-console.${PREVIEW_DOMAIN}}"
# Preview host shape, mirroring PREVIEW_HOST_STYLE / PREVIEW_HOST_TAG.
PREVIEW_SUFFIX=".preview.${PREVIEW_DOMAIN}"
if [ "${PREVIEW_HOST_STYLE:-nested}" = "flat" ]; then
  PREVIEW_SUFFIX="${PREVIEW_HOST_TAG:+--$PREVIEW_HOST_TAG}.${PREVIEW_DOMAIN}"
fi

echo
bold "sandboxd is running 🎉"
cat <<EOF

  Control-plane API : http://${API_BIND}   (auth required — use your API key)
  Preview URLs      : ${URL_SCHEME}://s-<id>-<port>${PREVIEW_SUFFIX}${PORTSUFFIX}

  Create your first sandbox (exposing a dev server on port 3000):

    curl -s -XPOST http://${API_BIND}/sandbox \\
      -H "Authorization: Bearer ${API_KEY:-\$SANDBOXD_API_KEY}" \\
      -H 'content-type: application/json' \\
      -d '{"id":"demo01","ports":[3000]}' | tee /dev/stderr

  Then open:  ${URL_SCHEME}://s-demo01-3000${PREVIEW_SUFFIX}${PORTSUFFIX}

  Logs:   $COMPOSE logs -f sandboxd
  Stop:   $COMPOSE down

  Telemetry : anonymous version + daily heartbeat (no code/PII). Opt out: SANDBOXD_TELEMETRY=off
EOF

if [ "$CONSOLE" = "1" ]; then
  echo
  bold "Web console — open this first 👇"
  printf '\n  Open    : %s://%s%s\n' "$URL_SCHEME" "$CONSOLE_HOST" "$PORTSUFFIX"
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
printf '  Forgot the console password later?  ./console-login.sh --reset-password\n'
printf '  Update later:      run:  ./upgrade.sh   (backs up first, auto-rollback)\n'
if [ -z "$FRONT" ]; then
  printf '  Public URL:        previews on %s only work from this network / plain HTTP.\n' "$PREVIEW_DOMAIN"
  printf '                     https://you.sandboxd.io in one command: https://sandboxd.io/cloud  ·  or your own domain: https://sandboxd.io/guides/production-tls\n'
fi
chmod +x upgrade.sh 2>/dev/null || true

# A single, plain (no-color) nudge — suppress with SANDBOXD_NO_SPONSOR=1.
if [ -z "${SANDBOXD_NO_SPONSOR:-}" ]; then
  printf '\n  \342\230\205 sandboxd is free & MIT. If it saves you time: https://github.com/sponsors/tastyeffectco\n'
  printf '  Deploying this for real? Support: https://sandboxd.io/support\n'
fi

# Machine-readable result for the desktop (servers only). Must stay the last
# line printed.
if [ -n "$FRONT" ]; then
  echo
  front_result_line "$FRONT" "$PREVIEW_DOMAIN" "$FRONT_PROXY" \
    "$(front_api_token "$(sed -n 's/^SANDBOXD_API_TOKENS=//p' .env | tail -n1)")" "$API_BIND"
fi
