#!/usr/bin/env bash
#
# sandboxd — safe, in-place upgrade.
#
#   ./upgrade.sh                 upgrade to the latest version (backs up first)
#   ./upgrade.sh --check         show current vs latest release; make NO changes
#   ./upgrade.sh <ref>           upgrade to a specific tag or branch (e.g. v0.4.0)
#   ./upgrade.sh --rebuild-base  also force-rebuild the sandbox base image
#                                (runtimed); normally auto-detected from the diff
#   ./upgrade.sh --yes           don't ask before applying a release that lists
#                                breaking changes (the notes are still printed)
#
# Before applying, it prints the release notes of the target ("What's new"),
# with any breaking changes first. It ALWAYS backs up the database + .env before touching anything, applies new
# migrations (additive by design), health-checks the new stack, and rolls back
# automatically if it does not come up. Safe to run on a production instance.

set -euo pipefail
# Never fail silently: name the line so a report carries it.
trap 'rc=$?; if [ "$rc" -ne 0 ]; then printf "  \033[31m✗ upgrade.sh stopped (exit %s) at line %s\033[0m\n" "$rc" "$LINENO" >&2; fi' ERR
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
CUR="$(git describe --tags --match 'v[0-9]*' --always 2>/dev/null || echo unknown)"
LATEST="$(curl -fsSL https://api.github.com/repos/tastyeffectco/sandboxd/releases/latest 2>/dev/null \
          | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1 || true)"
[ -n "$LATEST" ] || LATEST="(couldn't reach GitHub)"

bold "sandboxd upgrade"
info "current release checkout : $CUR"
info "latest release          : $LATEST"

# ── release notes (best effort: silent when offline or without a JSON tool) ──
# NOTES holds the release body of <tag>; BREAKING its "breaking changes"
# section (a heading of any level, or a bold line, mentioning "breaking", up
# to the next heading of the same or higher level). NOTES_KNOWN=1 once fetched.
NOTES="" BREAKING="" NOTES_KNOWN=0
fetch_notes() {
  local json
  json="$(curl -fsSL --max-time 10 -H 'Accept: application/vnd.github+json' \
          "https://api.github.com/repos/tastyeffectco/sandboxd/releases/tags/$1" 2>/dev/null)" || return 0
  [ -n "$json" ] || return 0
  if command -v python3 >/dev/null 2>&1; then
    NOTES="$(printf '%s' "$json" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("body") or "")' 2>/dev/null)" || return 0
  elif command -v jq >/dev/null 2>&1; then
    NOTES="$(printf '%s' "$json" | jq -r '.body // ""' 2>/dev/null)" || return 0
  else
    return 0
  fi
  NOTES_KNOWN=1
  BREAKING="$(printf '%s\n' "$NOTES" | awk '
    function level(l,  n) {
      if (l ~ /^#+[ \t]/) { n = 0; while (substr(l, n + 1, 1) == "#") n++; return n <= 6 ? n : 0 }
      if (l ~ /^\*\*[^*]+\*\*:?$/) return 7
      return 0
    }
    { sub(/\r$/, ""); l = $0; sub(/^[ \t]+/, "", l); sub(/[ \t]+$/, "", l); n = level(l) }
    start == 0 && n > 0 && tolower(l) ~ /breaking/ { start = n; next }
    start > 0 && n > 0 && n <= start { exit }
    start > 0 { print }
  ' | sed -e :a -e '/^\n*$/{$d;N;ba' -e '}')"
}
# Print the notes for the target tag: breaking changes first, clearly marked.
print_notes() {
  [ "$NOTES_KNOWN" = 1 ] || return 0
  echo
  if [ -n "$BREAKING" ]; then
    printf '  \033[31;1m!! BREAKING CHANGES in %s — read before continuing\033[0m\n' "$1"
    printf '%s\n' "$BREAKING" | sed 's/^/     /'
    echo
  fi
  bold "What's new in $1"
  if [ -n "$NOTES" ]; then printf '%s\n' "$NOTES" | sed 's/^/     /'; else info "(no notes on this release)"; fi
  echo
}

# ── --check: report and exit, changing nothing ───────────────────────
if [ "${1:-}" = "--check" ]; then
  case "$LATEST" in
    "(couldn't reach GitHub)") warn "couldn't reach GitHub to check for the latest release." ;;
    "$CUR")     ok "you're on the latest release ($LATEST)." ;;
    *) case "$CUR" in
         "$LATEST"-*) info "you're on a development build ahead of the latest release ($LATEST) — tracking main." ;;
         *)           warn "an update may be available: $LATEST. Run ./upgrade.sh to install it." ;;
       esac
       fetch_notes "$LATEST"
       if [ "$NOTES_KNOWN" = 1 ]; then
         if [ -n "$BREAKING" ]; then info "breaking changes: yes"; else info "breaking changes: no"; fi
       fi ;;
  esac
  exit 0
fi

# First non-flag argument is the ref; default: track main (where releases land
# today). Flags (--rebuild-base, --yes) are handled where they apply.
REF="main" YES=0
for _a in "$@"; do case "$_a" in --*) ;; *) REF="$_a"; break ;; esac; done
case " $* " in *" --yes "*) YES=1 ;; esac

# Show what the target release brings; when it lists breaking changes, an
# operator at a terminal must confirm (or pass --yes). Driven non-interactively
# — the console's in-place upgrader — it only prints, never prompts.
case "$REF" in v[0-9]*) NOTES_TAG="$REF" ;; *) NOTES_TAG="$LATEST" ;; esac
case "$NOTES_TAG" in "(couldn't reach GitHub)") ;; *) fetch_notes "$NOTES_TAG" ;; esac
print_notes "$NOTES_TAG"
if [ -n "$BREAKING" ] && [ "$YES" != 1 ] && [ -t 0 ]; then
  printf '  Continue with the upgrade? [y/N] '
  read -r _ans || _ans=""
  case "$_ans" in y|Y|yes|YES) ;; *) die "upgrade cancelled — rerun with --yes to skip this question." ;; esac
fi

# ── load .env + detect docker/compose (mirrors install.sh) ───────────
# shellcheck disable=SC1091
# (bash 3.2 exits on sourcing a missing file even with `|| true`, so check first)
if [ -f ./.env ]; then set -a; . ./.env; set +a; fi

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
API_BIND="${SANDBOXD_API_BIND:-127.0.0.1:9090}"

DOCKER="docker"; docker info >/dev/null 2>&1 || DOCKER="sudo docker"
if $DOCKER compose version >/dev/null 2>&1; then COMPOSE="$DOCKER compose"
elif command -v docker-compose >/dev/null 2>&1; then COMPOSE="docker-compose"
else die "Docker Compose not found."; fi
# Keep the console running if it was running.
# `up` keeps the console running only if it was running; `build` ALWAYS includes
# it — otherwise an upgrade run while the console was stopped (or being
# recreated) leaves an old console image behind the new control plane.
PROFILE=""; $DOCKER ps -a --format '{{.Names}}' 2>/dev/null | grep -q 'sandboxd-console' && PROFILE="--profile console"
[ "${SANDBOXD_CONSOLE:-1}" = "0" ] || PROFILE="--profile console"
BUILD_PROFILE="--profile console"

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
# Bring tags into the shallow checkout: without them `git describe` stamps
# the build with a bare sha, and the update check (semver-only) goes silent.
git fetch --tags --depth 1 -q origin 2>/dev/null || true
NEW_SHA="$(git rev-parse HEAD)"
if [ "$NEW_SHA" = "$PREV_SHA" ]; then ok "already up to date — nothing to do."; exit 0; fi
ok "updated source to $(git describe --tags --match 'v[0-9]*' --always 2>/dev/null || echo "$NEW_SHA")"

# ── 3. rebuild + restart (migrations apply on control-plane boot) ────
bold "3/4 · Building + restarting the stack"

# The sandbox BASE image carries runtimed (the in-sandbox supervisor), which
# `compose build` does NOT rebuild — so a runtimed change would never reach
# sandboxes on upgrade (they'd keep running the old supervisor and miss new
# agent/model support). Rebuild the base image when anything that feeds it
# changed between the two commits; if history is too shallow to tell, rebuild
# to be safe. Force with:  ./upgrade.sh --rebuild-base
BASE_IMAGE="${SANDBOXD_IMAGE:-sandboxd-base:0.3.0}"
NEED_BASE=0
case " $* " in *" --rebuild-base "*) NEED_BASE=1 ;; esac
if [ "$NEED_BASE" = 0 ]; then
  if git cat-file -e "$PREV_SHA" 2>/dev/null; then
    git diff --quiet "$PREV_SHA" "$NEW_SHA" -- \
      image/ control-plane/cmd/runtimed/ control-plane/internal/runtime/ 2>/dev/null \
      || NEED_BASE=1
  else
    NEED_BASE=1   # shallow history — can't prove it's unchanged
  fi
fi
if [ "$NEED_BASE" = 1 ]; then
  info "sandbox base image sources changed — rebuilding $BASE_IMAGE (runtimed lives here)"
  DOCKER="$DOCKER" SANDBOXD_IMAGE="$BASE_IMAGE" bash image/build.sh "${BASE_IMAGE##*:}"
  ok "base image rebuilt: $BASE_IMAGE"
  info "new sandboxes use it immediately; existing ones pick it up when recreated"
else
  info "base image unchanged — skipping its rebuild"
fi

# Stamp the build (sandboxd version / telemetry / settings) from git.
export SANDBOXD_VERSION="$(git describe --tags --match 'v[0-9]*' --always --dirty 2>/dev/null || echo dev)"
grep -q '^SANDBOXD_SRC_DIR=.' .env 2>/dev/null || printf 'SANDBOXD_SRC_DIR=%s\n' "$(pwd)" >> .env
$DOCKER build -q -t "sandboxd-upgrader:${SANDBOXD_VERSION}" image/upgrader >/dev/null && {
  tmp="$(mktemp "${TMPDIR:-/tmp}/sandboxd-env.XXXXXX")"; grep -vE '^SANDBOXD_UPGRADER_IMAGE=' .env > "$tmp" 2>/dev/null || true
  printf 'SANDBOXD_UPGRADER_IMAGE=sandboxd-upgrader:%s\n' "$SANDBOXD_VERSION" >> "$tmp"; mv "$tmp" .env; } || true
export SANDBOXD_GIT_COMMIT="$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)"
info "building $SANDBOXD_VERSION ($SANDBOXD_GIT_COMMIT)"
$COMPOSE $BUILD_PROFILE build
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
  ok "sandboxd is healthy on $(git describe --tags --match 'v[0-9]*' --always 2>/dev/null || echo "$NEW_SHA") 🎉"
  info "backup kept at $BK — delete it once you're happy."
else
  echo
  warn "the new version did not become healthy — rolling back."
  [ -f "$BK/sandboxd.db" ] && { cp "$BK/sandboxd.db" "$DB" 2>/dev/null || sudo cp "$BK/sandboxd.db" "$DB"; }
  git reset --hard -q "$PREV_SHA"
  # Re-stamp so the rebuilt image reports the version we actually rolled back to.
  export SANDBOXD_VERSION="$(git describe --tags --match 'v[0-9]*' --always --dirty 2>/dev/null || echo dev)"
  export SANDBOXD_GIT_COMMIT="$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)"
  $COMPOSE $BUILD_PROFILE build >/dev/null 2>&1 || true
  $COMPOSE $PROFILE up -d || true
  die "rolled back to $PREV_SHA (database restored from $BK). Inspect logs: $COMPOSE logs sandboxd"
fi
