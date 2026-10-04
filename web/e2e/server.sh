#!/bin/sh
# Build the real binary and let Testcontainers own the disposable test database.
set -eu
cd "$(dirname "$0")/../.."
mkdir -p .test-docker/e2e
go build -o .test-docker/e2e/portcullis ./cmd/portcullis
go build -o .test-docker/e2e/runner ./web/e2e/server
# The runner terminates its own containers and make e2e-run sweeps them by label, so it skips the shared reaper whose concurrent startup races with Go test processes (ADR-0045).
export TESTCONTAINERS_RYUK_DISABLED=true
exec .test-docker/e2e/runner
