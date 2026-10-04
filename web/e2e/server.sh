#!/bin/sh
# Build the real binary and let Testcontainers own the disposable test database.
set -eu
cd "$(dirname "$0")/../.."
mkdir -p .test-docker/e2e
go build -o .test-docker/e2e/portcullis ./cmd/portcullis
go build -o .test-docker/e2e/runner ./web/e2e/server
# Cleanup without Ryuk (ADR-0045).
export TESTCONTAINERS_RYUK_DISABLED=true
exec .test-docker/e2e/runner
