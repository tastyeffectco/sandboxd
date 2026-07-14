#!/usr/bin/env bash
#
# sandboxd — safe, in-place upgrade.
#
#   ./upgrade.sh            upgrade to the latest version (backs up first)
#   ./upgrade.sh --check    show current vs latest release; make NO changes
#   ./upgrade.sh <ref>      upgrade to a specific tag or branch (e.g. v0.4.0)
#
# It ALWAYS backs up the database + .env before touching anything, applies new
# migrations (additive by design), health-checks the new stack, and rolls back
# automatically if it does not come up. Safe to run on a production instance.

set -euo pipefail
cd "$(dirname "$0")"

bold() { printf '\033[1m%s\033[0m\n' "$*"; }
info() { printf '  \033[36m›\033[0m %s\n' "$*"; }
ok()   { printf '  \033[32m✓\033[0m %s\n' "$*"; }
warn() { printf '  \033[33m! %s\033[0m\n' "$*"; }
die()  { printf '  \033[31m✗ %s\033[0m\n' "$*" >&2; exit 1; }

[ -f docker-compose.yml ] && [ -d control-plane ] || \
  die "run this from your sandboxd source dir (the folder with install.sh)."
command -v git >/dev/null 2>&1 || die "git is required."

# ── current + latest versions ────────────────────────────────────────
CUR="$(git describe --tags --always 2>/dev/null || echo unknown)"
LATEST="$(curl -fsSL https://api.github.com/repos/tastyeffectco/sandboxd/releases/latest 2>/dev/null \
          | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1 || true)"
[ -n "$LATEST" ] || LATEST="(couldn't reach GitHub)"

bold "sandboxd upgrade"
info "current release checkout : $CUR"
info "latest release          : $LATEST"

# ── --check: report and exit, changing nothing ───────────────────────
if [ "${1:-}" = "--check" ]; then
  case "$LATEST" in
    "(couldn't reach GitHub)") warn "couldn't reach GitHub to check for the latest release." ;;
    "$CUR")     ok "you're on the latest release ($LATEST)." ;;
    *) case "$CUR" in
         "$LATEST"-*) info "you're on a development build ahead of the latest release ($LATEST) — tracking main." ;;
         *)           warn "an update may be available: $LATEST. Run ./upgrade.sh to install it." ;;
       esac ;;
  esac
  exit 0
fi

REF="${1:-main}"   # default: track main (where releases land today); or pass a tag

# ── load .env + detect docker/compose (mirrors install.sh) ───────────
# shellcheck disable=SC1091
set -a; . ./.env 2>/dev/null || true; set +a
DATA_DIR="${SANDBOXD_DATA_DIR:-/var/lib/sandboxed}"
API_BIND="${SANDBOXD_API_BIND:-127.0.0.1:9090}"

DOCKER="docker"; docker info >/dev/null 2>&1 || DOCKER="sudo docker"
if $DOCKER compose version >/dev/null 2>&1; then COMPOSE="$DOCKER compose"
elif command -v docker-compose >/dev/null 2>&1; then COMPOSE="docker-compose"
else die "Docker Compose not found."; fi
# Keep the console running if it was running.
PROFILE=""; $DOCKER ps --format '{{.Names}}' 2>/dev/null | grep -q 'sandboxd-console' && PROFILE="--profile console"

# ── 1. back up (always, before anything changes) ─────────────────────
TS="$(date +%Y%m%d-%H%M%S 2>/dev/null || echo backup)"
BK="$DATA_DIR/backups/$TS"
DB="$DATA_DIR/state/sandboxd.db"
PREV_SHA="$(git rev-parse HEAD)"
bold "1/4 · Backing up"
( mkdir -p "$BK" 2>/dev/null || sudo mkdir -p "$BK" ) || die "could not create backup dir $BK"
[ -f "$DB" ] && { cp "$DB" "$BK/" 2>/dev/null || sudo cp "$DB" "$BK/"; }
cp .env "$BK/.env" 2>/dev/null || true
printf '%s\n' "$PREV_SHA" > "$BK/previous-commit.txt" 2>/dev/null || \
  { printf '%s\n' "$PREV_SHA" | sudo tee "$BK/previous-commit.txt" >/dev/null; }
ok "backup at $BK (database + .env + current commit $PREV_SHA)"

# ── 2. fetch the new code ────────────────────────────────────────────
bold "2/4 · Fetching $REF"
git fetch --depth 1 -q origin "$REF"
git reset --hard -q FETCH_HEAD
NEW_SHA="$(git rev-parse HEAD)"
if [ "$NEW_SHA" = "$PREV_SHA" ]; then ok "already up to date — nothing to do."; exit 0; fi
ok "updated source to $(git describe --tags --always 2>/dev/null || echo "$NEW_SHA")"

# ── 3. rebuild + restart (migrations apply on control-plane boot) ────
bold "3/4 · Building + restarting the stack"
$COMPOSE $PROFILE build
$COMPOSE $PROFILE up -d

# ── 4. health-check; roll back automatically on failure ──────────────
bold "4/4 · Health check"
healthy=0
for _ in $(seq 1 30); do
  if curl -fsS "http://$API_BIND/healthz" 2>/dev/null | grep -q ok; then healthy=1; break; fi
  sleep 2
done

if [ "$healthy" = "1" ]; then
  echo
  ok "sandboxd is healthy on $(git describe --tags --always 2>/dev/null || echo "$NEW_SHA") 🎉"
  info "backup kept at $BK — delete it once you're happy."
else
  echo
  warn "the new version did not become healthy — rolling back."
  [ -f "$BK/sandboxd.db" ] && { cp "$BK/sandboxd.db" "$DB" 2>/dev/null || sudo cp "$BK/sandboxd.db" "$DB"; }
  git reset --hard -q "$PREV_SHA"
  $COMPOSE $PROFILE build >/dev/null 2>&1 || true
  $COMPOSE $PROFILE up -d || true
  die "rolled back to $PREV_SHA (database restored from $BK). Inspect logs: $COMPOSE logs sandboxd"
fi
