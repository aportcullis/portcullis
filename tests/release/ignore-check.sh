#!/usr/bin/env bash
set -euo pipefail

# Local secrets and private notes must stay out of git and Docker contexts, while build inputs must remain.
excluded=(
  web/.npmrc
  tests/load/.npmrc
  internal/nested/.npmrc
  tests/load/private/var/cache.json
  .review-cache/mod/cache.txt
  docs/review-2000-01-01.md
  deploy/local/.env
  secrets/demo.key
)
retained=(
  .env.example
  migrations/0001_example.sql
  web/src/gen/portcullis/v1/example_pb.ts
  gen/portcullis/v1/example.pb.go
)

context="$(mktemp -d)"
ignore_repository="$(mktemp -d)"
image="portcullis-ignore-check:$(date +%s)-$$"
cleanup_ignore_check() {
  docker image rm "$image" >/dev/null 2>&1 || true
  rm -rf "$context" "$ignore_repository"
}
trap cleanup_ignore_check EXIT

# Check only the committed .gitignore; a contributor's private exclude file must not make this pass.
git init --quiet "$ignore_repository"
cp .gitignore "$ignore_repository/.gitignore"
failures=0
for path in "${excluded[@]}"; do
  if ! git -C "$ignore_repository" check-ignore --quiet --no-index "$path"; then
    echo "git does not ignore $path" >&2
    failures=$((failures + 1))
  fi
done
for path in "${retained[@]}"; do
  if git -C "$ignore_repository" check-ignore --quiet --no-index "$path"; then
    echo "git ignores required build input $path" >&2
    failures=$((failures + 1))
  fi
done

cp .dockerignore "$context/.dockerignore"
for path in "${excluded[@]}" "${retained[@]}"; do
  mkdir -p "$context/$(dirname "$path")"
  echo marker > "$context/$path"
done
printf 'FROM busybox:1.37\nCOPY . /context\n' > "$context/Dockerfile.ignore-check"
docker build --quiet --tag "$image" --file "$context/Dockerfile.ignore-check" "$context" >/dev/null
copied="$(docker run --rm "$image" find /context -type f)"
for path in "${excluded[@]}"; do
  if grep -qxF "/context/$path" <<<"$copied"; then
    echo "Docker context includes $path" >&2
    failures=$((failures + 1))
  fi
done
for path in "${retained[@]}"; do
  if ! grep -qxF "/context/$path" <<<"$copied"; then
    echo "Docker context omits required build input $path" >&2
    failures=$((failures + 1))
  fi
done
if [ "$failures" -ne 0 ]; then
  exit 1
fi
echo 'Ignore rules exclude local secrets and keep build inputs in git and Docker contexts'
