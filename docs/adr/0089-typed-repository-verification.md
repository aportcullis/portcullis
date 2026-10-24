# ADR-0089: Type-check repository verification before execution

- **Status:** Accepted
- **Date:** 2026-10-25
- **Refines:** ADR-0080 for repository-owned tooling dependencies and ADR-0088 for verification execution.

## Context

Repository verification runs Node scripts alongside the Go and browser suites. JavaScript execution alone does not check fixture contracts, process outcomes or parsed JSON. Node 24 can execute erasable TypeScript directly, but it performs no type checking and ignores tsconfig settings.

## Decision

Use TypeScript for maintained Node and browser source, including tests and automation. Convert existing JavaScript when its concern is changed, retaining generated JavaScript and formats genuinely required by a tool. Begin with the package-scheduling verification suite rather than combining unrelated migrations.

The root package owns its repository tooling dependencies through the existing exact TypeScript, Node-type and Oxlint catalog entries. Keep the web and load members' dependency declarations and the current workspace membership. Do not borrow an undeclared compiler from a member or add a runtime transpiler.

Use ordinary .ts source files and declare the root package's module type explicitly. Execute verification as native Node ESM TypeScript after strict no-emit checking and linting. Use NodeNext resolution, erasable syntax, explicit type imports and checked collection access. Retain native absence through explicit checks and model internal process states as tagged unions. Parse JSON into unknown and narrow it at the boundary; do not use assertions, any or suppression comments to silence the checker.

The canonical verification command must stop before launching tests when type checking or linting fails. Root aggregate commands include repository checks explicitly because recursive pnpm scripts normally exclude the root. Retain the actual Go/process/session scenarios, bounded cleanup and original failure assertions.

## Consequences

Native execution avoids an emitted test build, while static checks add a required gate. A runnable file with an invalid type must demonstrate rejection before an execution witness appears; a Node-only pass does not establish type safety. Qualify valid and refused source scenarios, then run complete functional/security gates and running-application QA before the focused commit.

## Sources (verified 2026-10-25)

- [Node 24 TypeScript execution and its type-checking limits](https://nodejs.org/docs/latest-v24.x/api/typescript.html).
- [TypeScript erasable syntax](https://www.typescriptlang.org/tsconfig/erasableSyntaxOnly.html).
- [Oxlint TypeScript configuration](https://oxc.rs/docs/guide/usage/linter/config.html).
- [pnpm 10 recursive script scope](https://pnpm.io/10.x/cli/recursive).
