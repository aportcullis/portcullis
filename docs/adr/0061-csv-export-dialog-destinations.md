# ADR-0061: CSV export destinations inside the dialog

- **Status:** Accepted
- **Date:** 2026-10-05
- **Supersedes:** [ADR-0056](0056-account-menu-and-csv-dialog.md) CSV destination selection only; its account menu, focus, cancellation and response-fencing contracts remain binding.

## Context

The owner clarified that Export CSV should open one dialog with settings and a choice of clipboard copy or file download. The initial implementation prepared CSV and added a download link to the toolbar.
Per [W3C modal dialog guidance](https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/), focus stays inside the open dialog and returns on dismissal. Per [Clipboard API documentation](https://developer.mozilla.org/en-US/docs/Web/API/Clipboard/writeText), text copying requires a secure context and may fail when permission is denied.

## Decision

Export CSV opens the settings dialog before preparing data. Explain whole-snapshot scope, original query order and spreadsheet-safe output, then offer Copy CSV and Download CSV inside the dialog. Keep preparation, progress, cancellation, completion and errors there without adding a toolbar download action.
Both destinations use the same bounded, authorized CSV output with headers and the same escaping policy. Raw output requires the existing explicit warning and opt-in; no export setting reruns SQL or bypasses requester/organization checks.
Full CSV clipboard copy is distinct from Copy visible rows, which remains a bounded view action. Enforce an explicit clipboard byte limit before building or copying a complete string; larger snapshots retain file download. Clipboard denial keeps the dialog open with a clear error and download fallback.
Preserve ADR-0056's cancellation and stale-response fencing. Closing or changing sessions releases temporary export data and prevents obsolete completion actions.

## Implementation

The feature owns a pure export controller with consumer-defined ports for streaming, clipboard writes, object URLs and failure classification. The Solid component injects Connect and browser adapters through those ports and uses the existing Kobalte dialog. Tagged operation and download states keep absence explicit; runtime guards check browser capabilities, checkbox events and byte fragments before use.

Clipboard preparation stops before constructing complete text above 2 MiB. File preparation stops above 128 MiB, separately from the server's encrypted snapshot and execution limits. Incoming fragments coalesce into 64 KiB blocks, bounding retained buffer objects as well as payload bytes; empty fragments retain nothing. The final partially filled block has at most 64 KiB of unused capacity. Blob creation and bounded clipboard decoding may temporarily copy prepared bytes; these accounting limits are not a JavaScript heap measurement or capacity qualification.

Only one export operation remains unsettled, including aborted work. Cancel, close, safety changes, request changes and disposal abort it, clear its buffers and revoke prepared object URLs. Generation checks also run before external stream and clipboard calls, so a synchronous disposal callback cannot initiate obsolete output. Copy denial offers an already authorized native file link; exceeding the clipboard limit leaves the download action available to prepare the complete authorized stream. Partial or failed streams never publish a file.

Preparation and native saving share one primary footer position: the preparation button becomes the save link when the complete file is ready. Keyboard focus follows that replacement only within the same dialog opening and only when the requester has not moved focus to another control. Closing, changing requests or disposal fences delayed focus work as well as delayed data.

`ExportQueryCSVRequest.raw` is an additive explicit opt-in with a safe false default. Its domain options select formula-prefix escaping independently of CSV quoting and preserve the stored snapshot. Both representations use the same owner, organization, resource-condition, result-lifetime, session and CSRF checks. The dialog requires a raw-output warning and acknowledgment before submitting that choice. Per [OWASP CSV Injection guidance](https://owasp.org/www-community/attacks/CSV_Injection), escaping is not a universal guarantee across spreadsheet applications and re-saving.

## Acceptance

Verify open/configure/cancel without export, both destinations, equivalent CSV content, safe/raw choices, clipboard limit/denial, download readback, failures and closing/reopening during preparation. Check keyboard focus, 320 px layouts and permission revocation.
Per [M1 scope](../milestones/m1/scope.md), qualification belongs in [validation](../milestones/m1/validation.md) and complete milestone readiness remains separate.
