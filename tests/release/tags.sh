#!/usr/bin/env bash
set -euo pipefail

# Verify release admission before any registry credentials are available.
validator=.github/scripts/validate-release-tag.sh
for tag in v0.0.0 v1.2.3 v12.34.56 v1.2.3-rc.1 v1.2.3-alpha v1.2.3-0 v1.2.3-x-y.9; do
  if ! bash "$validator" "$tag"; then
    echo "Valid release rejected: $tag" >&2
    exit 1
  fi
done
for tag in '' vbanana 1.2.3 v1.2 v01.2.3 v1.02.3 v1.2.03 v1.2.3-01 v1.2.3-rc.01 v1.2.3- v1.2.3-a..b v1.2.3+build v1.2.3/foo 'v1.2.3;echo bad'; do
  if bash "$validator" "$tag" >/dev/null 2>&1; then
    echo "Invalid release accepted: $tag" >&2
    exit 1
  fi
done
printf -v long_identifier '%0130d' 1
if bash "$validator" "v1.2.3-x$long_identifier" >/dev/null 2>&1; then
  echo 'Oversized Docker tag accepted' >&2
  exit 1
fi
echo 'Release tag admission scenarios passed'
