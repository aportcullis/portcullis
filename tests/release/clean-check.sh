#!/usr/bin/env bash
set -euo pipefail

# make clean removes build outputs only; it must never delete committed generated sources.
clone="$(mktemp -d)"
trap 'rm -rf "$clone"' EXIT
git clone --quiet --no-hardlinks . "$clone"
cp Makefile "$clone/Makefile"
mkdir -p "$clone/bin" "$clone/internal/platform/assets/dist/assets"
echo binary > "$clone/bin/portcullis"
echo built > "$clone/internal/platform/assets/dist/index.html"
echo built > "$clone/internal/platform/assets/dist/assets/app.js"
make --no-print-directory -C "$clone" clean >/dev/null

failures=0
deleted="$(git -C "$clone" ls-files --deleted)"
if [ -n "$deleted" ]; then
  echo "make clean deleted committed files:" >&2
  echo "$deleted" | head -5 >&2
  failures=$((failures + 1))
fi
for kept in web/src/gen gen internal/infra/postgres/db internal/platform/assets/dist/.gitkeep; do
  if [ ! -e "$clone/$kept" ]; then
    echo "make clean removed committed $kept" >&2
    failures=$((failures + 1))
  fi
done
for removed in bin/portcullis internal/platform/assets/dist/index.html internal/platform/assets/dist/assets/app.js; do
  if [ -e "$clone/$removed" ]; then
    echo "make clean left build output $removed" >&2
    failures=$((failures + 1))
  fi
done
if [ "$failures" -ne 0 ]; then
  exit 1
fi
echo 'make clean removes build outputs and keeps committed generated sources'
