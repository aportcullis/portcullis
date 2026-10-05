# M1 · Gate

**In progress — first PostgreSQL alpha boundary.**

Give teams one complete path from connection registration through policy, request, distinct review, single-use execution, result exploration, and audit. Preserve exact result values and explicitly report uncertain outcomes without automatically rerunning SQL.

User and custom-role administration with one-time password setup links belongs to this milestone.
Administrators can create users, assign roles, disable/enable accounts and manage custom permission bundles without escalating their own authority.
Revalidate active approver eligibility before execution, serialize permission removal with approval/execution checks, revoke affected sessions and audit every administration mutation.

## UI usability

Result exploration provides readable search and column-header sorting and wraps complete Text values on narrow screens.
Result tables use virtual scrolling with limited overscan, nearby-page prefetch and a bounded browser cache that evicts distant pages. Keep a visible row range and total count; revisiting a range reads the existing snapshot without rerunning SQL. Ordinary lists retain numbered pages.
The account header shows a profile-image menu button; names, email, role, profile actions and sign-out appear inside its menu rather than filling the header.
CSV export opens a settings dialog explaining snapshot scope, query order and spreadsheet safety. Offer clipboard copy and file download inside the dialog, with progress, errors and cancellation; enforce a clipboard size limit and retain download when copying is unavailable.
These improve existing M1 workflows, separate from M3 personalization and review notifications.

## Acceptance

- The PostgreSQL governance journey works from bootstrap through distinct review, one execution, exact results/CSV and audit.
- Users and Roles RPCs and permission-aware SPA pages support administration and one-time password setup.
- Cross-organization access, privilege escalation, self-administration and last-administrator removal are refused, including concurrent changes.
- Disabled or demoted approvers cannot supply a still-valid quorum; concurrent role-permission removal cannot bypass this rule.
- Result navigation bounds rendered rows and retained browser pages/bytes, supports forward/backward scrolling and keyboard access, and rejects stale responses after sort, filter, permission or route changes.
- CSV dialogs offer equivalent authorized CSV through clipboard or download, enforce clipboard size limits and preserve download on clipboard denial.
- Account menus and CSV export dialogs support keyboard interaction, focus return and 320 px layouts without changing session, CSRF or result ownership checks.
- Release admission binds a tag to its release branch and publication promotes the verified image digest.
- Build version appears only in startup logs; Health responses do not expose it.
- Review findings, documentation and CHANGELOG are resolved, and both `make verify` and `make supply-chain` pass before release readiness.

Per [ADR-0059](../../adr/0059-bounded-result-scrolling.md) and [ADR-0061](../../adr/0061-csv-export-dialog-destinations.md), virtual result navigation and CSV destinations are M1 product requirements, separate from current implementation evidence.
Per [ADR-0053](../../adr/0053-user-and-role-administration.md), administration closes the multi-user gap in the first alpha.
See [progress](progress.md) for implementation status and [validation](validation.md) for recorded governance evidence.
