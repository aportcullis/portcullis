#!/usr/bin/env bash
set -euo pipefail

# Admit one exact event commit at its freshly observed matching release-branch tip.
script_directory=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

# Refuse admission without emitting a partial machine-readable success result.
refuse_release_source() {
  printf 'Release source refused: %s\n' "$1" >&2
  exit 1
}

[[ $# == 2 ]] || refuse_release_source 'expected TAG EVENT_COMMIT'
tag=$1
event_commit=$2
bash "$script_directory/validate-release-tag.sh" "$tag"
object_id_pattern='^([0-9a-f]{40}|[0-9a-f]{64})$'
[[ $event_commit =~ $object_id_pattern ]] || refuse_release_source 'event commit must be a full lowercase object ID'
checkout_commit=$(git rev-parse --verify --end-of-options 'HEAD^{commit}' 2>/dev/null) || refuse_release_source 'checkout must resolve to a commit'
[[ $checkout_commit == "$event_commit" ]] || refuse_release_source 'checkout does not match the event commit'
version=${tag#v}
major=${version%%.*}
remaining=${version#*.}
minor=${remaining%%.*}
release_branch="release/v$major.$minor"
private_tag=refs/portcullis/release-admission/tag
private_branch=refs/portcullis/release-admission/branch
git fetch --atomic --no-tags --no-write-fetch-head --refmap= origin \
  "+refs/tags/$tag:$private_tag" "+refs/heads/$release_branch:$private_branch" >&2 ||
  refuse_release_source 'origin must provide the exact release tag and matching branch'
tag_oid=$(git rev-parse --verify --end-of-options "$private_tag^{object}" 2>/dev/null) || refuse_release_source 'release tag object is unavailable'
source_sha=$(git rev-parse --verify --end-of-options "$private_tag^{commit}" 2>/dev/null) || refuse_release_source 'release tag must resolve to a commit'
branch_commit=$(git rev-parse --verify --end-of-options "$private_branch^{commit}" 2>/dev/null) || refuse_release_source 'release branch must resolve to a commit'
[[ $tag_oid =~ $object_id_pattern ]] || refuse_release_source 'release tag must have a full lowercase object ID'
[[ $source_sha == "$event_commit" ]] || refuse_release_source 'release tag does not match the event commit'
[[ $branch_commit == "$source_sha" ]] || refuse_release_source 'release tag is not the matching branch tip'
printf 'source_sha=%s\ntag_oid=%s\nrelease_branch=%s\n' "$source_sha" "$tag_oid" "$release_branch"
