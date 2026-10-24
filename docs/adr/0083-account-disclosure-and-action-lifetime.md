# ADR-0083: Account disclosure and action lifetime

- **Status:** Accepted
- **Date:** 2026-10-24
- **Refines:** ADR-0056 for account-menu semantics, current Binding summaries and action lifetime.

## Context

The authenticated header still displays identity, Binding badges and sign-out beside the avatar. M1 requires those details inside an account menu. Current authorization exposes multiple scoped Binding summaries, while the legacy membership role is no longer authoritative. The installed Kobalte 0.13.12 dropdown menu intercepts Tab and restores trigger focus after the custom close callback, which conflicts with the required native keyboard exit.

## Decision

Use an avatar-triggered, nonmodal disclosure panel with the existing Kobalte Popover primitive behind a thin domain-independent shared UI family. Keep its implementation, explicit exports, example and usage notes together. Render content directly after the trigger without a Portal so ordinary document tab order remains meaningful. Give the trigger an accessible account label and expanded/controls state; give the content a named account region and native action buttons. Explicitly override the primitive's dialog role and popup hint. This panel does not expose ARIA menu semantics or require an application-menu arrow-key model.

Enter and Space toggle the disclosure. Escape focuses the current connected native trigger through the primitive's callback before default dismissal; explicit trigger dismissal also leaves focus at the trigger. Native Tab and Shift+Tab may leave, close the panel and retain their resulting focus; outside pointer dismissal retains the chosen target. The pinned Popover excludes its trigger from outside detection, so record a content Tab intention and consume it on focusout, closing only when focus actually leaves. Do not prevent Tab, enumerate focusable elements or add timer-based focus repair. Suppress obsolete close autofocus after disposal or reopening and preserve native focus exits; ordinary current dismissal retains a clear trigger or outside-target focus. Qualify trigger clicking, rapid reopening and route departure against the actual installed implementation.

Show name with explicit email fallback, email, and each current Binding's role, organization or exact-connection scope and conditional marker inside the panel. Display an unavailable scope explicitly when advisory scope facts are unrecognized or incomplete. Keep these summaries advisory and preserve the server's independent authorization. Reuse the opaque-ID local identicon without external image requests. Provide implemented account actions only: M1 currently has sign-out; profile editing and image personalization remain their existing roadmap scope.

Own logout pending and failure state outside the conditionally mounted panel content. Closing and reopening must not admit a duplicate operation, discard pending state or hide a recoverable failure permanently. Fence callbacks by the stable authenticated interaction and component lifetime; close on route or identity/authority changes. Reuse the session store's existing logout operation and CSRF interceptor: successful revocation or an unusable session becomes anonymous, while transport/server failure preserves authentication and offers explicit retry. Do not add navigation, automatic write retry or a second logout transport path.

Keep authentication-write ownership distinct from advisory-read ordering. A completed or pending same-account Me refresh cannot supersede confirmed logout; successful revocation invalidates older in-flight advisory responses before becoming anonymous. A later sign-in, later logout or newly resolved account identity still fences an older completion. Preserve current advisory-read and sign-in sequencing and do not treat a rejected transport operation as successful revocation.

## Consequences

The header becomes smaller while account details require one explicit interaction. Both PRD translations retain the same M1 behavior and M3 boundary; this decision specifies implementation and accessible semantics. Native disclosure semantics avoid promising a keyboard model the panel does not implement.

Observe a real-browser red against the compiling existing header before implementation. Cover at least four success and four refusal/recovery/lifetime cases, including both keyboard exit directions, identity and scope display, recoverable logout failure, duplicate admission and stale callbacks. Require both complete gates, code/architecture/design and staged review, and actual light/dark desktop and 320-pixel QA before the focused commit. Do not claim complete assistive-technology or WCAG conformance from these browser checks.

## Sources (verified 2026-10-24)

- [WAI disclosure semantics and keyboard activation](https://www.w3.org/WAI/ARIA/apg/patterns/disclosure/).
- [WAI disclosure navigation and the distinction from an ARIA menu](https://www.w3.org/WAI/ARIA/apg/patterns/disclosure/examples/disclosure-navigation/).
- [Kobalte Popover composition and focus APIs](https://kobalte.dev/docs/core/components/popover) (current documentation is 0.13.14; behavior is qualified against the pinned 0.13.12 implementation).
