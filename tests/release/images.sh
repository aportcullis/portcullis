#!/usr/bin/env bash
set -euo pipefail

# Build and execute the distributed entrypoint for each requested image variant.
platforms=${IMAGE_PLATFORMS:-linux/amd64,linux/arm64}
build_version=${BUILD_VERSION-development}
if [[ $build_version != development ]]; then
  bash .github/scripts/validate-release-tag.sh "$build_version"
fi
IFS=',' read -r -a targets <<< "$platforms"
mkdir -p .test-docker
fixture=$(mktemp -d "${TMPDIR:-$PWD/.test-docker}/portcullis-image-check.XXXXXX")
run_id=${fixture##*/}
images=()
containers=()
results=()

# Remove only an owned Docker reference, distinguishing absence from daemon failure.
remove_owned_docker_resource() {
  local resource_kind=$1 resource_name=$2
  if docker "$resource_kind" inspect "$resource_name" >/dev/null 2> "$fixture/cleanup-error"; then
    if ! docker "$resource_kind" rm --force "$resource_name" >/dev/null; then
      return 1
    fi
  elif ! grep -Fq "No such $resource_kind: $resource_name" "$fixture/cleanup-error"; then
    cat "$fixture/cleanup-error" >&2
    return 1
  fi
  if docker "$resource_kind" inspect "$resource_name" >/dev/null 2> "$fixture/cleanup-error"; then
    echo "Owned Docker $resource_kind remains: $resource_name" >&2
    return 1
  elif ! grep -Fq "No such $resource_kind: $resource_name" "$fixture/cleanup-error"; then
    cat "$fixture/cleanup-error" >&2
    return 1
  fi
}

# Verify all owned references and temporary files are gone before reporting success.
cleanup_images() {
  local cleanup_failed=0
  for container in "${containers[@]}"; do
    if ! remove_owned_docker_resource container "$container"; then cleanup_failed=1; fi
  done
  for image in "${images[@]}"; do
    if ! remove_owned_docker_resource image "$image"; then cleanup_failed=1; fi
  done
  if [[ $cleanup_failed != 0 ]]; then return 1; fi
  if ! rm -rf "$fixture"; then return 1; fi
  [[ ! -e $fixture ]]
}
trap cleanup_images EXIT
trap 'exit 130' INT TERM
for platform in "${targets[@]}"; do
  case "$platform" in linux/amd64|linux/arm64) ;; *) echo "Unsupported image test platform: $platform" >&2; exit 1 ;; esac
done
for platform in "${targets[@]}"; do
  architecture=${platform#linux/}
  image="portcullis-image-check:$run_id-$architecture"
  container="portcullis-$run_id-$architecture"
  images+=("$image")
  containers+=("$container")
  build_arguments=(--platform "$platform" --load --provenance=false --tag "$image")
  if [[ ${BUILD_VERSION+x} == x ]]; then
    build_arguments+=(--build-arg "BUILD_VERSION=$build_version")
  fi
  docker buildx build "${build_arguments[@]}" .
  actual=$(docker image inspect --format '{{.Os}}/{{.Architecture}}' "$image")
  [[ $actual == "$platform" ]] || { echo "Wrong image platform: $actual" >&2; exit 1; }
  user=$(docker image inspect --format '{{.Config.User}}' "$image")
  [[ $user == 65532 || $user == 65532:65532 || $user == nonroot ]] || { echo "Unexpected image user: $user" >&2; exit 1; }
  status=0
  output=$(timeout 30s docker run --name "$container" --rm --platform "$platform" --network none --read-only --cap-drop ALL --security-opt no-new-privileges --env PORTCULLIS_LOG_FORMAT=invalid "$image" 2>&1) || status=$?
  if [[ $status != 1 || $output != *'config load failed'* || $output != *'invalid log_format'* ]]; then
    printf 'Entrypoint smoke failed for %s (exit %s): %s\n' "$platform" "$status" "$output" >&2
    exit 1
  fi
  if [[ $output == *'"msg":"starting"'* || $output == *'"version":'* ]]; then
    printf 'Invalid logging emitted startup identity for %s: %s\n' "$platform" "$output" >&2
    exit 1
  fi
  version_container="$container-version"
  containers+=("$version_container")
  status=0
  output=$(timeout 30s docker run --name "$version_container" --rm --platform "$platform" --network none --read-only --cap-drop ALL --security-opt no-new-privileges \
    --env PORTCULLIS_LOG_FORMAT=json --env BUILD_VERSION=runtime-version-spoof --env PORTCULLIS_BUILD_VERSION=runtime-version-spoof \
    "$image" 2>&1) || status=$?
  startup_count=$(printf '%s\n' "$output" | grep -c '"msg":"starting"' || true)
  if [[ $status != 1 || $startup_count != 1 || $output != *"\"level\":\"INFO\",\"msg\":\"starting\",\"version\":\"$build_version\""* || \
    $output != *'master key required'* || $output == *runtime-version-spoof* ]]; then
    printf 'Linked startup version smoke failed for %s (wanted %s, exit %s): %s\n' "$platform" "$build_version" "$status" "$output" >&2
    exit 1
  fi
  results+=("$platform ($build_version)")
done
cleanup_images
trap - EXIT
printf 'Image platform, non-root user, linked startup version, entrypoint refusals and owned cleanup passed: %s\n' "${results[@]}"
