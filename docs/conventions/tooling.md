# Tooling & commands

## Make
- `make build` — optimized static binary · `make run` — dev server · `make web` — build the SPA into
  the embed dir (`internal/platform/assets/dist`).
- `make test` — all tests (parallel + shuffle) · `make test-race` · `make lint` — golangci-lint via a
  pinned `go run` (built with the project Go) · `make vuln` / `make audit` — govulncheck + pnpm audit.
- `make generate` — `buf generate` (Connect Go + TS) and `sqlc generate`.

## Codegen
Generated code is **committed** — Go (`gen/`, `internal/infra/postgres/db/`) so `go build` works without
the codegen tools, and the TS client (`web/src/gen/`) so `pnpm`/`tsgo` and CI work without running `buf`.
Regenerate by editing `proto/*.proto` and the `*.sql` queries, then `make generate`; never hand-edit the
generated files.

## Dependencies
**Pinned** — Docker base images to patch tags, npm to exact builds — and kept current by **Renovate**
(`renovate.json`).

## Commit messages
Conventional-commit subject; body as **one sentence per line** (break on topic) or bullets /
sub-bullets. **Do not add a `Co-Authored-By` trailer.**

## Definition of done
`go build ./...`, `go vet ./...`, `make lint` (0 issues), and `make test` all green before calling
any work done.
