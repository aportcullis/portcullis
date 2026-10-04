# ADR-0029: Dependency installation and supply-chain controls

- **Status:** Accepted
- **Date:** 2026-10-03

## Context

Pinned dependencies and immutable image/action references reduce unexpected changes, but do not prevent an approved dependency from running a compromised installation script. CI also installs pnpm from a floating major, and the vulnerability command resolves an unreviewed latest version. The user requested supply-chain defenses while completing M1 performance validation.

TypeScript 7.0.2 is available as a stable native compiler. k6 uses that exact version. The current typescript-eslint 8.64.0 and the latest inspected 8.71.0 still require the classic TypeScript API with a peer range below 6.1; replacing that API with the native compiler breaks lint tooling.

## Options

1. Rely only on lockfiles and periodic vulnerability scans.
2. Disable all dependency use and installation scripts, including required native build tools.
3. Combine exact versions, immutable references, frozen installs, explicit script decisions, release-age and trust controls, module integrity verification and vulnerability checks.

## Decision

Choose option 3. Retain separate web and load lockfiles. Configure each project with a seven-day minimum release age, no trust downgrade, blocked exotic transitive sources (pnpm permits registry, local/workspace and its explicitly trusted upstream sources) and explicit dependency build-script decisions. Unknown installation scripts fail until reviewed; no global build-script approval or broad trust exclusions are permitted. Keep store integrity verification enabled.

Use exact stable TypeScript 7.0.2 for web and load type checking through its official `tsc` command. ADR-0031 removes the classic compiler API and migrates lint enforcement to native Oxlint. Do not invoke package implementation files through direct `node_modules` paths.

Pin pnpm to 10.34.6. npm metadata for 10.34.3 has no provenance, and no-downgrade rejects it; 10.34.6 carries provenance and fixes engine download verification and archive handling. Allow only `pnpm@10.34.6` and its explicitly listed `@pnpm` engine artifacts at the same version to bypass the seven-day age delay after reviewing its signed immutable upstream release and registry metadata; no trust-policy exclusion is added. This exact-version age exception becomes inert once the release ages past seven days.

Freeze normal Make/CI installs and pin CI pnpm to the packageManager version. Disable persisted checkout credentials. Pin the Go vulnerability scanner, verify Go modules, and scan both npm lockfiles. Preserve read-only CI permissions, full action SHAs and image digests. Do not auto-upgrade dependencies or bypass advisories to obtain a green gate; assess affected versions and reachable behavior before a focused fix.

## Consequences

### Go toolchain security update (2026-10-03)

The CI vulnerability gate reported six reachable standard-library advisories with Go 1.26.5. The reports identify Go 1.26.6 as the fixed patch in that series; the user requested migration to Go 1.27. Adopt the current stable Go 1.27.1 patch in `go.mod` and the Docker builder together. CI already reads `go-version-file: go.mod`, so it inherits the same version without a second version pin. The builder uses the official `golang:1.27.1-alpine3.24` multi-platform index digest verified directly against Docker Hub. Keep the vulnerability gate enabled and verify the complete supply-chain target with the upgraded compiler; do not suppress the advisories. This delivery-toolchain update changes no product contract or PRD requirements.

- [Go release history](https://go.dev/doc/devel/release): Go 1.26.6 security fixes and Go 1.27.1 stable patch.
- [GO-2026-6218](https://pkg.go.dev/vuln/GO-2026-6218): affected and fixed `net/url` versions.
- [Official Go Docker image](https://hub.docker.com/_/golang): builder tag and registry digest.

Verification: `make supply-chain` passes on Go 1.27.1 with zero reachable or imported-package vulnerabilities. Its remaining module-only advisory, GO-2026-5932, concerns `golang.org/x/crypto/openpgp`, which this application does not import; it is not suppressed. Build, vet, lint (zero issues), Go scenario tests, frontend/load type checks, frontend lint, 126 frontend tests, and 14 real-binary browser scenarios pass. The browser gate uses the documented remote Chromium adapter. A separate HEAD snapshot containing only the toolchain edits also passes build, vet and govulncheck, without depending on existing uncommitted work.

A newly released security fix can need a narrowly scoped, documented version exception to the age policy after review. Delay and provenance checks reduce exposure and do not prove a package is benign. Third-party code can still run when an explicitly invoked compiler, bundler or test runner starts. CI must enforce the same policy on fresh installations, not just an existing local node_modules directory.

### Go 1.27 Linux lint compatibility (2026-10-03)

The Linux CI gate subsequently exposed a compatibility failure missed by the macOS validation above: golangci-lint v2.12.2 embeds Staticcheck v0.7.0, whose IR builder panics on Go 1.27 standard-library struct initializers in `internal/poll` (`unexpected expr: *ast.KeyValueExpr`). Building the old linter with the new compiler does not update its analyzers. Pin golangci-lint v2.14.0, which depends on Staticcheck v0.8.1 and x/tools v0.50.0. Staticcheck 2026.2 introduced support for Go 1.27 generic methods and direct references to embedded fields in struct initializers. Keep all existing lint checks and the Go 1.27.1 compiler; no product requirements change is needed.

When changing the Go compiler or analyzer version on macOS, also analyze the Linux target with `GOOS=linux GOARCH=amd64 make lint`. This exercises Linux-specific standard-library source; it supplements the actual Linux CI gate and does not execute Linux tests on macOS. Validate the regression with the real lint command rather than a test that only asserts a version string.

Validation: the reported Linux CI command is the observed failing regression. The upgraded command, with the same enabled checks, completes with zero issues inside the repository-pinned Go 1.27.1 Linux Docker image. Host `go build ./...` and `go vet ./...` also pass. Full `make verify` and a new GitHub Actions run have not been repeated for this analyzer-only change.

- [Staticcheck 2026.2 release and Go 1.27 support](https://github.com/dominikh/go-tools/releases/tag/2026.2).
- [golangci-lint v2.14.0 release](https://github.com/golangci/golangci-lint/releases/tag/v2.14.0).
- [golangci-lint v2.14.0 analyzer dependency pins](https://github.com/golangci/golangci-lint/blob/v2.14.0/go.mod).

No TypeScript 6 compatibility dependency remains. No product requirements change is needed: these controls concern development and delivery.

### Local build-context exclusion (2026-10-03)

Exclude local test/runtime artifacts before sending a Docker build context: `.test-docker`, dependency and bundler caches, browser traces/reports, load credentials/manifests/results/bundles, environment secrets and local key files. Keep committed generated clients/sqlc code, migrations and `.env.example` available. `.gitignore` is not a Docker boundary, and removing secrets in a later image layer does not undo their inclusion in an earlier build layer. Do not supply credentials through build arguments.

A small synthetic context using the actual `.dockerignore` reproduced the cache-marker inclusion before the fix. The real Docker `COPY`/`RUN` check then passed for excluded cache/load/browser/environment markers and retained Go/TypeScript generated sources, sqlc files, migrations and `.env.example`. No real credentials or repository cache contents were sent. This packaging regression does not constitute a complete image build or image scan.

Revised 2026-10-04: package-manager `.npmrc` files (registry tokens and machine-specific store paths), key/PEM files, private load output and the review cache are excluded from both git and Docker contexts through the committed ignore files rather than per-clone excludes. `make ignore-check`, part of `make verify`, repeats the synthetic-context check on every run and evaluates `.gitignore` in an isolated repository so a contributor's private exclude file cannot make it pass.

### GitHub Actions Node 24 runtime (2026-10-03)

The CI and PostgreSQL compatibility jobs reported deprecated Node 20 action runtimes. `setup-node`'s `node-version: 24` selects the application's Node version; it does not change the runtime declared by other actions. Update both workflows to immutable SHAs for checkout v7.0.1, setup-go v7.0.0, setup-node v7.0.0 and pnpm/action-setup v6.1.0. Their exact-SHA `action.yml` files declare `runs.using: node24`; official release tags resolve to the pinned, verified commits. All four releases satisfy the existing seven-day release-age policy.

Keep pnpm 10.34.6, explicit pnpm caching, disabled persisted checkout credentials, read-only permissions and the existing PostgreSQL matrix. Hosted `ubuntu-latest` supplies the required Node 24-compatible runner; self-hosted runners would need at least v2.327.1. Do not use runtime-forcing environment variables or unsafe checkout overrides to hide warnings. This delivery change does not alter product requirements.

Validation: actionlint v1.7.12 accepts both workflows; official tag/SHA, commit verification, runtime metadata and existing input contracts were checked. A new hosted run has not been observed, so local validation does not establish that the remote jobs passed or that their warnings disappeared.

- [checkout v7.0.1](https://github.com/actions/checkout/releases/tag/v7.0.1).
- [setup-go v7.0.0](https://github.com/actions/setup-go/releases/tag/v7.0.0).
- [setup-node v7.0.0](https://github.com/actions/setup-node/releases/tag/v7.0.0).
- [pnpm/action-setup v6.1.0](https://github.com/pnpm/action-setup/releases/tag/v6.1.0).

## Sources (checked 2026-10-03)

- [pnpm 10 settings](https://pnpm.io/10.x/settings).
- [pnpm supply-chain guidance](https://pnpm.io/supply-chain-security).
- [GitHub secure use of Actions](https://docs.github.com/en/actions/reference/security/secure-use).
- [Stable TypeScript 7.0.2](https://github.com/microsoft/typescript-go/releases/tag/typescript%2Fv7.0.2).

- [typescript-eslint supported dependency versions](https://typescript-eslint.io/users/dependency-versions/).

- [pnpm 10.34.6 security patch](https://github.com/pnpm/pnpm/releases/tag/v10.34.6).

- [Docker build context and dockerignore semantics](https://docs.docker.com/build/concepts/context/#dockerignore-files).
