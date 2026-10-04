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

## Failure states and recovery (amended 2026-10-04)

A route never renders a blank document. Unknown addresses inside the authenticated shell render a not-found page with the application frame and a link to the start page, using the router's catch-all route. An application-level `ErrorBoundary` replaces a page whose render or reactive update throws with a generic, recoverable error card (retry or return to the start page) that never echoes the error text.

Solid's resource accessor rethrows its fetch error, and a boundary does not catch errors thrown from event handlers, so a failed instance-config read previously broke request pages and turned Save/Submit into silent no-ops. Optional consumers of the instance limits (title/body/reason counters and client-side length checks) read them through a guarded accessor that yields "unknown" on failure; the server still enforces every limit. The authenticated shell retries a failed config read in the background so the limits return without a reload. The login and bootstrap pages keep their explicit retry card because routing there depends on the config.

A direct link that finds no valid session redirects to `/login?returnTo=<path>` and continues at that path after sign-in; an explicit sign-out starts fresh at `/login`. The return path is attacker-controllable, so it is accepted only as a same-origin relative path: a single leading `/` not followed by `/` or `\`, no ASCII control characters or spaces (which URL parsers strip into a host reference), the same origin after URL parsing, and never `/login` or `/bootstrap`. Anything else falls back to the start page, and only the parsed path, query and fragment are navigated to. The return path is navigation only and grants nothing.

## Consequences

Replace request overlays first, then apply the same rule to remaining routine connection and policy flows. Update browser scenarios and actual README captures alongside their screen changes. URL state is navigation only, never authorization. The known native-download gate remains enabled.

## Sources (verified 2026-10-04)

- [Solid ErrorBoundary](https://docs.solidjs.com/reference/components/error-boundary): catches render and reactive-update errors, not event-handler errors; the fallback receives a reset function.
- [Solid Router catch-all routes](https://docs.solidjs.com/solid-router/concepts/catch-all): a `*404` route at the end of the route list renders unmatched paths.
- [OWASP Unvalidated Redirects and Forwards Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Unvalidated_Redirects_and_Forwards_Cheat_Sheet.html): validate redirect targets against an allow-list rather than trusting user input; here the allow-list is "a path on this origin".
- [solid-js `createResource` source](https://github.com/solidjs/solid/blob/main/packages/solid/src/reactive/signal.ts): `read()` throws the stored error while the resource is errored.
