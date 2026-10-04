#!/usr/bin/env bash
set -euo pipefail

# The working-tree protobuf API must stay wire- and source-compatible with the default branch.
source .github/scripts/codegen-images.sh
against="${BUF_BREAKING_AGAINST:-.git#ref=refs/remotes/origin/main}"
docker run --rm --user "$(id -u):$(id -g)" --env HOME=/tmp \
  --mount "type=bind,source=$PWD,target=/src,readonly" --workdir /src \
  "$BUF_IMAGE" breaking --against "$against"
echo "Protobuf API is compatible with $against"
