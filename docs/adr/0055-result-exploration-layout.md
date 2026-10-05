# ADR-0055: Result exploration controls and readable text

- **Status:** Superseded by [ADR-0058](0058-header-only-result-sorting.md); processing and other presentation contracts remain binding through that decision.
- **Date:** 2026-10-05

## Context

Result filters, copy/export actions and sorting occupy separate rows with inconsistent control heights. The filter placeholder is clipped, and long Text results require horizontal scrolling before the user can read a complete value.
Per [PRD §7.1](../product/prd.en.md#71-main-screens), result exploration must remain usable at 320 px and preserve exact values without executing SQL again.
Per [Grafana table documentation](https://grafana.com/docs/grafana/latest/visualizations/panels-visualizations/visualizations/table/), table exploration exposes filtering, alignment and text wrapping as distinct presentation concerns.

## Options

- Keep the existing controls and rely on horizontal scrolling for Text results.
- Group filtering, sorting and copy/export actions, align control heights and wrap Text values (chosen).
- Introduce Grafana-style dashboards and per-column filter expressions, which belong outside this M1 layout change.

## Decision

Use a labeled, flexible search field with an explicit filter action. Keep copy/export actions separate from the filter form and group sorting controls together. Controls have consistent heights and stack within the viewport on narrow screens.
Wrap long Text values visually while retaining complete tab-separated content for selection and clipboard copying. Keep bounded vertical scrolling for large pages and full-cell inspection in Table view.
Preserve snapshot paging, sorting, filtering, CSV scope and all authorization/session boundaries. This decision extends the presentation guidance in ADR-0037 and ADR-0039 without replacing their behavior contracts.

## Consequences

Filtering remains an explicit action; typing does not issue requests. Visual wrapping changes line layout but not stored values or copied content. Narrow screens need more vertical space to keep actions and labels readable.
README screenshots and walkthroughs must show the resulting layout.
