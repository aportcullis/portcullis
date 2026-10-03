#!/usr/bin/env bash
set -euo pipefail

# Build and execute the distributed entrypoint for each requested image variant.
platforms=${IMAGE_PLATFORMS:-linux/amd64,linux/arm64}
IFS=',' read -r -a targets <<< "$platforms"
mkdir -p .test-docker
fixture=$(mktemp -d "$PWD/.test-docker/image-check.XXXXXX")
run_id=${fixture##*/}
images=()
containers=()
cleanup_images() {
  for container in "${containers[@]}"; do docker rm -f "$container" >/dev/null 2>&1 || true; done
  for image in "${images[@]}"; do docker image rm "$image" >/dev/null 2>&1 || true; done
  rm -rf "$fixture"
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
  docker buildx build --platform "$platform" --load --provenance=false --tag "$image" .
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
  echo "Image platform, non-root user and entrypoint refusal passed: $platform"
done
