# ADR-0077: Explicit frontend type contracts and safe destructuring

- **Status:** Accepted
- **Date:** 2026-10-21
- **Refines:** ADR-0013, ADR-0031 and ADR-0076 for source maintainability.

## Context

The PRD requires preserved input, current authority and safe asynchronous completion. Anonymous component prop objects and inline value unions obscure the vocabulary of these contracts. Equivalent list-loading types are duplicated between entity stores, while the input wrapper manually lists native input types without naming its supported contract.

## Decision

Give each component a named Props type next to its implementation and give meaningful finite value sets a named type owned by their feature or component. Keep discriminated union members literal so narrowing still associates each status with its payload; optional values and ordinary nullable references do not require artificial aliases. Reuse generated and library types rather than duplicating their vocabulary. Share domain-independent loading state below entity slices and derive control sizes from their existing style keys. Keep feature-specific vocabulary local, without a global types bucket or runtime enum allocation.

Use destructuring for plain response snapshots and stable return objects when it makes the consumed fields clear. Do not destructure reactive props or store properties into component-scope values: preserve property access, accessors or splitProps. Signal tuple destructuring already preserves accessors. Type-only imports, aliases and interfaces must not add requests, reactive effects or state ownership. Existing input restrictions, permission keys, discriminated outcomes and response fences remain unchanged.

## Verification

This concern preserves behavior; existing scenario tests are its regression baseline, not a new behavioral red. Verify the named contracts through the actual TypeScript compiler and inspect emitted application behavior through the full make verify gate, including session-refresh, pending mutation, permission-loss and result-sorting scenarios. Require make supply-chain and staged review before commit. No product behavior or PRD change is required.

## Sources (verified 2026-10-21)

- [TypeScript type aliases and unions](https://www.typescriptlang.org/docs/handbook/2/everyday-types.html).
- [TypeScript indexed access types](https://www.typescriptlang.org/docs/handbook/2/indexed-access-types.html).
- [Solid props and destructuring](https://docs.solidjs.com/concepts/components/props).
- [Solid splitProps](https://docs.solidjs.com/reference/reactive-utilities/split-props).
