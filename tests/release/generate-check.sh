#!/usr/bin/env bash
set -euo pipefail

# Generated code, protobuf style and module files must match their sources; regenerate in a disposable copy so the working tree is never rewritten.
source .github/scripts/codegen-images.sh
copy="$(mktemp -d)"
trap 'rm -rf "$copy"' EXIT
git ls-files --cached --others --exclude-standard -z | while IFS= read -r -d '' path; do
  if [ -e "$path" ]; then
    mkdir -p "$copy/$(dirname "$path")"
    cp -p "$path" "$copy/$path"
  fi
done

failures=0
bash .github/scripts/generate.sh "$copy"
for generated in gen web/src/gen internal/infra/postgres/db; do
  if ! diff -r "$generated" "$copy/$generated" >/dev/null; then
    echo "generated code in $generated differs from its sources; run make generate" >&2
    diff -r "$generated" "$copy/$generated" | head -10 >&2 || true
    failures=$((failures + 1))
  fi
done
if ! docker run --rm --mount "type=bind,source=$copy,target=/src,readonly" --workdir /src "$BUF_IMAGE" lint; then
  echo "buf lint failed" >&2
  failures=$((failures + 1))
fi
if ! go mod tidy -diff >/dev/null; then
  echo "go.mod or go.sum is not tidy; run go mod tidy" >&2
  failures=$((failures + 1))
fi
if [ "$failures" -ne 0 ]; then
  exit 1
fi
echo 'Generated code, protobuf lint and module files match their sources'
