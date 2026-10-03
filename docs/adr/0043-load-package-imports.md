# ADR-0043: Package-root imports for load tests

- **Status:** Accepted
- **Date:** 2026-10-03

## Context

The owner requests root-based imports for the TypeScript k6 suite instead of relative paths. The suite also runs contracts directly through Node and bundles npm dependencies through esbuild before k6 execution. Machine-specific absolute paths would break other checkouts; TypeScript `paths` alone does not rewrite runtime imports.

## Decision

Use `#load/<module>` for package-local TypeScript imports and re-exports, including contract tests. Define one `imports` map in `tests/load/package.json`: `#load/*` resolves to `./*.ts`. TypeScript's bundler resolution, Node's native package imports and esbuild resolve the same declaration without an additional runtime loader or duplicated alias configuration. Keep external imports such as `k6`, `zod` and `node:test` unchanged.

Prohibit relative, filesystem-absolute and `file:` module imports with Oxlint. Runtime fixture files are data, not source modules, and continue to use configured absolute paths. k6 receives bundled ESM with local aliases resolved; it does not need to understand the package map.

## Consequences

Imports stay stable when a caller moves within the package, and portable across CI and developer checkouts. A successful TypeScript check alone is insufficient: verify the Node contracts, esbuild output and k6 contract smoke. This changes import resolution, not workload behavior, fixture ownership or capacity claims.

## Sources

- [Node package subpath imports](https://nodejs.org/api/packages.html#subpath-imports): private package mappings use a `#` prefix and support patterns.
- [TypeScript paths](https://www.typescriptlang.org/tsconfig/paths.html): mappings do not rewrite emitted imports.
- [esbuild package resolution](https://esbuild.github.io/api/#packages): subpath imports resolve before package externalization.
