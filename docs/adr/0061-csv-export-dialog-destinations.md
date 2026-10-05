# ADR-0061: CSV export destinations inside the dialog

- **Status:** Accepted
- **Date:** 2026-10-05
- **Supersedes:** [ADR-0056](0056-account-menu-and-csv-dialog.md) CSV destination selection only; its account menu, focus, cancellation and response-fencing contracts remain binding.

## Context

The owner clarified that Export CSV should open one dialog with settings and a choice of clipboard copy or file download. The current implementation prepares CSV and adds a download link to the toolbar; the dialog remains planned M1 work.
Per [W3C modal dialog guidance](https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/), focus stays inside the open dialog and returns on dismissal. Per [Clipboard API documentation](https://developer.mozilla.org/en-US/docs/Web/API/Clipboard/writeText), text copying requires a secure context and may fail when permission is denied.

## Decision

Export CSV opens the settings dialog before preparing data. Explain whole-snapshot scope, original query order and spreadsheet-safe output, then offer Copy CSV and Download CSV inside the dialog. Keep preparation, progress, cancellation, completion and errors there without adding a toolbar download action.
Both destinations use the same bounded, authorized CSV output with headers and the same escaping policy. Raw output requires the existing explicit warning and opt-in; no export setting reruns SQL or bypasses requester/organization checks.
Full CSV clipboard copy is distinct from Copy visible rows, which remains a bounded view action. Enforce an explicit clipboard byte limit before building or copying a complete string; larger snapshots retain file download. Clipboard denial keeps the dialog open with a clear error and download fallback.
Preserve ADR-0056's cancellation and stale-response fencing. Closing or changing sessions releases temporary export data and prevents obsolete completion actions.

## Acceptance

Verify open/configure/cancel without export, both destinations, equivalent CSV content, safe/raw choices, clipboard limit/denial, download readback, failures and closing/reopening during preparation. Check keyboard focus, 320 px layouts and permission revocation.
Per [M1 scope](../milestones/m1/scope.md), this clarifies the planned export contract; it does not claim implementation.
