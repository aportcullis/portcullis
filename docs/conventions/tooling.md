# Tooling & commands

## Make
- **Build and development**
  - `make build` produces the optimized static binary; `make run` starts the dev server.
  - `make web` builds the SPA into `internal/platform/assets/dist` for embedding.
- **Backend verification**
  - `make test` runs uncached shuffled tests; `make test-race` adds the race detector and runs as its own CI job. Both require the test databases, so a database-backed test fails instead of skipping when its container cannot start; `GO_TEST_PACKAGES` narrows the package set. Both schedule package processes sequentially (`-p 1`) to avoid the reproduced shared Testcontainers reaper startup race (ADR-0045), while retaining within-package parallel tests and competing-caller scenarios.
  - `make clean` removes build outputs only and never deletes committed generated sources; `make clean-check`, part of `make verify`, proves it in a disposable clone.
  - `make lint` checks gofmt on every tracked Go file and runs pinned golangci-lint with the project’s Go toolchain. After compiler or analyzer upgrades on macOS, also run `GOOS=linux GOARCH=amd64 make lint` to check Linux-specific source before relying on Linux CI (ADR-0029).
- **Frontend verification**
  - `make web-lint`, `make web-typecheck`, and `make web-test` run Oxlint, TypeScript 7 native `tsc`, and Vitest.
- **Container images**
  - `make image-check` builds and smoke-tests Linux AMD64 and ARM64 images. `IMAGE_PLATFORMS` selects a supported variant; cross-architecture local execution needs emulation. CI uses native runners and both checks gate tagged publication (ADR-0049).
- **Release history**
  - `make changelog` generates the reviewed English `CHANGELOG.md` snapshot with digest-pinned git-cliff in Docker; run before release tagging and review the diff.
  - `make dockerfile-check` runs Docker build checks on every tracked Dockerfile and on accepted and refused fixtures; included in `make verify`. Supply base images that come from a single pinned definition as named build contexts instead of `ARG`s in `FROM`.
  - `make ignore-check` proves that the committed `.gitignore` and `.dockerignore` exclude package-manager configuration, keys, environment files, private load output and review ledgers while keeping generated sources, migrations and `.env.example`; included in `make verify`.
  - `make changelog-check` exercises real Git history and release boundaries; included in `make verify`. `make release-notes` renders only the current tag for the GHCR publication summary.
- **Dependency security**
  - `make vuln` runs pinned govulncheck; `make audit` also runs pnpm audit. `make supply-chain` verifies Go modules, audits both npm lockfiles and checks fresh-install script rejection; CI runs it after the functional gate.
- `make generate` — `buf generate` (Connect Go + TS) through `go tool buf` with local plugins, and `sqlc generate` through the digest-pinned image in `.github/scripts/codegen-images.sh`; generation needs no network and no local buf or sqlc install (run `pnpm -C web install` first for the TypeScript plugin).
- `make generate-check` regenerates in a disposable copy and fails on generated-code drift, `buf lint` violations or an untidy `go.mod`; `make proto-breaking` runs `buf breaking` against `origin/main`. Both are part of `make verify`.
- `make load-check` — strict TypeScript, locally bundled k6 scripts and Zod RPC/fixture contract tests; included in `make verify`.
- `make load-test` — k6 2.3.0 scenario tests against an isolated installation; fixture and sizing instructions in [performance guide](../performance/README.md).
- `make query-bench` — actual PostgreSQL query workloads through raw pgconn and the execution adapter; three repeats of 20 iterations with allocation metrics.

## Codegen
Generated code is **committed** — Go (`gen/`, `internal/infra/postgres/db/`) so `go build` works without the codegen tools, and the TS client (`web/src/gen/`) so `pnpm`/`tsc` and CI work without running `buf`.
Regenerate by editing `proto/*.proto` and the `*.sql` queries, then `make generate`; never hand-edit the generated files.
The protobuf plugins are pinned through the runtimes themselves: `protoc-gen-go` and `protoc-gen-connect-go` are `go tool` entries resolved from the same modules as the Go runtime, and `@bufbuild/protoc-gen-es` is an exact web devDependency matching `@bufbuild/protobuf`; update the runtime, plugin and generated output together.

## Dependencies
**Pinned** — Docker base images to patch tags, npm to exact builds — and kept current by **Renovate** (`renovate.json`). PostgreSQL pins in Compose, the test catalog and README capture harness are tracked. Major promotions are disabled for Compose and regex managers; PostgreSQL 19 preview updates stay on `19betaN` until explicit GA qualification. Docker compatibility suffixes such as `-alpine3.24` remain fixed; changing that platform suffix requires a reviewed update.

## Branches and versions

`main` develops the next release.
After the current milestone meets all completion criteria, cut `release/vX.Y` from main; the first release branch is `release/v0.1`.
The owner creates and pushes `vX.Y.Z` tags on that branch and maintains its patch releases there; agents never tag or push.
Develop the next milestone on `feat/<feature>` branches from main for owner review and merge, and obtain approval before starting work outside the current milestone.

Only Git tags assign product versions; documents, source files and CHANGELOG have no independent revision number.
Keep dependency/tool versions as evidence and use `Unreleased` before an actual release tag exists.
Per [ADR-0054](../adr/0054-release-branches-and-documentation-policy.md), milestone scope, progress and release versions have separate ownership.
See the [release procedure](../operations/container-releases.md).

## Commit messages
Use a Conventional Commit subject and keep each commit focused on one concern.
Until the owner releases `v0.1.0`, `fix`, `refactor` and `perf` are prohibited; use the applicable `feat`, `docs`, `test`, `build`, `ci` or other permitted type. Write the body as topic bullets without a trailing colon, each with one-sentence sub-bullets, and finish with a `Verification` topic listing the observed red, the passing gates, the staged-diff review and any remaining item; keep each sentence on one line. **Do not add a `Co-Authored-By` or other AI attribution trailer**; this applies to coding agents, which commit each completed concern themselves.

Immediately before each commit, review the staged diff for scope, correctness, dependency direction, security and scenario coverage. Check the staged snapshot rather than relying on unstaged dependencies in the working tree, and pass the complete `make verify` gate before each commit, including uncached Go tests with required databases, web tests, load-check and browser E2E. Fix findings, repeat the review after changing the staged content, and record any remaining failed gate explicitly. Commit related tests with the behavior they verify; do not reconstruct an unobserved TDD history after implementation.

Commit a completed red→green, checked and reviewed concern immediately, before starting another concern. Queue incoming requests until the current commit is complete. Never carry completed uncommitted changes into the next task; an explicit stop request or destructive action takes precedence.

## Definition of done
`make verify` builds the SPA, browser and load bundle once, then runs three groups concurrently — static checks, Go tests and browser E2E — each in its own order, keeping each group's log in a per-run `$TMPDIR/portcullis-verify.XXXXXX` directory and printing it only when the group fails (ADR-0045).
Milestone completion requires `make verify` (Go build/vet/lint/tests, web typecheck/lint/tests, load-check and browser E2E) plus `make supply-chain`, matching the functional CI gate. CI also builds and smoke-tests image packaging on native AMD64 and ARM64 runners; tagged publication requires those jobs. Every test passes before each small commit; any remaining failed non-test gate must be explicit.
The browser harness builds the real embedded application and uses the [Testcontainers PostgreSQL module](https://golang.testcontainers.org/modules/postgres/) with its readiness strategy.
`web/playwright.config.ts` reserves free loopback ports once and passes them as `E2E_APP_PORT` and `E2E_TARGET_PORT`, so concurrent browser runs never share a port.
The harness serves its fixture coordinates on the target port, removes its own databases and server at shutdown, and removes only containers whose owning harness process has exited.
The fixture clears inherited application configuration before supplying its disposable database and key.

For a restricted host browser environment, the [Docker browser E2E guide](../operations/browser-e2e.md) runs Chromium remotely while preserving the same real-binary, Testcontainers and native CSV file-readback assertions.
