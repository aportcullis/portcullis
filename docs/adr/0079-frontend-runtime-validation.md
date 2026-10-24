# ADR-0079: Frontend runtime validation and explicit internal states

- **Status:** Accepted
- **Date:** 2026-10-24
- **Refines:** ADR-0013 and ADR-0031 for frontend boundaries; partly supersedes ADR-0077's allowance for ordinary nullable internal references.

## Context

Generated Connect/protobuf types describe the intended response but cannot establish every runtime invariant. An unset result cell can reach presentation as SQL NULL, and a missing expiry can bypass the browser's expiry scheduling. The frontend must reject malformed responses before retaining or displaying them while preserving exact values, recovery and current authority. The owner also requires explicit state transitions and runtime narrowing rather than pervasive optional values and implicit fallbacks.

## Options

Hand-written checks avoid another frontend dependency but duplicate schema traversal and error handling. Trusting generated types leaves cross-field invariants unchecked. Zod supplies one runtime parsing boundary with inferred output types and reusable safe error handling; result-specific checks remain with the wire adapter.

## Decision

Use the exact reviewed Zod version for frontend runtime validation. Keep the generic parser and sanitized validation error in shared/lib; keep generated-wire response schemas and reader adapters in shared/api. Accept unknown at an external boundary, parse once after Connect decoding and before state admission, and return the existing generated result type. Features supply their reader without introducing shared-to-feature dependencies. Do not add validation to render loops or replay SQL for recovery.

For result pages, validate the established logical enum, cell oneof and value representation, timestamp bounds, page-size choices, snapshot row limit and coherent page/row dimensions. Preserve SQL NULL, false, empty strings/bytes, exact integer/decimal text, nonfinite floating values and backend temporal/fallback representations. Do not impose new column-name/type/length limits or infer cell storage kind from column metadata. Strip unknown fields and copy validated byte views once so cached cells do not retain unrelated wire buffers. Keep generated source unchanged.

Expose only fixed, operation-scoped error messages without raw input, SQL, schema issues or causes. Invalid pages follow existing result retry and stale-response fences. Parsing is not resource authorization, a replacement for server validation, a new snapshot lifetime or a reason to classify a malformed page as expired.

Use named discriminated states with required payloads for internal absence and operation outcomes. Prefer unknown, runtime is predicates and checking assertion functions over any, unchecked casts, optional chaining and nullish fallbacks. Handle actual protocol or browser absence explicitly at the boundary; do not invent values to satisfy a type. This applies to new and substantially changed code, with existing callers migrated in small independently verified concerns. Retain ADR-0077's named props and finite vocabulary, plain-snapshot destructuring and Solid reactive-access discipline.

## Consequences

The frontend gains a production dependency and bounded parsing/copy work per response. Schemas must track generated contracts and preserve valid values; declaration-only typing is insufficient evidence. Invalid data becomes a recoverable visible failure, and retained state excludes unchecked extension fields. This decision does not claim that every existing frontend boundary has already migrated.

Each concern requires observed behavioral red and green scenarios, code/architecture/design review, QA of the running application, make verify and make supply-chain before its focused commit. Record implementation and verification in milestone evidence rather than changing this accepted decision body.

## Sources (verified 2026-10-24)

- [Zod parsing and errors](https://zod.dev/basics).
- [Zod object, union and numeric schemas](https://zod.dev/api).
- [Zod error customization](https://zod.dev/error-customization).
- [TypeScript type predicates, discriminated unions and assertion functions](https://www.typescriptlang.org/docs/handbook/2/narrowing.html).
- [Protocol Buffers Timestamp bounds](https://protobuf.dev/reference/protobuf/google.protobuf/#timestamp).
