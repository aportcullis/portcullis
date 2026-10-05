# ADR-0058: Result ordering through column headers

- **Status:** Accepted
- **Date:** 2026-10-05
- **Supersedes:** [ADR-0033](0033-type-aware-result-sorting.md) and [ADR-0055](0055-result-exploration-layout.md) sorting-control presentation; their processing, filtering, text wrapping and security contracts remain binding.

## Context

The product owner wants SQL results shown in their returned order and finds a separate sorting toolbar and arrows on every column distracting. Sorting remains useful as an explicit result-exploration action.
Per [PostgreSQL ordering documentation](https://www.postgresql.org/docs/18/queries-order.html), SQL needs an explicit `ORDER BY` to guarantee an order. Preserving the received snapshot does not imply that a query without `ORDER BY` will repeat that order on a later execution.
Per [W3C sortable-table guidance](https://www.w3.org/WAI/ARIA/apg/patterns/table/examples/sortable-table/), sortable headers use buttons, `aria-sort` identifies the active column and decorative arrows are hidden from assistive technology. Unsorted-column icons are optional.

## Decision

Remove the separate Sort by and Direction selectors. Initially display the stored SQL result in its received order, with no active-column arrow or automatic first-column sorting.
A Table header cycles its column through ascending, descending and original result order. Show one upward or downward arrow only on the active column; restoring original order removes every arrow. Header buttons retain keyboard operation, visible focus and accessible sort state.
Retain a short status description and a shared accessible explanation of the three-state cycle. Table sorting remains an optional view operation; Text retains the selected view order and Table allows changing it.

Preserve every processing rule in ADR-0033, including type-aware global snapshot sorting before paging, stable ties, NULL-last handling, temporal/fallback semantics, limits, filter preservation and page reset. Restore query order through the same stored snapshot without changing SQL or executing the target again. CSV remains the complete original-order snapshot.
Separate result columns with subtle dashed vertical borders using the theme border color; retain horizontal overflow for wide tables.
Preserve ADR-0055's labeled filter, separate copy/export actions, consistent control sizing, responsive layouts and complete wrapped Text values. Only its separate sorting-control group is replaced.

## Consequences

The default result view has fewer controls. Users sort by clicking a column header and can reverse or clear that sort through the same button.
Browser scenarios verify no separate sort controls, no unsorted arrows, one active arrow, ascending/descending/restored rows, keyboard interaction, preserved filters and no additional target execution. Refresh screenshots and GIFs from the real application.
This decision changes presentation only; existing authorization, sessions, snapshot processing and CSV contracts remain binding.
