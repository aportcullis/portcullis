# ADR-0038: Inline request workflow disclosure

- **Status:** Accepted
- **Date:** 2026-10-03

## Context

The owner requested clearer request progress and easier result exploration without routine popups or unbounded rendering.

## Decision

Use a button on each request title to expand a four-stage directed acyclic graph below that row: Draft → Review → Ready → Execution. Use only authorized list summary facts, with no additional payload fetch. One expanded request is retained by ID across refreshes. Preserve separate permission-gated detail links and actions.

Distinguish automatic approval, current stages, rejected decisions and execution outcomes. For cancellation/expiry, the last active stage is unknown; do not fabricate approval history or timestamps. Unknown outcomes never imply a retry. A succeeded execution does not promise result-cache availability. Provide an ordered textual representation, explicit status words and `aria-expanded`; stack nodes on narrow screens.

Pure scenario tests cover quorum, automatic approval, stopped requests, unknown outcomes and success. Browser tests expand/collapse an actual request without opening a modal. This is a progress summary, not an editable DAG or audit-history replacement.

Sources: [W3C disclosure pattern](https://www.w3.org/WAI/ARIA/apg/patterns/disclosure/) and [research evidence](../design/ux-evidence.md), verified 2026-10-03.
