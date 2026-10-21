#!/usr/bin/env bash
# Sourced by install.sh. Only official release images are eligible for pulling.
# Development checkouts and custom image names keep the source-build path.
acquire_images() {
  local release_tag release_version ref digest identity clean=0 eligible=0
  local base_ref plane_ref console_ref verified=1
  release_tag="$(git -C "$REPO_ROOT" describe --tags --match 'v[0-9]*' --exact-match HEAD 2>/dev/null || true)"
  release_version="${release_tag#v}"
  git -C "$REPO_ROOT" diff --quiet HEAD -- && clean=1
  if [[ "$release_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] &&
     [ "$clean" = 1 ] &&
     [ "${SANDBOXD_IMAGE_TAG:-$release_version}" = "$release_version" ]; then
    eligible=1
  fi
  base_ref="ghcr.io/tastyeffectco/sandboxd-base:$release_version"
  plane_ref="ghcr.io/tastyeffectco/sandboxd-control-plane:$release_version"
  console_ref="ghcr.io/tastyeffectco/sandboxd-console:$release_version"
  # Do not pull a user-supplied local/custom image from an unintended registry.
  [ -z "${SANDBOXD_IMAGE:-}" ] || [ "$SANDBOXD_IMAGE" = "$base_ref" ] || eligible=0
  [ -z "${SANDBOXD_CONTROL_PLANE_IMAGE:-}" ] || [ "$SANDBOXD_CONTROL_PLANE_IMAGE" = "$plane_ref" ] || eligible=0
  [ -z "${SANDBOXD_CONSOLE_IMAGE:-}" ] || [ "$SANDBOXD_CONSOLE_IMAGE" = "$console_ref" ] || eligible=0

  if [ "$MODE" = pull ] && { [ "$eligible" != 1 ] || ! command -v cosign >/dev/null 2>&1; }; then
    die "--pull requires cosign, a clean stable release tag, matching image tag, and official image names."
  fi
  USE_PULL=0
  if [ "$MODE" != build ] && [ "$eligible" = 1 ] && command -v cosign >/dev/null 2>&1; then
    STAGE=image-pull
    identity="https://github.com/tastyeffectco/sandboxd/.github/workflows/release-images.yml@refs/tags/$release_tag"
    for ref in "$base_ref" "$plane_ref" "$console_ref"; do
      [ "$CONSOLE" = 1 ] || [ "$ref" != "$console_ref" ] || continue
      if ! $DOCKER pull "$ref"; then verified=0; break; fi
      digest="$($DOCKER image inspect --format '{{index .RepoDigests 0}}' "$ref")" || { verified=0; break; }
      # Verify the digest actually pulled, not a mutable remote tag.
      case "$digest" in "${ref%:*}@sha256:"*) ;; *) verified=0; break ;; esac
      if ! cosign verify --certificate-identity "$identity" \
          --certificate-oidc-issuer https://token.actions.githubusercontent.com \
          "$digest" >/dev/null; then verified=0; break; fi
    done
    if [ "$verified" = 1 ]; then
      USE_PULL=1
      BASE_IMAGE="$base_ref"
      export SANDBOXD_IMAGE="$base_ref" SANDBOXD_CONTROL_PLANE_IMAGE="$plane_ref"
      export SANDBOXD_CONSOLE_IMAGE="$console_ref"
      ok "release images pulled and signatures verified"
    elif [ "$MODE" = pull ]; then
      die "Release images could not be pulled and verified; the stack was not started."
    else
      info "verified release images unavailable — building from source"
    fi
  elif [ "$MODE" = auto ]; then
    info "using source builds (prebuilt images require a clean release tag and cosign)"
  fi

  if [ "$USE_PULL" = 0 ]; then
    # Never reuse a partially pulled image after verification failed.
    BASE_IMAGE="${SANDBOXD_IMAGE:-sandboxd-base:0.3.0}"
    export SANDBOXD_IMAGE="$BASE_IMAGE"
    export SANDBOXD_CONTROL_PLANE_IMAGE="${SANDBOXD_CONTROL_PLANE_IMAGE:-sandboxd-control-plane:0.3.0}"
    export SANDBOXD_CONSOLE_IMAGE="${SANDBOXD_CONSOLE_IMAGE:-sandboxd-console:0.3.0}"
    STAGE=base-image
    DOCKER="$DOCKER" SANDBOXD_IMAGE="$BASE_IMAGE" bash image/build.sh
    STAGE=stack
    $COMPOSE $PROFILE_ARGS build
  fi
}
