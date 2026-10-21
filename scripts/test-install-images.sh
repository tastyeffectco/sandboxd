#!/usr/bin/env bash
# Exercise release selection, signature failures and source fallback without
# a Docker daemon, network access, or writes outside a temporary directory.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/sandboxd-image-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/image"
cat > "$tmp/image/build.sh" <<'STUB'
printf 'base-build %s\n' "$SANDBOXD_IMAGE" >> "$TEST_LOG"
STUB

run_case() (
  cd "$tmp"
  TEST_LOG="$tmp/$1.log"; export TEST_LOG
  : > "$TEST_LOG"
  MODE="$2"; CONSOLE="${3:-1}"
  REPO_ROOT="$tmp"; DOCKER=docker_stub; COMPOSE=compose_stub
  PROFILE_ARGS="--profile console"
  unset SANDBOXD_IMAGE SANDBOXD_CONTROL_PLANE_IMAGE SANDBOXD_CONSOLE_IMAGE SANDBOXD_IMAGE_TAG
  TEST_TAG=v1.2.3; TEST_DIRTY=0; TEST_BAD_SIGNATURE=0; TEST_PULL_FAIL=0
  info() { :; }
  ok() { :; }
  die() { printf '%s\n' "$*" >> "$TEST_LOG"; exit 1; }
  git() {
    if [ "${TEST_REAL_GIT:-0}" = 1 ]; then command git "$@"; return; fi
    case "$*" in
      *describe*) printf '%s\n' "$TEST_TAG" ;;
      *diff*) return "$TEST_DIRTY" ;;
      *) return 1 ;;
    esac
  }
  docker_stub() {
    printf 'docker %s\n' "$*" >> "$TEST_LOG"
    case "$1 $2" in
      "pull "*) return "$TEST_PULL_FAIL" ;;
      "image inspect")
        # The image reference is the final argument.
        local ref
        for ref in "$@"; do :; done
        printf '%s@sha256:%064d\n' "${ref%:*}" 1
        ;;
      *) return 1 ;;
    esac
  }
  compose_stub() { printf 'compose %s\n' "$*" >> "$TEST_LOG"; }
  cosign() {
    printf 'cosign %s\n' "$*" >> "$TEST_LOG"
    return "$TEST_BAD_SIGNATURE"
  }
  case "$1" in
    desktop-tag) TEST_REAL_GIT=1; REPO_ROOT="$tmp/tagged" ;;
    development) TEST_TAG= ;;
    dirty) TEST_DIRTY=1 ;;
    mismatch) SANDBOXD_IMAGE_TAG=0.3.0 ;;
    custom) SANDBOXD_IMAGE=local/custom:test ;;
    unsigned-auto|unsigned-pull) TEST_BAD_SIGNATURE=1 ;;
    unavailable) TEST_PULL_FAIL=1 ;;
    no-cosign) unset -f cosign; PATH=/nonexistent ;;
  esac
  # Source before removing PATH in no-cosign; this helper uses shell builtins
  # until it rejects --pull, so no system tools can accidentally run.
  . "$ROOT/scripts/install-images.sh"
  acquire_images
  printf 'result pull=%s base=%s\n' "$USE_PULL" "$BASE_IMAGE" >> "$TEST_LOG"
)

for scenario in development dirty mismatch custom unavailable unsigned-auto; do
  run_case "$scenario" auto
  grep -q '^result pull=0 ' "$tmp/$scenario.log"
  grep -q '^base-build ' "$tmp/$scenario.log"
  grep -q '^compose .*build$' "$tmp/$scenario.log"
done
run_case build build
! grep -q '^docker pull ' "$tmp/build.log"
# A separate annotated desktop tag must not hide an engine tag on the same
# commit (Git otherwise prefers the annotated tag over the lightweight tag).
command git init -q "$tmp/tagged"
command git -C "$tmp/tagged" -c user.name=Fixture -c user.email=fixture@example.invalid commit --allow-empty -qm fixture
command git -C "$tmp/tagged" tag v1.2.3
command git -C "$tmp/tagged" -c user.name=Fixture -c user.email=fixture@example.invalid tag -a desktop-v9.9.9 -m desktop
run_case desktop-tag pull
grep -q '^result pull=1 base=ghcr.io/tastyeffectco/sandboxd-base:1.2.3$' "$tmp/desktop-tag.log"
run_case verified pull
grep -q '^result pull=1 ' "$tmp/verified.log"
[ "$(grep -c '^cosign verify ' "$tmp/verified.log")" = 3 ]
grep -q -- '--certificate-identity https://github.com/tastyeffectco/sandboxd/.github/workflows/release-images.yml@refs/tags/v1.2.3 --certificate-oidc-issuer https://token.actions.githubusercontent.com ghcr.io/tastyeffectco/sandboxd-base@sha256:' "$tmp/verified.log"
! grep -q '^base-build ' "$tmp/verified.log"
run_case headless pull 0
[ "$(grep -c '^cosign verify ' "$tmp/headless.log")" = 2 ]
for scenario in unsigned-pull mismatch dirty development custom unavailable no-cosign; do
  if run_case "$scenario" pull; then
    echo "FAIL: --pull accepted $scenario" >&2
    exit 1
  fi
  ! grep -q '^result ' "$tmp/$scenario.log"
done
# Run the actual installer against a disposable checkout and stub executables,
# including curl/sudo so a regression cannot contact a service or use privilege.
mkdir -p "$tmp/bin"
cat > "$tmp/bin/git" <<'STUB'
#!/bin/sh
case "$*" in *diff*) exit 0 ;; *) echo v1.2.3 ;; esac
STUB
cat > "$tmp/bin/docker" <<'STUB'
#!/bin/sh
printf 'docker %s\n' "$*" >> "$TEST_LOG"
case "$1 $2" in
  "compose version") echo 2.0-stub ;;
  "image inspect")
    case "$*" in
      *RepoDigests*)
        for ref in "$@"; do :; done
        printf '%s@sha256:%064d\n' "${ref%:*}" 1 ;;
      *) echo 400 ;;
    esac ;;
  "version "*) echo 27.0-stub ;;
esac
exit 0
STUB
cat > "$tmp/bin/cosign" <<'STUB'
#!/bin/sh
printf 'cosign %s\n' "$*" >> "$TEST_LOG"
exit "${TEST_SIGNATURE_FAIL:-0}"
STUB
for tool in curl sudo; do
  printf '#!/bin/sh\nexit 1\n' > "$tmp/bin/$tool"
done
chmod +x "$tmp/bin/"*
for scenario in build pull bad-signature; do
  checkout="$tmp/install-$scenario"
  mkdir -p "$checkout/control-plane" "$checkout/scripts" "$checkout/image"
  cp "$ROOT/install.sh" "$ROOT/docker-compose.yml" "$checkout/"
  cp "$ROOT/scripts/install-images.sh" "$checkout/scripts/"
  cp "$tmp/image/build.sh" "$checkout/image/"
  sed -e "s|^SANDBOXD_DATA_DIR=.*|SANDBOXD_DATA_DIR=$checkout/data|" \
      -e "s|^SANDBOXD_LOG_DIR=.*|SANDBOXD_LOG_DIR=$checkout/data/log|" \
      -e 's|^SANDBOXD_TELEMETRY=.*|SANDBOXD_TELEMETRY=off|' \
      "$ROOT/.env.example" > "$checkout/.env"
  TEST_LOG="$tmp/install-$scenario.log"; export TEST_LOG
  : > "$TEST_LOG"
  mode="$scenario"; fail=0
  [ "$scenario" != bad-signature ] || { mode=pull; fail=1; }
  if PATH="$tmp/bin:$PATH" TEST_SIGNATURE_FAIL="$fail" SANDBOXD_NO_SPONSOR=1 \
      bash "$checkout/install.sh" "--$mode" > "$tmp/installer-output" 2>&1; then
    [ "$scenario" != bad-signature ]
    grep -q '^docker compose pull --policy missing traefik$' "$TEST_LOG"
    grep -q '^docker compose --profile console up -d --pull never$' "$TEST_LOG"
    if [ "$scenario" = pull ]; then
      [ "$(grep -c '^cosign verify ' "$TEST_LOG")" = 3 ]
      grep -q '^SANDBOXD_IMAGE=ghcr.io/tastyeffectco/sandboxd-base:1.2.3$' "$checkout/.env"
      ! grep -q '^base-build ' "$TEST_LOG"
    else
      grep -q '^base-build sandboxd-base:0.3.0$' "$TEST_LOG"
    fi
  else
    if [ "$scenario" != bad-signature ]; then
      echo "FAIL: installer $scenario" >&2
      exit 1
    fi
    ! grep -q 'compose .* up ' "$TEST_LOG"
  fi
done
echo "OK: image selection, signature failures, source fallback and full installer integration"
