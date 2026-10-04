#!/usr/bin/env bash
set -euo pipefail

# The working-tree protobuf API must stay wire- and source-compatible with the default branch.
against="${BUF_BREAKING_AGAINST:-.git#ref=refs/remotes/origin/main}"
go tool buf breaking --against "$against"
echo "Protobuf API is compatible with $against"
