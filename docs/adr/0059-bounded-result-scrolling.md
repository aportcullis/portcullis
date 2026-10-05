# ADR-0059: Bounded virtual scrolling for result tables

- **Status:** Accepted
- **Date:** 2026-10-05
- **Supersedes:** [ADR-0039](0039-result-views-and-clipboard.md) result-navigation and visible-range presentation only; its exact values, clipboard safety and authorization contracts remain binding.

## Context

The owner selected virtual scrolling for M1 results and deferred implementation to a separate task. The current result screen still uses numbered pages.
Per [TanStack Virtual documentation](https://tanstack.com/virtual/latest/docs/introduction), virtualization limits rendered elements to the visible window.
Per [TanStack Query's limited infinite queries](https://tanstack.com/query/latest/docs/framework/react/guides/infinite-queries#what-if-i-want-to-limit-the-number-of-pages), a separate page limit bounds retained data while allowing forward and backward fetching.
These references explain two distinct limits; this decision does not select a new dependency.

## Decision

Replace result-table numbered navigation with a virtual row window over the existing encrypted snapshot. Render visible rows plus limited overscan, prefetch nearby bounded server pages and evict distant pages from the browser cache. Refetch evicted pages from the same snapshot when scrolling back; never accumulate all visited results in browser memory or execute SQL again.
Show the current row range and total count. Preserve exact values, header sorting, filtering and original-order restoration. Changing the request, principal, filter or sort clears the relevant window/cache and rejects stale responses; snapshot expiry shows unavailability without re-execution.
Text and Copy visible rows use an explicitly labeled bounded row range with headers, excluding hidden overscan and prefetched rows. CSV retains the complete original-order snapshot independently of the displayed range.
Keep ordinary request, administration and audit lists paginated. Server result requests retain bounded page sizes; virtualization changes presentation and browser retention, not server storage quotas or snapshot processing.

## Acceptance

Verify forward/backward scrolling, distant-page eviction and refetch, rapid scrolling, stale responses, filter/sort changes, expiry, fetch errors and session/route changes. Keyboard navigation and focus must remain usable when rows leave the rendered window.
Specify and measure rendered-row, retained-page/byte and concurrent-fetch limits before reporting completion. Confirm that browser retention stays bounded as the visited range grows and that SQL executes only once.
Per [M1 scope](../milestones/m1/scope.md), this is M1 acceptance; [progress](../milestones/m1/progress.md) records currently delivered behavior.
