#!/usr/bin/env bash
set -euo pipefail

# Generate notes offline from a read-only project using a pinned official tool.
source .github/scripts/git-cliff-image.sh
exec docker run --rm --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges --user "$(id -u):$(id -g)" \
  --tmpfs /tmp:rw,noexec,nosuid,size=16m --env HOME=/tmp \
  --mount "type=bind,source=$PWD,target=/app,readonly" --workdir /app \
  "$GIT_CLIFF_IMAGE" --config /app/cliff.toml --offline --no-exec "$@"
