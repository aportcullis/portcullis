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

No TypeScript 6 compatibility dependency remains. No product requirements change is needed: these controls concern development and delivery.

## Sources (checked 2026-10-03)

- [pnpm 10 settings](https://pnpm.io/10.x/settings).
- [pnpm supply-chain guidance](https://pnpm.io/supply-chain-security).
- [GitHub secure use of Actions](https://docs.github.com/en/actions/reference/security/secure-use).
- [Stable TypeScript 7.0.2](https://github.com/microsoft/typescript-go/releases/tag/typescript%2Fv7.0.2).

- [typescript-eslint supported dependency versions](https://typescript-eslint.io/users/dependency-versions/).

- [pnpm 10.34.6 security patch](https://github.com/pnpm/pnpm/releases/tag/v10.34.6).
