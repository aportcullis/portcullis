# Browser E2E with a disposable Docker browser

The default `make verify` gate runs the real embedded binary, a Testcontainers-owned PostgreSQL database, and local Chromium. When native browser file saving fails in a restricted host environment, use Playwright's [remote browser connection](https://playwright.dev/docs/docker#remote-connection) to keep the tests, binary and database harness on the host and run only Chromium in a disposable container.

Match the browser image and server package to `web/package.json` (currently Playwright 1.61.1). The image below is pinned by digest. This recipe mounts only the project, with source read-only and the test download directory writable. The browser endpoint is published only on host loopback. The Docker socket stays on the host; it is not mounted into the browser container.

Install the web dependencies first (`pnpm -C web install --frozen-lockfile`). Run from the project root in a shell with the Docker and Go-tool permissions required for the test environment, and set `DOCKER_HOST` to the explicitly permitted Docker endpoint:

```sh
set -eu
mkdir -p .test-docker/downloads
test -f .test-docker/config.json || printf '{"auths":{}}\n' > .test-docker/config.json
export DOCKER_CONFIG="$PWD/.test-docker"
: "${DOCKER_HOST:?Set DOCKER_HOST to your permitted Docker endpoint}"
export DOCKER_HOST

docker run -d --rm --init --name portcullis-m1-browser --shm-size=1g \
  -p 127.0.0.1:13000:3000 \
  --mount "type=bind,source=$PWD,target=$PWD,readonly" \
  --mount "type=bind,source=$PWD/.test-docker/downloads,target=$PWD/.test-docker/downloads" \
  --workdir "$PWD" \
  mcr.microsoft.com/playwright:v1.61.1-noble@sha256:5b8f294aff9041b7191c34a4bab3ac270157a28774d4b0660e9743297b697e48 \
  node web/node_modules/@playwright/test/cli.js run-server \
  --host 0.0.0.0 --port 3000 --artifacts-dir "$PWD/.test-docker/downloads"
trap 'docker stop portcullis-m1-browser >/dev/null' EXIT

CI=true PLAYWRIGHT_BROWSERS_PATH="$PWD/.test-docker/browsers" \
  PW_TEST_CONNECT_WS_ENDPOINT=ws://127.0.0.1:13000/ \
  PW_TEST_CONNECT_EXPOSE_NETWORK='<loopback>' make verify
```

Loopback network exposure lets the remote browser reach the host's test application without changing the application's origin or cookie rules. The PostgreSQL target coordinates are still supplied by the owned fixture endpoint and used by the host application.

CSV coverage retains the native `download` event, suggested filename, successful file saving and saved-file readback. Fetching a Blob's prepared contents alone does not satisfy that scenario. Passing here establishes browser behavior in the recorded Linux container; it does not diagnose or fix a host-native macOS download failure. Capacity/soak qualification is separate from this correctness gate.
