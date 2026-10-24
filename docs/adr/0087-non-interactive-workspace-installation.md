# ADR-0087: Non-interactive workspace installation

- **Status:** Accepted
- **Date:** 2026-10-25
- **Refines:** ADR-0080 for automated workspace installation.

## Context

The pre-push hook runs the complete verification target. pnpm may need to recreate generated module directories when their installation metadata differs from the requested settings. In Lefthook's non-interactive pseudo-terminal, pnpm can display a confirmation while keyboard input is not forwarded to that process. The reported log matches this prompt; it does not establish which installation setting differed.

## Decision

Set CI=true only for the pnpm process in the workspace-install recipe, retaining the exact pinned package manager and frozen root lockfile. Pinned pnpm disables its module-purge confirmation in CI mode. The hook and other verification targets continue to receive the caller's original environment; do not export CI from Make or set it around the complete verification command.

Retain the complete pre-push verification and separate dependency-security gate. Keep member dependency ownership, lock/catalog validation, integrity and build-script policy unchanged. Do not force installation, update the lockfile, pipe confirmation answers or route Git's pre-push reference input into the package manager. Interactive terminal configuration does not solve the automation boundary.

Installation may regenerate disposable module directories when their settings differ. An invalid lockfile, unavailable dependency or refused script must still fail verification. This does not make installation atomic or eliminate network time, and it does not claim that every possible lifecycle script is non-interactive. Later builds/tests retain their normal reporter and retry configuration.

## Consequences

Automated verification can complete a compatible frozen installation without waiting for a person, or stop with a package-manager error. Direct manual pnpm commands keep their normal interaction behavior. No global package-manager configuration or user store needs inspection or modification.

Qualify the real Make recipe and pinned pnpm against owned local fixtures rather than a fake package-manager command. Observe a compatible module-layout transition fail or wait under the old recipe, then cover at least four successful and four refused scenarios, unchanged lock/manifests, member usability and installer-only CI scope. Keep tests bounded, clean owned files and pass complete verification/security, running-application QA and code/architecture/design/staged reviews before the focused commit.

The Unix hook regression uses Python 3's standard-library pseudo-terminal support to exercise real Lefthook interaction from a piped test runner. Require that interpreter explicitly, use no Python package dependency and bound cleanup to recorded owned process groups.

## Sources (verified 2026-10-25)

- [pnpm 10 frozen installation](https://pnpm.io/10.x/cli/install).
- [Pinned pnpm CI confirmation boundary](https://raw.githubusercontent.com/pnpm/pnpm/v10.34.6/pkg-manager/core/src/install/index.ts).
- [Pinned pnpm module validation](https://raw.githubusercontent.com/pnpm/pnpm/v10.34.6/pkg-manager/core/src/install/validateModules.ts).
- [Lefthook output following](https://lefthook.dev/configuration/follow/).
- [Lefthook standard input](https://lefthook.dev/configuration/use_stdin/).
- [Git pre-push input contract](https://git-scm.com/docs/githooks#_pre_push).
