# ADR-0031: TypeScript 7 and native lint enforcement

- **Status:** Accepted
- **Date:** 2026-10-03

## Context

The user requires TypeScript 7 throughout web and k6, without a TypeScript 6 compatibility dependency or direct `node_modules` implementation paths. Stable `typescript@7.0.2` exports the official `tsc` command; the preceding native-preview package exported `tsgo`. Current typescript-eslint requires the classic compiler API and fails against the native compiler. Retaining that dependency would contradict the requested toolchain.

## Decision

Pin **TypeScript 7.0.2** in both projects and invoke `tsc` through pnpm scripts. Remove the native-preview package, compiler aliases and TypeScript 6. Use **Oxlint 1.85.0**, a stable release older than the seven-day installation delay, for native TypeScript/TSX linting without the classic compiler API. Remove ESLint/typescript-eslint and their tooling-only dependencies.

Transfer the existing JavaScript/TypeScript recommended rules explicitly into typed `web/oxlint.config.ts`, rather than enable unrelated presets. Keep the dynamically generated downward FSD and sibling-slice restrictions, including type-only imports, explicit-any rejection, prefer-const and no comma operators. Disable the parentheses exception of no-sequences to meet the existing convention. The unavailable no-dupe-args/no-new-symbol rules were already disabled for TypeScript; strict TypeScript/native parsing rejects illegal duplicate bindings and legacy octal literals in modules. Native source type checking remains a separate required gate, not a lint substitute.

Verify observable lint scenarios with the actual CLI: allow downward alias imports; refuse relative, upward, sibling and type-only boundary violations, explicit any and comma operators. Removing enforcement must make the scenarios red before the production configuration makes them green. Require typecheck, lint, existing frontend tests, build and browser E2E before declaring the toolchain change complete. No product feature or compiler-API shim is introduced.

## Consequences

Editor integrations use Oxlint. Preserve the rule inventory when upgrading tooling and update conventions. Type-aware lint was not enabled by the prior recommended preset; adding it requires its own decision. Supply-chain installation and audit policy remains ADR-0029. Stable native TypeScript keeps the familiar tsc name; command names alone do not identify compiler implementation.

## Primary sources (verified 2026-10-03)

- [TypeScript 7.0.2 native release](https://github.com/microsoft/typescript-go/releases/tag/typescript%2Fv7.0.2).
- [typescript-eslint dependency compatibility](https://typescript-eslint.io/users/dependency-versions/).
- [Oxlint migration](https://oxc.rs/docs/guide/usage/linter/migrate-from-eslint).
- [Oxlint configuration](https://oxc.rs/docs/guide/usage/linter/config-file-reference.html).
- [Oxlint built-in TypeScript rules](https://oxc.rs/docs/guide/usage/linter/plugins).
