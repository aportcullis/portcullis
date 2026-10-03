# Tooling & commands

## Make
- **Build and development**
  - `make build` produces the optimized static binary; `make run` starts the dev server.
  - `make web` builds the SPA into `internal/platform/assets/dist` for embedding.
- **Backend verification**
  - `make test` runs uncached shuffled tests; `make test-race` adds the race detector.
  - `make lint` checks gofmt and runs pinned golangci-lint with the project’s Go toolchain.
- **Frontend verification**
  - `make web-lint`, `make web-typecheck`, and `make web-test` run Oxlint, TypeScript 7 native `tsc`, and Vitest.
- **Dependency security**
  - `make vuln` runs pinned govulncheck; `make audit` also runs pnpm audit. `make supply-chain` verifies Go modules, audits both npm lockfiles and checks fresh-install script rejection; CI runs it after the functional gate.
- `make generate` — `buf generate` (Connect Go + TS) and `sqlc generate`.
- `make load-check` — strict TypeScript, locally bundled k6 scripts and Zod RPC/fixture contract tests; included in `make verify`.
- `make load-test` — k6 2.3.0 scenario tests against an isolated installation; fixture and sizing instructions in [performance guide](../performance/README.md).
- `make query-bench` — actual PostgreSQL query workloads through raw pgconn and the execution adapter; three repeats of 20 iterations with allocation metrics.

## Codegen
Generated code is **committed** — Go (`gen/`, `internal/infra/postgres/db/`) so `go build` works without the codegen tools, and the TS client (`web/src/gen/`) so `pnpm`/`tsc` and CI work without running `buf`.
Regenerate by editing `proto/*.proto` and the `*.sql` queries, then `make generate`; never hand-edit the generated files.
Pin remote plugin versions in `buf.gen.yaml` to the corresponding Go/TypeScript runtime versions; update pins and generated output together.

## Dependencies
**Pinned** — Docker base images to patch tags, npm to exact builds — and kept current by **Renovate** (`renovate.json`).

## Commit messages
Use a conventional-commit subject and keep each commit focused on one concern. Write the body as paragraphs separated when the topic changes, or as bullets; do not automatically break after every sentence. **Do not add a `Co-Authored-By` trailer.**

Immediately before each commit, review the staged diff for scope, correctness, dependency direction, security and scenario coverage. Check the staged snapshot rather than relying on unstaged dependencies in the working tree, and run verification appropriate to the change. Fix findings, repeat the review after changing the staged content, and record any remaining failed gate explicitly. Commit related tests with the behavior they verify; do not reconstruct an unobserved TDD history after implementation.

## Definition of done
`go build ./...`, `go vet ./...`, `make lint` (0 issues), and `make test` all green before calling any work done.
`make verify` also checks frontend and k6 TypeScript sources and runs browser E2E. The browser harness builds the real embedded application, uses the [Testcontainers PostgreSQL module](https://golang.testcontainers.org/modules/postgres/) with its readiness strategy, and serves its dynamically assigned fixture coordinates at the loopback-only test endpoint `127.0.0.1:18081/target`. It removes its own database and server at shutdown and refuses occupied application or fixture ports. The fixture clears inherited application configuration before supplying its disposable database and key.

For a restricted host browser environment, the [Docker browser E2E guide](../operations/browser-e2e.md) runs Chromium remotely while preserving the same real-binary, Testcontainers and native CSV file-readback assertions.
