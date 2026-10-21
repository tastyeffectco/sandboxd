#!/usr/bin/env bash
# Install the signed macOS desktop client. Called by install.sh after the engine.
# Separate process: a failed optional desktop download never rolls back an engine.
set -euo pipefail
[ "$(uname -s)" = Darwin ] || { echo 'The native desktop installer supports macOS and Windows.' >&2; exit 1; }
VERSION="${SANDBOXD_DESKTOP_VERSION:-0.3.24}"
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'Invalid desktop version.' >&2; exit 1; }
case "$(uname -m)" in arm64) ARCH=arm64 ;; x86_64) ARCH=x64 ;; *) echo 'Unsupported Mac architecture.' >&2; exit 1 ;; esac
BASE="https://github.com/tastyeffectco/sandboxd/releases/download/desktop-v$VERSION"
ASSET="sandboxd-desktop-$VERSION-mac-$ARCH.zip"
TMP="$(mktemp -d "${TMPDIR:-/tmp}/sandboxd-desktop.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT
curl --proto '=https' --tlsv1.2 -fsSL "$BASE/$ASSET" -o "$TMP/$ASSET"
curl --proto '=https' --tlsv1.2 -fsSL "$BASE/SHA256SUMS" -o "$TMP/SHA256SUMS"
EXPECTED="$(awk -v name="$ASSET" '$2 == name {print $1}' "$TMP/SHA256SUMS")"
[[ "$EXPECTED" =~ ^[a-fA-F0-9]{64}$ ]] || { echo 'Missing or invalid desktop checksum.' >&2; exit 1; }
ACTUAL="$(shasum -a 256 "$TMP/$ASSET" | awk '{print $1}')"
[ "$ACTUAL" = "$EXPECTED" ] || { echo 'Desktop checksum mismatch.' >&2; exit 1; }
ditto -x -k "$TMP/$ASSET" "$TMP/unpacked"
APP="$TMP/unpacked/sandboxd.app"
codesign --verify --deep --strict "$APP"
spctl --assess --type execute "$APP"
DEST="$HOME/Applications/sandboxd.app"
# Do not overwrite an existing installation or a running app implicitly.
if [ -e "$DEST" ]; then
  echo "sandboxd is already installed at $DEST. Use the release DMG to update it."
  open "$DEST"
  exit 0
fi
mkdir -p "$HOME/Applications"
ditto "$APP" "$DEST"
open "$DEST"
printf 'Desktop installed: %s\n' "$DEST"
