#!/usr/bin/env bash
set -euo pipefail

# Fence publication against tag changes while retaining the admitted checkout.
script_directory=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

# Refuse publication without emitting success data on standard output.
refuse_release_verification() {
  printf 'Release source verification refused: %s\n' "$1" >&2
  exit 1
}

[[ $# == 3 ]] || refuse_release_verification 'expected TAG SOURCE_SHA TAG_OID'
tag=$1
source_sha=$2
tag_oid=$3
bash "$script_directory/validate-release-tag.sh" "$tag"
object_id_pattern='^([0-9a-f]{40}|[0-9a-f]{64})$'
[[ $source_sha =~ $object_id_pattern && $tag_oid =~ $object_id_pattern ]] ||
  refuse_release_verification 'source and tag must be full lowercase object IDs'
checkout_commit=$(git rev-parse --verify --end-of-options 'HEAD^{commit}' 2>/dev/null) || refuse_release_verification 'checkout must resolve to a commit'
[[ $checkout_commit == "$source_sha" ]] || refuse_release_verification 'checkout does not match the admitted source'
private_tag=refs/portcullis/release-verification/tag
git fetch --atomic --no-tags --no-write-fetch-head --refmap= origin "+refs/tags/$tag:$private_tag" >&2 ||
  refuse_release_verification 'origin must still provide the admitted release tag'
current_tag_oid=$(git rev-parse --verify --end-of-options "$private_tag^{object}" 2>/dev/null) || refuse_release_verification 'release tag object is unavailable'
[[ $current_tag_oid == "$tag_oid" ]] || refuse_release_verification 'release tag object changed after admission'
current_commit=$(git rev-parse --verify --end-of-options "$private_tag^{commit}" 2>/dev/null) || refuse_release_verification 'release tag must resolve to a commit'
[[ $current_commit == "$source_sha" ]] || refuse_release_verification 'release tag commit changed after admission'
