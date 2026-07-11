#!/bin/sh
# Boots the e2e stack for Playwright (ADR-0013): a throwaway PostgreSQL
# container on a random host port plus the real Go server on :18080 serving the
# embedded SPA. Fresh database every run — the bootstrap flow must always be
# exercisable. The container is removed when the server process exits.
set -eu

cd "$(dirname "$0")/../.." # repo root: the Go module lives above web/

PG_IMAGE="postgres:18.4-alpine3.24" # same pin as compose.yaml / dbtest

CID=$(docker run -d --rm \
    -e POSTGRES_USER=portcullis -e POSTGRES_PASSWORD=portcullis -e POSTGRES_DB=portcullis \
    -p 127.0.0.1:0:5432 "$PG_IMAGE")

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

PORT=$(docker port "$CID" 5432/tcp | head -n1 | awk -F: '{print $NF}')

i=0
until docker exec "$CID" pg_isready -U portcullis >/dev/null 2>&1; do
    i=$((i + 1))
    [ "$i" -gt 60 ] && { echo "postgres never became ready" >&2; exit 1; }
    sleep 1
done

PORTCULLIS_DATABASE_URL="postgres://portcullis:portcullis@127.0.0.1:${PORT}/portcullis?sslmode=disable"
export PORTCULLIS_DATABASE_URL
# Single-role dev shape: the owner DSN doubles as the runtime DSN (ADR-0009),
# so the privileged-runtime check must be explicitly waived — e2e only.
export PORTCULLIS_ALLOW_PRIVILEGED_RUNTIME=true
PORTCULLIS_MASTER_KEY=$(head -c 32 /dev/urandom | base64)
export PORTCULLIS_MASTER_KEY
export PORTCULLIS_ADDR=127.0.0.1:18080

go run ./cmd/portcullis &
SERVER_PID=$!
wait "$SERVER_PID"
