#!/usr/bin/env bash
# sync_spec.sh - keep the pinned copy of Autonomat's OpenAPI contract current.
#
#   scripts/sync_spec.sh           download docs/api_spec.yaml from dotdevlabs/autonomat
#                                  and record the commit SHA it came from
#   scripts/sync_spec.sh --check   exit 1 when the pinned copy or its SHA is stale
#
# The contract lives in a private repository, so a token is required:
# GITHUB_TOKEN, or the one `gh auth token` prints. Without a token --check
# fails (it never passes silently); set AUTONOMATCTL_ALLOW_SPEC_SKIP=1 to skip
# the check explicitly, e.g. for a fork whose CI has no access to the source.
#
# Test seams: SPEC_API_BASE (API host), SPEC_DEST (pinned spec path).
set -euo pipefail

cd "$(dirname "$0")/.."

REPO="dotdevlabs/autonomat"
FILE="docs/api_spec.yaml"
API="${SPEC_API_BASE:-https://api.github.com}"
DEST="${SPEC_DEST:-internal/schema/testdata/api_spec.yaml}"
SHA_DEST="${DEST%.yaml}.sha"

MODE="sync"
case "${1:-}" in
  "") ;;
  --check) MODE="check" ;;
  *) echo "usage: $0 [--check]" >&2; exit 2 ;;
esac

TOKEN="${GITHUB_TOKEN:-}"
if [ -z "$TOKEN" ] && command -v gh >/dev/null 2>&1; then
  TOKEN="$(gh auth token 2>/dev/null || true)"
fi
if [ -z "$TOKEN" ]; then
  if [ "$MODE" = "check" ] && [ "${AUTONOMATCTL_ALLOW_SPEC_SKIP:-}" = "1" ]; then
    echo "WARNING: no GITHUB_TOKEN; spec check skipped (AUTONOMATCTL_ALLOW_SPEC_SKIP=1)" >&2
    exit 0
  fi
  echo "ERROR: GITHUB_TOKEN is not set (and gh is not logged in); cannot reach ${REPO}." >&2
  echo "Set GITHUB_TOKEN to a token that can read ${REPO}." >&2
  exit 1
fi

TMPDIR_="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_"' EXIT

fetch() { # url accept outfile
  curl -fsS --retry 2 \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Accept: $2" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    "$1" -o "$3" || { echo "ERROR: GET $1 failed" >&2; exit 1; }
}

fetch "${API}/repos/${REPO}/contents/${FILE}" "application/vnd.github.raw+json" "$TMPDIR_/spec.yaml"
fetch "${API}/repos/${REPO}/commits?path=${FILE}&per_page=1" "application/vnd.github+json" "$TMPDIR_/commits.json"

# The first "sha" of the first commit object is the commit itself.
LIVE_SHA="$(sed -n 's/.*"sha": *"\([0-9a-f]\{40\}\)".*/\1/p' "$TMPDIR_/commits.json" | head -n 1)"
if [ -z "$LIVE_SHA" ]; then
  echo "ERROR: could not read the commit SHA of ${FILE} from the GitHub API." >&2
  exit 1
fi

if [ "$MODE" = "check" ]; then
  STORED_SHA="$(tr -d '[:space:]' < "$SHA_DEST" 2>/dev/null || true)"
  if ! cmp -s "$DEST" "$TMPDIR_/spec.yaml"; then
    echo "ERROR: ${DEST} is out of sync with ${REPO}:${FILE} (${LIVE_SHA})." >&2
    echo "Run: scripts/sync_spec.sh" >&2
    exit 1
  fi
  if [ "$STORED_SHA" != "$LIVE_SHA" ]; then
    echo "ERROR: ${SHA_DEST} (${STORED_SHA:-missing}) does not match the live commit ${LIVE_SHA}." >&2
    echo "Run: scripts/sync_spec.sh" >&2
    exit 1
  fi
  echo "Spec is in sync with ${REPO}@${LIVE_SHA}."
else
  mkdir -p "$(dirname "$DEST")"
  cp "$TMPDIR_/spec.yaml" "$DEST"
  printf '%s\n' "$LIVE_SHA" > "$SHA_DEST"
  echo "Updated ${DEST} and ${SHA_DEST} (${LIVE_SHA})."
fi
