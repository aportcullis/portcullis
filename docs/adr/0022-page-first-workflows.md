# ADR-0022: Page-first workflows and request navigation
- **Status:** Accepted
- **Date:** 2026-10-03

## Context

SQL composition, draft editing, approval review and result exploration are sustained work. Modal dialogs limit available space, obscure navigation context and cannot be reopened through a URL. The product owner requires routine workflows within pages and reserves modal confirmation for risky actions.

GitHub presents review context on a pull-request page with dedicated review views ([official review guide](https://docs.github.com/en/pull-requests/get-started/reviewing-pull-requests-quickstart)); Kviklet describes a [pull-request-like SQL approval workflow](https://github.com/kviklet/kviklet). These references motivate navigation and context, without changing Portcullis's immutable approval unit, requester ownership or single-use execution semantics.

## Options

- Keep modal forms: small navigation changes, but constrained SQL editing and no durable route.
- Use drawers: more space, but still an overlay and limited history semantics.
- Use routed pages: direct links, reload and browser history, with enough space for SQL and review context.

## Decision

Use pages for routine creation, editing, details, policies and results. Use modal confirmation only where a risky/destructive action requires a focused acknowledgement; avoid confirmations for routine navigation or reading.

Requests use `/requests` for the list, `/requests/new` for composition, `/requests/:id` for SQL/target/approval details and draft editing, and `/requests/:id/result` for the execution snapshot. Saving or submitting navigates to the stored request when the caller has `requests.get`; create-only callers return to their allowed request landing page. Decisions stay on the detail page and refresh its server-owned state. Provide visible links back to the list/detail, preserve browser history and allow direct reload. A full-cell view stays within the result page.

Keep permission checks per action and all backend enforcement unchanged. Page reads and mutations remain fenced against route/session changes; unmounting discards in-flight UI callbacks. Polling must not replace SQL being edited or a decision reason. Route changes never automatically save or submit SQL; use the explicit draft action before leaving. An unsaved form does not persist plaintext to browser storage.

Closing an optional read panel invalidates its session and immediately clears its loading state; completion of the underlying request is not required. Late success, error and finally callbacks must not update a reopened panel or clear its new loading state. The close/in-flight/reopen scenarios enforce this independently of network timing, using Solid's [explicit signal setters](https://docs.solidjs.com/reference/basic-reactivity/create-signal).

## Consequences

Replace request overlays first, then apply the same rule to remaining routine connection and policy flows. Update browser scenarios and actual README captures alongside their screen changes. URL state is navigation only, never authorization. The known native-download gate remains enabled.
