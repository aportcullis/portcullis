# Tooling & commands

## Make
- **Build and development**
  - `make build` produces the optimized static binary; `make run` starts the dev server.
  - `make web` builds the SPA into `internal/platform/assets/dist` for embedding.
- **Backend verification**
  - `make test` runs uncached shuffled tests; `make test-race` adds the race detector.
  - `make lint` checks gofmt and runs pinned golangci-lint with the project’s Go toolchain.
- **Frontend verification**
  - `make web-lint`, `make web-typecheck`, and `make web-test` run ESLint, tsgo, and Vitest.
- **Dependency security**
  - `make vuln` runs govulncheck; `make audit` also runs pnpm audit.
- `make generate` — `buf generate` (Connect Go + TS) and `sqlc generate`.
- `make load-test` — k6 2.3.0 scenario tests against an isolated installation; fixture and sizing instructions in [performance guide](../performance/README.md).
- `make query-bench` — actual PostgreSQL query workloads through raw pgconn and the execution adapter; three repeats of 20 iterations with allocation metrics.

## Codegen
Generated code is **committed** — Go (`gen/`, `internal/infra/postgres/db/`) so `go build` works without the codegen tools, and the TS client (`web/src/gen/`) so `pnpm`/`tsgo` and CI work without running `buf`.
Regenerate by editing `proto/*.proto` and the `*.sql` queries, then `make generate`; never hand-edit the generated files.
Pin remote plugin versions in `buf.gen.yaml` to the corresponding Go/TypeScript runtime versions; update pins and generated output together.

## Dependencies
**Pinned** — Docker base images to patch tags, npm to exact builds — and kept current by **Renovate** (`renovate.json`).

## Commit messages
Conventional-commit subject; body as **one sentence per line** (break on topic) or bullets / sub-bullets. **Do not add a `Co-Authored-By` trailer.**

## Definition of done
`go build ./...`, `go vet ./...`, `make lint` (0 issues), and `make test` all green before calling any work done.
`make verify` also checks frontend and k6 TypeScript sources and runs browser E2E.
