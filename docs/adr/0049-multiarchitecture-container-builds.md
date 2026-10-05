# ADR-0049: Build AMD64 and ARM64 container variants

- **Status:** Accepted
- **Date:** 2026-10-04

## Context

ADR-0047 initially publishes only linux/amd64. AMD and Intel x86-64 share that architecture; ARM64 hosts need a separate binary and runtime manifest. Architecture coverage is not equivalent to support for every CPU or operating system.

## Decision

Publish a single OCI image index for linux/amd64 and linux/arm64 under each existing release tag, keeping SBOM/provenance enabled. Run frontend and Go builder stages on BUILDPLATFORM. Pass BuildKit TARGETOS/TARGETARCH to the static Go compiler; the distroless runtime follows the target platform. No QEMU is needed for compilation.

Add a reusable CI image matrix on native ubuntu-24.04 and ubuntu-24.04-arm runners. Build the actual Dockerfile, inspect image OS/architecture and non-root user, and execute the actual entrypoint with deliberately invalid nonsecret configuration. Require its known config-refusal behavior, rather than accepting an exec-format error or any failure.
Publication depends on this matrix through the same-commit CI workflow. Local `make image-check` checks both variants; non-native execution needs existing emulation support, while IMAGE_PLATFORMS can select a native variant. Remove only owned test images/containers.

## Consequences

The initial image contract is Linux x86-64 (AMD/Intel) and Linux AArch64. Apple Silicon uses the ARM64 Linux container through Docker's Linux VM; native macOS/Windows binaries, 32-bit ARM/x86, RISC-V, POWER and s390x are outside this release image scope. This is a packaging/startup smoke gate, not a full database/browser acceptance suite on each architecture; functional CI remains on AMD64.
Both architecture builds and native hosted runner checks must pass before release publication. Local emulated execution does not establish native hosted qualification.

## Sources

- [Docker multi-platform and Go cross-compilation](https://docs.docker.com/build/building/multi-platform/).
- [Docker multi-platform Actions builds](https://docs.docker.com/build/ci/github-actions/multi-platform/).
- [GitHub native hosted runner labels](https://docs.github.com/en/actions/reference/runners/github-hosted-runners).
