#!/usr/bin/env bash
set -euo pipefail

# Regenerate Connect/protobuf and sqlc code in the given directory with the pinned generator images.
source "$(dirname "$0")/codegen-images.sh"
workspace="$(cd "${1:-.}" && pwd)"
run_generator() {
  docker run --rm --user "$(id -u):$(id -g)" --env HOME=/tmp \
    --mount "type=bind,source=$workspace,target=/src" --workdir /src "$@"
}
run_generator "$BUF_IMAGE" generate
run_generator "$SQLC_IMAGE" generate
