#!/usr/bin/env bash
# verify-base.sh — assert the sandboxd base image has the toolchains the
# presets + native-module builds rely on. Runs INSIDE the image:
#
#   docker run --rm <image> bash /opt/verify-base.sh
#
# (image/build.sh copies this to /opt/verify-base.sh; or bind-mount it.)
# Exits non-zero on the first failed check so it works as a CI gate.
set -u
fail=0
ok()   { printf '  ok   %s\n' "$1"; }
bad()  { printf '  FAIL %s\n' "$1"; fail=1; }

check() { # <label> <command...>
  local label="$1"; shift
  if "$@" >/dev/null 2>&1; then ok "$label"; else bad "$label"; fi
}

echo "sandboxd base-image verification"

# Node must be 22.x (Finding B: <22 / <20.19 lacks worker_threads.markAsUncloneable).
node_major="$(node -p 'process.versions.node.split(".")[0]' 2>/dev/null || echo 0)"
if [ "$node_major" = "22" ]; then ok "node $(node --version)"; else bad "node is $(node --version 2>/dev/null) (want 22.x)"; fi
# markAsUncloneable present (the undici-8 crash class).
check "node worker_threads.markAsUncloneable" node -e 'require("worker_threads").markAsUncloneable||process.exit(1)'

check "npm    $(npm --version 2>/dev/null)"  npm --version
check "pnpm   $(pnpm --version 2>/dev/null)" pnpm --version
# pnpm must be >= 10 (catalog-only workspaces; Ghost 6 etc.)
pnpm_major="$(pnpm --version 2>/dev/null | cut -d. -f1)"
if [ "${pnpm_major:-0}" -ge 10 ] 2>/dev/null; then ok "pnpm major >= 10"; else bad "pnpm major is ${pnpm_major:-?} (want >= 10)"; fi
check "bun    $(bun --version 2>/dev/null)"  bun --version
check "python3 $(python3 --version 2>/dev/null)" python3 --version
check "uv     $(uv --version 2>/dev/null)"   uv --version

# Finding A: node-gyp on Python 3.12+ needs distutils, which setuptools provides.
check "python distutils (node-gyp)" python3 -c 'import distutils.version; distutils.version.StrictVersion'
check "python setuptools"           python3 -c 'import setuptools'

# Global npm prefix must be user-writable (default /usr is root-owned). Checked under
# a LOGIN shell (`bash -lc`) because runtimed runs app/agent commands that way, which
# is where NPM_CONFIG_PREFIX is set.
npm_prefix="$(bash -lc 'npm config get prefix' 2>/dev/null)"
if [ "$npm_prefix" != "/usr" ] && [ -n "$npm_prefix" ]; then ok "npm global prefix is $npm_prefix (not /usr)"; else bad "npm global prefix is '$npm_prefix' (root-owned /usr)"; fi
# prefix bin dir is on the login PATH
check "npm global bin on login PATH" bash -lc 'case ":$PATH:" in *":$(npm config get prefix)/bin:"*) exit 0;; *) exit 1;; esac'
# prefix is actually writable by this (sandbox) user — proves `npm install -g` works
check "npm global prefix writable"   bash -lc 'p=$(npm config get prefix); mkdir -p "$p/bin" && touch "$p/bin/.probe" && rm -f "$p/bin/.probe"'

check "make"  make --version
check "gcc"   gcc --version
check "g++"   g++ --version
check "git"   git --version
check "curl"  curl --version

if [ "$fail" -eq 0 ]; then echo "ALL OK"; else echo "VERIFY FAILED"; fi
exit "$fail"
