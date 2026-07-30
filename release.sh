#!/usr/bin/env bash
#
# sandboxd — cut a release: changelog + annotated tag + GitHub release.
#
#   ./release.sh                  next patch (v0.3.0 → v0.3.1), notes auto-generated
#   ./release.sh v0.4.0           explicit version (milestone/minor releases)
#   ./release.sh --title "…"      release title (default "vX.Y.Z")
#   ./release.sh --dry-run        print what would happen; change nothing
#
# What it does, in order:
#   1. sanity: on main, clean tree, up to date with origin
#   2. compute the next version from the latest v* tag
#   3. generate release notes from merged PR titles (GitHub generate-notes API)
#   4. prepend a matching section to CHANGELOG.md and commit it
#   5. annotated tag vX.Y.Z on that commit; push commit + tag
#   6. create the GitHub release with those notes
#
# Requires: git, gh (authenticated). Tags also trigger the prebuilt-image
# workflow once .github/workflows/release-images.yml is on main.

set -euo pipefail
cd "$(dirname "$0")"

bold() { printf '\033[1m%s\033[0m\n' "$*"; }
info() { printf '  \033[36m›\033[0m %s\n' "$*"; }
ok()   { printf '  \033[32m✓\033[0m %s\n' "$*"; }
die()  { printf '  \033[31m✗ %s\033[0m\n' "$*" >&2; exit 1; }

command -v git >/dev/null || die "git is required"
command -v gh  >/dev/null || die "gh (GitHub CLI) is required"

REPO="${SANDBOXD_REPO:-tastyeffectco/sandboxd}"
REMOTE_URL="${RELEASE_REMOTE_URL:-origin}"   # override to push with an explicit token URL

VERSION="" TITLE="" DRY=0
while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY=1 ;;
    --title)   shift; TITLE="${1:-}" ;;
    v[0-9]*)   VERSION="$1" ;;
    *) die "unknown argument: $1" ;;
  esac
  shift
done

# ── 1. sanity ────────────────────────────────────────────────────────
BRANCH="$(git rev-parse --abbrev-ref HEAD)"
[ "$BRANCH" = "main" ] || die "release from main (currently on $BRANCH)"
git diff --quiet && git diff --cached --quiet || die "working tree not clean"
git fetch -q origin main
[ "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)" ] || \
  die "main is not in sync with origin/main — pull/push first"

# ── 2. version ───────────────────────────────────────────────────────
LAST="$(git tag -l 'v*' --sort=-v:refname | head -1)"
[ -n "$LAST" ] || die "no existing v* tag found"
if [ -z "$VERSION" ]; then
  VERSION="$(printf '%s' "$LAST" | awk -F. -v OFS=. '{ $NF=$NF+1; print }')"
fi
case "$VERSION" in v[0-9]*.[0-9]*.[0-9]*) ;; *) die "version must look like vX.Y.Z (got $VERSION)";; esac
git rev-parse -q --verify "refs/tags/$VERSION" >/dev/null && die "tag $VERSION already exists"
[ -n "$TITLE" ] || TITLE="$VERSION"

bold "release $VERSION  (previous: $LAST)"

# ── 3. notes from merged PRs ─────────────────────────────────────────
NOTES="$(gh api "repos/$REPO/releases/generate-notes" \
          -f tag_name="$VERSION" -f previous_tag_name="$LAST" -f target_commitish=main \
          --jq .body)"
[ -n "$NOTES" ] || die "could not generate release notes"
info "generated notes ($(printf '%s' "$NOTES" | grep -c '^\*') PRs since $LAST)"

if [ "$DRY" = 1 ]; then
  bold "--dry-run — would tag $VERSION with:"
  printf '%s\n' "$NOTES"
  exit 0
fi

# ── 4. CHANGELOG.md ──────────────────────────────────────────────────
DATE="$(date +%Y-%m-%d)"
BODY="$(printf '%s\n' "$NOTES" | sed -e 's/^## What.s Changed$//' -e 's/^##/###/')"
tmp="$(mktemp)"
awk -v ver="${VERSION#v}" -v date="$DATE" -v body="$BODY" '
  BEGIN { inserted = 0 }
  /^## \[/ && !inserted { print "## [" ver "] — " date "\n" body "\n"; inserted = 1 }
  { print }
  END { if (!inserted) print "## [" ver "] — " date "\n" body }
' CHANGELOG.md > "$tmp" && mv "$tmp" CHANGELOG.md
git add CHANGELOG.md
git commit -q -m "release: $VERSION"
ok "CHANGELOG.md updated + committed"

# ── 5. tag + push ────────────────────────────────────────────────────
git tag -a "$VERSION" -m "$TITLE"
git push -q "$REMOTE_URL" main "refs/tags/$VERSION"
ok "pushed main + tag $VERSION"

# ── 6. GitHub release ────────────────────────────────────────────────
printf '%s\n' "$NOTES" | gh release create "$VERSION" --repo "$REPO" \
  --title "$TITLE" --notes-file - >/dev/null
ok "release published: https://github.com/$REPO/releases/tag/$VERSION"
info "installs on older versions now see the console update banner and ./upgrade.sh --check"
