# M1 · Gate

**In progress — first PostgreSQL alpha boundary.**

Give teams one complete path from connection registration through policy, request, distinct review, single-use execution, result exploration, and audit. Preserve exact result values and explicitly report uncertain outcomes without automatically rerunning SQL.

User, role and binding administration with one-time password setup links belongs to this milestone.
Per [ADR-0062](../../adr/0062-role-bindings-and-conditions.md), separate organization membership, permission-bundle roles and multiple user bindings at organization or exact-connection scope, with optional bounded attribute/time conditions. [ADR-0071](../../adr/0071-binding-authorization-activation.md) activates their grant-preserving migration, session contract, governance and protected reads/output together. [ADR-0072](../../adr/0072-password-setup-consumption.md) completes one-time initial password registration through a public API and fragment-based setup page. These implementations do not establish the remaining result/export/account UX and release acceptance below.
Administrators manage users, roles and bindings under unconditional organization-wide delegation safeguards. Revalidate resource-scoped requester/approver eligibility and current conditions before execution, serialize authorization changes with approval/execution checks, revoke affected sessions and audit every administration mutation. Preserve an unconditional recovery administrator.

## UI usability

Result exploration provides readable search and column-header sorting and wraps complete Text values on narrow screens.
Result tables use virtual scrolling with limited overscan, nearby-page prefetch and a bounded browser cache that evicts distant pages. Keep a visible row range and total count; revisiting a range reads the existing snapshot without rerunning SQL. Ordinary lists retain numbered pages.
The account header shows a profile-image menu button; names, email, role, profile actions and sign-out appear inside its menu rather than filling the header.
CSV export opens a settings dialog explaining snapshot scope, query order and spreadsheet safety. Offer clipboard copy and file download inside the dialog, with progress, errors and cancellation; enforce a clipboard size limit and retain download when copying is unavailable.
These improve existing M1 workflows, separate from M3 personalization and review notifications.

## Acceptance

- The PostgreSQL governance journey works from bootstrap through distinct review, one execution, exact results/CSV and audit.
- Users, Roles and Bindings RPCs and resource-aware SPA actions support administration, condition validation and one-time password setup without flattening scoped grants into session-wide authority.
- Existing memberships migrate to equivalent unconditional organization bindings without widening grants or changing request history; partial migration refuses readiness and legacy role assignment ceases to authorize.
- Multiple connection roles, timed conditions and additive overlapping grants behave consistently across details, lists/counts, pickers, approval, execution and result/CSV output. Missing attributes, invalid expressions and exhausted evaluator budgets never grant access or disclose unfiltered totals.
- Per [ADR-0063](../../adr/0063-condition-dsl-and-pure-evaluation.md), Role/Binding remain data and pure CEL evaluation consumes explicit immutable inputs without I/O or clock reads. Verify reproducible decisions, fresh-time expiry, deterministic budget failure and operational cancellation refusal. Supported condition forms and API-authored CEL share server validation; preserve expressions outside the form's capabilities until explicit replacement/removal.
- Cross-organization access, privilege escalation, self-administration and last-administrator removal are refused, including concurrent changes.
- Disabled, revoked, out-of-scope or condition-expired approvers cannot supply a still-valid quorum; concurrent account/role/binding changes cannot bypass this rule. Stop further bounded result/CSV output after access is lost without replaying target SQL.
- Result navigation bounds rendered rows and retained browser pages/bytes, supports forward/backward scrolling and keyboard access, and rejects stale responses after sort, filter, permission or route changes.
- CSV dialogs offer equivalent authorized CSV through clipboard or download, enforce clipboard size limits and preserve download on clipboard denial.
- Account menus and CSV export dialogs support keyboard interaction, focus return and 320 px layouts without changing session, CSRF or result ownership checks.
- Release admission requires the tag, event commit and matching release-branch tip to agree, pins subsequent verification/publication to that source and refuses later tag replacement or deletion. Publication promotes the verified image digest (ADR-0084).
- Build version appears only in startup logs; Health responses do not expose it.
- Review findings, documentation and CHANGELOG are resolved, and both `make verify` and `make supply-chain` pass before release readiness.

Per [ADR-0059](../../adr/0059-bounded-result-scrolling.md) and [ADR-0061](../../adr/0061-csv-export-dialog-destinations.md), virtual result navigation and CSV destinations are M1 product requirements, separate from current implementation evidence.
Per [ADR-0053](../../adr/0053-user-and-role-administration.md) and its partial replacement [ADR-0062](../../adr/0062-role-bindings-and-conditions.md), administration closes the multi-user gap in the first alpha with separate role definitions, resource bindings and conditions.
See [progress](progress.md) for implementation status and [validation](validation.md) for recorded governance evidence.
