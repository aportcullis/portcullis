#!/usr/bin/env bash
set -euo pipefail

# Run all mutable fixtures in bounded tmpfs; never create test files on the host.
source .github/scripts/git-cliff-image.sh
run_id="$(date +%s)-$$-$RANDOM"
image="portcullis-changelog-check:$run_id"
container="portcullis-changelog-check-$run_id"
docker build --build-arg "GIT_CLIFF_IMAGE=$GIT_CLIFF_IMAGE" \
  --tag "$image" --file tests/release/Dockerfile.changelog tests/release
cleanup_changelog_resources() {
  if docker container inspect "$container" >/dev/null 2>&1; then
    docker rm -f "$container" >/dev/null
  fi
  docker image rm "$image" >/dev/null
}
trap cleanup_changelog_resources EXIT
docker run --name "$container" --rm --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges --user "$(id -u):$(id -g)" \
  --tmpfs "/app:rw,noexec,nosuid,size=64m,uid=$(id -u),gid=$(id -g),mode=0700" \
  --tmpfs /tmp:rw,noexec,nosuid,size=16m --env HOME=/tmp \
  --mount "type=bind,source=$PWD,target=/source,readonly" --workdir /app \
  "$image" /source/tests/release/changelog-scenario.sh
cleanup_changelog_resources
trap - EXIT
echo 'Changelog container and image cleanup passed'
