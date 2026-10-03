#!/usr/bin/env bash
set -euo pipefail

# Replace the reviewed snapshot only after complete generation succeeds.
notes=$(mktemp ./CHANGELOG.md.XXXXXX)
trap 'rm -f "$notes"' EXIT
if [[ -n ${RELEASE_TAG:-} ]]; then
  bash .github/scripts/validate-release-tag.sh "$RELEASE_TAG"
  bash .github/scripts/git-cliff.sh --tag "$RELEASE_TAG" > "$notes"
else
  bash .github/scripts/git-cliff.sh > "$notes"
fi
chmod 0644 "$notes"
mv "$notes" CHANGELOG.md
