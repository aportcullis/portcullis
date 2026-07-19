#!/bin/sh
# Boots the e2e stack for Playwright (ADR-0013): a throwaway PostgreSQL
# container on a random host port plus the real Go server on :18080 serving the
# embedded SPA. Fresh database every run — the bootstrap flow must always be
# exercisable. The container is removed when the server process exits.
set -eu

cd "$(dirname "$0")/../.." # repo root: the Go module lives above web/

# tag@digest pin, same digest as compose.yaml / dbtest (Renovate bumps together).
PG_IMAGE="postgres:18.4-alpine3.24@sha256:9a8afca54e7861fd90fab5fdf4c42477a6b1cb7d293595148e674e0a3181de15"

# Fixed host port: connections.spec.ts dials this database as its TARGET
# (E2E_PG_PORT there must match), and a fixed port needs no coordinate
# handoff between this child process and the Playwright test process.
PG_PORT=15432

CID=$(docker run -d --rm \
    -e POSTGRES_USER=portcullis -e POSTGRES_PASSWORD=portcullis -e POSTGRES_DB=portcullis \
    -p 127.0.0.1:${PG_PORT}:5432 "$PG_IMAGE")

# The container is stopped by the EXIT trap (--rm then removes it). The server
# must therefore run as a CHILD below — never via exec, which would replace
# this shell and take the trap with it, leaking one container per run.
cleanup() {
    [ -n "${SERVER_PID:-}" ] && kill "$SERVER_PID" 2>/dev/null || true
    docker stop "$CID" >/dev/null 2>&1 || true
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

i=0
until docker exec "$CID" pg_isready -U portcullis >/dev/null 2>&1; do
    i=$((i + 1))
    [ "$i" -gt 60 ] && { echo "postgres never became ready" >&2; exit 1; }
    sleep 1
done

PORTCULLIS_DATABASE_URL="postgres://portcullis:portcullis@127.0.0.1:${PG_PORT}/portcullis?sslmode=disable"
export PORTCULLIS_DATABASE_URL
# Single-role dev shape: the owner DSN doubles as the runtime DSN (ADR-0009),
# so the privileged-runtime check must be explicitly waived — e2e only — and
# startup migration is opted into EXPLICITLY (it is decoupled from the
# privileged-runtime flag; the fresh database has no schema yet).
export PORTCULLIS_ALLOW_PRIVILEGED_RUNTIME=true
export PORTCULLIS_STARTUP_MIGRATE=true
PORTCULLIS_MASTER_KEY=$(head -c 32 /dev/urandom | base64)
export PORTCULLIS_MASTER_KEY
export PORTCULLIS_ADDR=127.0.0.1:18080

go run ./cmd/portcullis &
SERVER_PID=$!
wait "$SERVER_PID"
