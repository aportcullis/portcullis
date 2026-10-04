# ADR-0037: Page hierarchy and responsive governance workflows

- **Status:** Accepted
- **Date:** 2026-10-03

## Context

The owner requested more polished UX after separating UI customization boundaries. Request composition currently presents a long undifferentiated form; global navigation has no current-location cue and its fixed horizontal layout does not adapt to narrow screens.

## Decision

Use the existing owned components and design tokens. Establish a quiet page background, white content surfaces, a restrained teal primary color, consistent headers and clear focus indicators. Mark the active navigation link with `aria-current`, including nested request routes, and let navigation/account controls reflow at narrow widths without introducing modal navigation.

Group request composition into context (connection/title/body), SQL/parameters and save/submit actions. On wide screens, provide a separate concise review guide; stack it on narrow screens. Preserve field labels, explicit save/submit behavior, formatting undo and authorization/session fencing. Explain that submitting does not execute SQL. Separate request evidence from a clearly labeled review/execution decision panel. Keep the panel sticky on wide screens and inline on narrow screens, show approval before rejection, and keep an explicit return-to-list link. Keep risky confirmations separate and routine actions within pages.

Use bounded three-line loading skeletons on initial list/detail/result reads, with accessible status text and hidden decorative bars. The authenticated shell shows the same skeleton while the session is still being resolved, so a slow session check never renders a blank page. Preserve known detail content during background refresh; distinguish loading, empty and error states.

A failed background refresh is news about the refresh, not about the data (amended 2026-10-04). The request list keeps the rows of the same page, size and filter when a refresh fails for a transient reason and labels them as the last loaded data; it clears them on a principal change, on `PermissionDenied`/`Unauthenticated`, or when the failed read asked a different query, so retained rows never pose as another query's answer. A failed background refresh of a request's details keeps the loaded details and reports the failure above them; only a failed first read shows the error alone. A result page, size, sort or filter change keeps the shown snapshot page with an "updating" status until the replacement arrives, and only the latest request's response may replace it.

Validate a real browser scenario at 320 CSS pixels for current-location cues, page reflow and reachable request actions, then run the existing full browser workflow matrix. Use actual application captures to update README media after visual changes. This is an incremental UX pass, not a claim of full WCAG conformance.

Research and task-based evaluation are tracked in [UX evidence](../design/ux-evidence.md). Inline request workflow disclosure is specified separately in ADR-0038; this presentation pass does not add a DAG editor or scheduler.

## Sources (verified 2026-10-03)

- [W3C reflow guidance](https://www.w3.org/WAI/WCAG22/Understanding/reflow.html): normal content reflows; SQL/data tables may retain local scrolling.
- [W3C visible focus guidance](https://www.w3.org/WAI/WCAG22/Understanding/focus-visible.html): keyboard users need a visible focus cue.
