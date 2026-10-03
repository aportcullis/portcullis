# ADR-0033: Type-aware result sorting within cached snapshots

- **Status:** Accepted
- **Date:** 2026-10-03

## Context

Column sorting already processes the entire encrypted snapshot on the server before pagination. Exact numbers are preserved, but lexical temporal comparisons can reverse fractional timestamps or disagree with timezone offsets. The UI lacks explicit sort status and a way to restore query order. SQL ordering is part of a user's query and must remain visible by default.

## Decision

Preserve the original snapshot order on initial reads. A chosen column automatically uses its declared logical type, not a guess from displayed text. Numeric-looking strings remain strings. Sort the entire bounded snapshot before paging, without executing target SQL again. Keep equal values in original row order and NULL last in both directions. Offer ascending, descending and original query order explicitly; show the selected column, direction, global snapshot scope and accessible column-header sort status.

Create domain sort keys once per processed row. Compare integers and decimals as arbitrary-precision rationals; finite floats, booleans and bytes retain their existing semantics. Compare DATE, TIME, TIMESTAMP and TIMESTAMPTZ through parsed calendar/time values; offsets affect instant ordering, while naive values remain timezone-independent. Recognize temporal infinities. Values outside supported canonical parsing (such as BC dates, extended years and unsupported temporal renderings) retain lossless text in a deterministic fallback group after typed values. Do not invent a timezone or coerce unsupported text. Keep that group after typed values in either direction and NULL after both groups. Numeric special values follow finite/infinity/NaN ordering from ADR-0005.

Keep sorting/filtering inside the existing bounded workers and snapshot limits. CSV continues to export the complete snapshot in original order; disclose this in the result page. This decision adds no automatic first-column sorting and no database collation emulation, schema mutation, new RPC or dependency.

## Consequences

Test observable sorted pages with exact numbers, fractional timestamps, equivalent instants, offsets, NULLs, fallback text and stable ties. Browser scenarios cover accessible header state, an explicit sort selector, page reset, sort removal, preserved filters and CSV scope. Initial query order and saved SQL remain unchanged.

## Primary sources (verified 2026-10-03)

- [TanStack Table v8 sorting](https://tanstack.com/table/v8/docs/guide/sorting): pair server paging with manual sorting over server data.
- [W3C sortable table](https://www.w3.org/WAI/ARIA/apg/patterns/table/examples/sortable-table/): buttons in sortable headers and aria-sort on the active header.
- [RFC 3339 §5.1](https://www.rfc-editor.org/rfc/rfc3339.html#section-5.1): string ordering requires matching timezone representation and fractional precision.
- [PostgreSQL date/time types](https://www.postgresql.org/docs/18/datatype-datetime.html): timestamp infinity and offset semantics.
