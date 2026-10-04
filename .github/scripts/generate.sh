#!/usr/bin/env bash
set -euo pipefail

# Regenerate Connect/protobuf code with the pinned go-tool buf and local plugins, and sqlc code with the pinned image, in the given directory.
source "$(dirname "$0")/codegen-images.sh"
workspace="$(cd "${1:-.}" && pwd)"
(cd "$workspace" && go tool buf generate)
docker run --rm --user "$(id -u):$(id -g)" --env HOME=/tmp \
  --mount "type=bind,source=$workspace,target=/src" --workdir /src "$SQLC_IMAGE" generate
