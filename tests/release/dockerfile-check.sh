#!/usr/bin/env bash
set -euo pipefail

# Every tracked Dockerfile and accepted fixture must pass Docker build checks; every refused fixture must fail them.
source .github/scripts/git-cliff-image.sh
fixtures=tests/release/dockerfile-fixtures

# build_context_arguments prints the named build contexts a Dockerfile resolves its base images from.
build_context_arguments() {
  case "$1" in
    tests/release/Dockerfile.changelog) echo "--build-context git-cliff=docker-image://$GIT_CLIFF_IMAGE" ;;
    "$fixtures"/accepted/named-build-context.Dockerfile) echo "--build-context base-image=docker-image://busybox:1.37" ;;
  esac
}

check_dockerfile() {
  local contexts
  contexts="$(build_context_arguments "$1")"
  # shellcheck disable=SC2086 # contexts holds whole flag/value words without spaces inside values.
  docker build --check --quiet $contexts --file "$1" "$fixtures" >/dev/null 2>&1
}

failures=0
while IFS= read -r dockerfile; do
  if ! check_dockerfile "$dockerfile"; then
    echo "Dockerfile check failed: $dockerfile" >&2
    failures=$((failures + 1))
  fi
done < <(git ls-files | grep -E '(^|/)Dockerfile[^/]*$'; ls "$fixtures"/accepted/*.Dockerfile)
for refused in "$fixtures"/refused/*.Dockerfile; do
  if check_dockerfile "$refused"; then
    echo "Dockerfile check accepted a refused fixture: $refused" >&2
    failures=$((failures + 1))
  fi
done
if [ "$failures" -ne 0 ]; then
  exit 1
fi
echo 'Dockerfile checks passed for tracked files and fixtures'
