#!/bin/bash
# <UDF name="preview_domain" label="Preview domain (e.g. sandboxd.example.com) — leave blank to use the server IP" default="" example="sandboxd.example.com" />
# <UDF name="api_token" label="API token to require auth (optional) — blank leaves the API on loopback with auth off" default="" />
#
# sandboxd — Linode StackScript. Create a StackScript from this file, then deploy
# a Linode from it. It installs Docker + sandboxd and (optionally) wires your
# preview domain and API auth. install.sh is the source of truth for the install.
set -euo pipefail

SRC="/opt/sandboxd"
BRANCH="main"

# UDF values are exported into the environment as PREVIEW_DOMAIN and API_TOKEN.

# .env key setter: replace in place, or append if the key is absent.
set_env() {
  local key="$1" val="$2"
  if grep -q "^${key}=" .env; then
    sed -i "s#^${key}=.*#${key}=${val}#" .env
  else
    printf '%s=%s\n' "$key" "$val" >>.env
  fi
}

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq || true
apt-get install -y -qq curl git ca-certificates openssl >/dev/null 2>&1 || true

command -v docker >/dev/null 2>&1 || curl -fsSL https://get.docker.com | sh
systemctl enable --now docker >/dev/null 2>&1 || true

git clone --depth 1 --branch "$BRANCH" -q https://github.com/tastyeffectco/sandboxd.git "$SRC"
cd "$SRC"
cp .env.example .env

if [ -n "${PREVIEW_DOMAIN:-}" ]; then
  set_env PREVIEW_DOMAIN "$PREVIEW_DOMAIN"
fi

if [ -n "${API_TOKEN:-}" ]; then
  set_env SANDBOXD_API_AUTH_DISABLED false
  set_env SANDBOXD_API_TOKENS "admin=${API_TOKEN}"
fi

./install.sh
