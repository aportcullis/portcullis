# ADR-0039: Result views and bounded clipboard export

- **Status:** Accepted
- **Date:** 2026-10-03

## Context

The owner requested clearer request progress and easier result exploration without routine popups or unbounded rendering.

## Decision

Offer Table and Text views over the same server-paged snapshot; switching views neither fetches the result again nor executes SQL. Preserve exact wire values, NULL markers and hexadecimal bytes. Text is quoted tab-separated content with column headers. Column headers advertise the ascending → descending → original-order cycle; sorting still applies on the server across the cached snapshot.

Copy visible rows copies headers and only the current filtered/sorted page using the browser Clipboard API. Escape formula-like textual values and headers for spreadsheet paste, including leading whitespace/control characters. Keep verified numeric wire values exact, without JavaScript number coercion. Distinguish empty text from NULL. Surface success or permission failure inline; offer Text/CSV fallback. Fence completion feedback against superseded pages/routes. CSV continues to export the whole cached snapshot in original order.

Loading placeholders have three decorative bars regardless of dataset size. Mark loading status accessibly and keep errors/empty results distinct. Do not replace known request details during background refresh. Results remain server paginated, default 20 and at most 100 rows per page; skeletons do not solve data-volume limits by themselves.

Validate exact values and formula escaping with scenario tests; use real-browser paging/filtering, view switching, clipboard success/denial and slow-response scenarios. Sources: [Clipboard API](https://developer.mozilla.org/en-US/docs/Web/API/Clipboard/writeText) and [W3C status messages](https://www.w3.org/WAI/WCAG22/Understanding/status-messages.html), verified 2026-10-03.
