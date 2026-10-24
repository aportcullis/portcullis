# ADR-0082: Result feature source ownership

- **Status:** Accepted
- **Date:** 2026-10-24
- **Refines:** ADR-0013 and ADR-0078 for result interaction and stylesheet ownership.

## Context

The request feature owns composition and review together with result navigation, presentation and CSV export. Result exploration has its own interaction and lifetime boundaries. Separating it must preserve downward imports, protected response fences and stylesheet behavior without introducing shared mutable result state.

## Decision

Own result navigation, sorting, presentation, error handling and CSV export together in features/result with their existing colocated tests. Keep the result route as assembly and retain route-owned controller disposal and authorization-key remounting. Shared wire validation remains at the API boundary. Do not introduce a result entity store or future BI APIs for this source move.

Move the passive execution summary and its pure metric formatter into entities/request, which already owns the request outcome vocabulary. Both request and result features may consume it downward; the request feature retains its requester-only fetch and lifetime. Do not introduce a sibling-entity import or a feature-to-feature dependency.

Colocate result styles with the result feature. Compose them once from the app CSS entry using the existing @/ source alias for every local CSS import. Vite resolves this alias in CSS imports. Preserve selector, declaration, layer and media behavior; package imports retain their package names.

## Consequences

This changes source ownership without a product, protocol, permission or state-lifetime change, so both PRD translations retain their existing requirements. Qualify existing success, refusal and stale-response scenarios and observe a deliberately broken guard before restoring it; do not claim a new product behavior red. Compare normalized moved source and compiled CSS, enforce FSD imports, and require both complete gates, code/architecture/design review and actual running-application QA before committing.

## Sources (verified 2026-10-24)

- [Feature-Sliced Design layer import rule](https://fsd.how/docs/reference/layers/).
- [Feature-Sliced Design slice cohesion and isolation](https://fsd.how/docs/reference/slices-segments/).
- [Vite CSS import alias support](https://vite.dev/guide/features.html#css).
