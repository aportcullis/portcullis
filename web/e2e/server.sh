#!/bin/sh
# Run the real server and embedded SPA against a disposable PostgreSQL database.
set -eu

cd "$(dirname "$0")/../.."

# Keep the image digest aligned with compose and dbtest.
PG_IMAGE="postgres:18.4-alpine3.24@sha256:9a8afca54e7861fd90fab5fdf4c42477a6b1cb7d293595148e674e0a3181de15"

# Match E2E_PG_PORT in connections.spec.ts.
PG_PORT=15432

CID=$(docker run -d --rm \
    -e POSTGRES_USER=portcullis -e POSTGRES_PASSWORD=portcullis -e POSTGRES_DB=portcullis \
    -p 127.0.0.1:${PG_PORT}:5432 "$PG_IMAGE")

# Run the server as a child so the EXIT trap can remove its database container.
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
# This disposable test database explicitly permits owner runtime access and startup migration.
export PORTCULLIS_ALLOW_PRIVILEGED_RUNTIME=true
export PORTCULLIS_STARTUP_MIGRATE=true
PORTCULLIS_MASTER_KEY=$(head -c 32 /dev/urandom | base64)
export PORTCULLIS_MASTER_KEY
export PORTCULLIS_ADDR=127.0.0.1:18080

go run ./cmd/portcullis &
SERVER_PID=$!
wait "$SERVER_PID"
