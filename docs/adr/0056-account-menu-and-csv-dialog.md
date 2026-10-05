# ADR-0056: Account menu and CSV export dialog

- **Status:** Superseded by [ADR-0061](0061-csv-export-dialog-destinations.md) for CSV destination selection; its remaining contracts continue through that decision.
- **Date:** 2026-10-05
- **Supersedes:** [ADR-0041](0041-default-profile-identicons.md)

## Context

The header repeats the avatar, name, role and sign-out action, distracting from the current workflow. CSV preparation introduces another toolbar action instead of explaining the download choices in one place.
Per [M1 scope](../milestones/m1/scope.md), account menus and CSV dialogs are small usability improvements to existing alpha workflows. Review inboxes, notification badges and profile-image personalization belong to [M3 scope](../milestones/m3/scope.md).
Per [WAI-ARIA menu button guidance](https://www.w3.org/WAI/ARIA/apg/patterns/menu-button/), account actions need an accessible trigger and keyboard interaction. Per [WAI-ARIA modal dialog guidance](https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/), dialogs manage focus and return it on dismissal.

## Options

- Keep account details and prepared-download actions permanently visible in the header and toolbar.
- Move account details into an avatar-triggered menu and CSV choices into a bounded dialog (chosen).

## Decision

Show a profile-image button with an accessible account label in the header. Its menu identifies the user by name, email and current role and provides supported profile actions and sign-out. Opening, keyboard navigation and dismissal preserve a clear focus location.
Retain ADR-0041's local five-by-five mirrored geometric SVG fallback, derived deterministically from the opaque user ID without email or external avatar lookups. The same account keeps its default across reloads; names remain visible inside the menu rather than beside the avatar.
Use a CSV dialog to explain whole-snapshot scope, original query order and spreadsheet-safe output, confirm the download and display progress or errors. Cancelling before confirmation makes no export request; no SQL is executed again. Closing and reopening fences stale responses.
This dialog is a deliberate exception to ADR-0022's page-first guidance for a short export decision; normal result exploration remains on its direct page.
Google photos and user-selected profile images are M3 product scope; their storage and image-validation design requires a separate decision before implementation.

## Consequences

The header carries less persistent account text, while the menu adds one interaction to reveal identity and account actions. CSV scope is clearer before download, and the result toolbar retains a stable set of actions.
Acceptance does not imply implementation: milestone progress and validation record delivered behavior, while this ADR establishes the next UI contract.
