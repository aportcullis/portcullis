#!/usr/bin/env bash
set -euo pipefail

# Generate notes offline from a read-only project using a pinned official tool.
image='ghcr.io/orhun/git-cliff/git-cliff:2.14.2@sha256:1e696a381a1b0366bb6ccbb6bd4e50e73473fda88eab3a757b95e0d0d66c9531'
exec docker run --rm --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges --user "$(id -u):$(id -g)" \
  --tmpfs /tmp:rw,noexec,nosuid,size=16m --env HOME=/tmp \
  --mount "type=bind,source=$PWD,target=/app,readonly" --workdir /app \
  "$image" --config /app/cliff.toml --offline --no-exec "$@"
