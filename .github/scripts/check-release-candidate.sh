#!/usr/bin/env bash
set -euo pipefail

script_directory=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

# Refuse a candidate without admitting an unverified artifact.
refuse_release_candidate() {
  printf 'Release candidate refused: %s\n' "$1" >&2
  exit 1
}

[[ $# == 4 ]] || refuse_release_candidate 'expected IMAGE_REPOSITORY DIGEST PLATFORM RELEASE_TAG'
image_repository=$1
candidate_digest=$2
platform=$3
release_tag=$4
repository_pattern='^[a-z0-9][a-z0-9.-]*(:[0-9]+)?/[a-z0-9][a-z0-9._/-]*$'
[[ $image_repository =~ $repository_pattern ]] || refuse_release_candidate 'expected a lowercase qualified repository without a tag'
[[ $candidate_digest =~ ^sha256:[0-9a-f]{64}$ ]] || refuse_release_candidate 'expected a full lowercase SHA-256 digest'
case "$platform" in linux/amd64|linux/arm64) ;; *) refuse_release_candidate 'unsupported candidate platform' ;; esac
bash "$script_directory/validate-release-tag.sh" "$release_tag"
native_platform=$(timeout 15s docker info --format '{{.OSType}}/{{.Architecture}}') || refuse_release_candidate 'Docker daemon platform is unavailable'
case "$native_platform" in
  linux/arm64|linux/aarch64) native_platform=linux/arm64 ;;
  linux/amd64|linux/x86_64) native_platform=linux/amd64 ;;
  *) refuse_release_candidate 'unsupported Docker daemon platform' ;;
esac
[[ $native_platform == "$platform" ]] || refuse_release_candidate 'candidate must execute on its native Docker daemon platform'

project_root=$(cd "$script_directory/../.." && pwd)
mkdir -p "$project_root/.test-docker"
fixture=$(mktemp -d "${TMPDIR:-$project_root/.test-docker}/portcullis-release-candidate.XXXXXX")
fixture_name=${fixture##*/}
containers=("$fixture_name-config" "$fixture_name-version")

# Remove only this invocation's containers and files, preserving shared pulled images.
cleanup_release_candidate() {
  local container cleanup_failed=0
  for container in "${containers[@]}"; do
    if timeout 15s docker container inspect "$container" >/dev/null 2> "$fixture/cleanup-error"; then
      if ! timeout 30s docker container rm --force "$container" >/dev/null; then cleanup_failed=1; fi
    elif ! grep -Fq "No such container: $container" "$fixture/cleanup-error"; then
      cat "$fixture/cleanup-error" >&2
      cleanup_failed=1
    fi
    if timeout 15s docker container inspect "$container" >/dev/null 2> "$fixture/cleanup-error"; then
      echo 'Owned release candidate container remains' >&2
      cleanup_failed=1
    elif ! grep -Fq "No such container: $container" "$fixture/cleanup-error"; then
      cat "$fixture/cleanup-error" >&2
      cleanup_failed=1
    fi
  done
  [[ $cleanup_failed == 0 ]] || return 1
  find "$fixture" -depth -delete
  [[ ! -e $fixture ]]
}

trap cleanup_release_candidate EXIT
trap 'exit 130' INT TERM
image_reference="$image_repository@$candidate_digest"
timeout 180s docker pull --platform "$platform" "$image_reference"
actual_platform=$(timeout 15s docker image inspect --format '{{.Os}}/{{.Architecture}}' "$image_reference")
[[ $actual_platform == "$platform" ]] || refuse_release_candidate 'pulled image platform does not match the candidate platform'
runtime_user=$(timeout 15s docker image inspect --format '{{.Config.User}}' "$image_reference")
case "$runtime_user" in 65532|65532:65532|nonroot) ;; *) refuse_release_candidate 'candidate runtime must use the expected non-root user' ;; esac

status=0
timeout 30s docker run --name "${containers[0]}" --rm --pull=never --platform "$platform" \
  --network none --read-only --cap-drop ALL --security-opt no-new-privileges \
  --env PORTCULLIS_LOG_FORMAT=invalid "$image_reference" > "$fixture/config.log" 2>&1 || status=$?
if [[ $status != 1 ]] || ! grep -Fq 'config load failed' "$fixture/config.log" || \
  ! grep -Fq 'invalid log_format' "$fixture/config.log" || \
  grep -Fq '"msg":"starting"' "$fixture/config.log" || grep -Fq '"version":' "$fixture/config.log"; then
  refuse_release_candidate 'candidate did not preserve invalid-configuration refusal'
fi

status=0
timeout 30s docker run --name "${containers[1]}" --rm --pull=never --platform "$platform" \
  --network none --read-only --cap-drop ALL --security-opt no-new-privileges \
  --env PORTCULLIS_LOG_FORMAT=json --env BUILD_VERSION=runtime-version-spoof \
  --env PORTCULLIS_BUILD_VERSION=runtime-version-spoof \
  "$image_reference" > "$fixture/version.log" 2>&1 || status=$?
first_record=$(head -n 1 "$fixture/version.log")
startup_count=$(awk 'index($0, "\"msg\":\"starting\"") { count++ } END { print count + 0 }' "$fixture/version.log")
later_versions=$(awk 'NR > 1 && index($0, "\"version\":") { count++ } END { print count + 0 }' "$fixture/version.log")
expected_startup="\"level\":\"INFO\",\"msg\":\"starting\",\"version\":\"$release_tag\""
if [[ $status != 1 || $startup_count != 1 || $later_versions != 0 || $first_record != *"$expected_startup"* ]] || \
  ! grep -Fq '"msg":"master key required"' "$fixture/version.log" || \
  grep -Fq runtime-version-spoof "$fixture/version.log"; then
  refuse_release_candidate 'candidate did not preserve its exact linked startup identity and key refusal'
fi

cleanup_release_candidate
trap - EXIT
printf 'Native release candidate passed: %s (%s, %s)\n' "$image_reference" "$platform" "$release_tag"
