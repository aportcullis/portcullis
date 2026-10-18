# M1 implementation and validation — 2026-10-03

The PostgreSQL M1 correctness/release gate passed on 2026-10-03 with Docker-hosted Chromium, including native CSV file saving and saved-file readback. This establishes the recorded governance slice; full M1 acceptance is defined in [scope.md](scope.md). It is not a published release or production-capacity certification.
Earlier host-native download failures remain recorded below and are not claimed fixed.

## Implemented scope

- Single-use approved execution with digest, pinned policy/configuration, quorum and requester checks; atomic lease/start evidence; heartbeat, recovery and late-completion fencing.
- Target timeout, read-only reads, cancellation, conservative catalog candidate proof, row/payload/protocol limits, separate pre-decode cell-memory admission, two execution workers and per-connection circuit breaking.
- Encrypted UNLOGGED result chunks with owner/org boundaries, TTL, quotas and eviction; bounded exact-value sorting/filtering/paging and formula-safe CSV streaming.
- Result grid, full cells, execution controls and 30-second approval/state fallback polling.
- Resumable multi-version key rotation, with historical keys retained for digest evidence.

See [ADR-0021](../../adr/0021-governed-query-execution.md), [quickstart](../../operations/pg-alpha-quickstart.md) and [key rotation](../../operations/key-rotation.md).

## Verification evidence

Build, vet, lint (zero issues), frontend typecheck/lint, 104 frontend unit tests, load-script typecheck and all Go tests passed during `make verify`. Targeted race checks passed for execution, result processing, circuit admission, PostgreSQL persistence, Connect execution/security, and governed target limits.
Behavioral coverage includes tampered wire payload refusal before leasing, concurrent ownership, audit rollback, overdue-owner refusal, unknown outcomes without retry and cache loss preserving success.
Result coverage includes precision/NULL/non-finite sorting, formula escaping, key rotation, oversized cells, wide NULL-row admission and returning writes committing the whole statement despite result truncation. A cancelled-call red scenario was corrected by checking cancellation before batch admission.

At the original full-gate run, the Chromium suite passed 7 of 8 tests. The new execution scenario passes execution, exact-value paging/sorting/filtering, generated whole-snapshot CSV content and suggested filename checks; `download.failure()` then returns `canceled`, so actual file saving is not verified. The assertion and file-content readback remain enabled.

A standalone eight-byte Blob download and a standalone ordinary HTTP attachment both reproduce `canceled` with a project-local download directory; regular Chromium and headless shell both reproduce the Blob failure. This establishes a failure outside the product-specific export path, but does not establish the operating-system cause.
An independent Firefox attempt also did not provide a successful download confirmation. No outside temporary download directory was accessed; the user requires project-local paths.

During README capture, long SQL was found to push request-detail actions beyond the dialog. A new browser scenario first failed with the Submit button ending at x=8122 outside the dialog's x=896 edge. Allowing the detail body to shrink fixed the layout while keeping the SQL scrollable.
The staged snapshot then passed frontend typecheck/lint, 104 unit tests and all five focused authentication/request browser tests, including that regression. A subsequent complete gate passed build, vet, lint (zero issues), all Go tests, frontend checks and 104 unit tests. Its browser suite passed 8 of 9 scenarios, including this regression; native CSV saving alone still returned `canceled`.
Both Go DB tests and the browser database now use Testcontainers. The browser harness publishes ephemeral target coordinates over a loopback-only fixture endpoint and cleans up its owned database and server. An earlier coordinate-file attempt was refused by this execution environment even inside the project; this is evidence of a file-write restriction, not proof of its operating-system cause.

The README walkthrough capture additionally exercised a distinct requester and reviewer, single-use approved execution, exact-value result paging/sorting/filtering and preparation of all 30 synthetic CSV rows despite a 10-row filter. Screenshots and GIFs document those interactions; they do not certify native file saving or substitute for `make verify`.

## Page-first workflow verification

ADR-0022 is implemented: request composition, details/review and results have reloadable routes and back links; connection creation, descriptor details/edits and policy settings use in-page panels. Only credential-destroying connection archive confirmation remains modal.
The request navigation scenario first failed because New request stayed at `/requests`, then passed after routing was implemented. The connection scenario first failed because creation rendered one dialog, then passed after in-page conversion.
Browser verification also exposed a rapid edit re-entry race; duplicate edit admission is now disabled until the current panel closes, and the scenario waits for actual save completion.

The final `make verify` after these changes passed build, vet, lint (zero issues), all Go tests including Testcontainers database coverage, frontend typecheck/lint, 104 frontend tests and load-script typecheck. Browser E2E passed 9 of 10, including direct reload/history, long SQL, draft recovery and connection/policy flows.
Its sole remaining failure is native CSV saving returning `canceled`; suggested filename and prepared whole-snapshot content pass, and native file-content readback remains enabled. The actual README screenshots and the six-frame workflow/five-frame result GIFs were regenerated and inspected for these page layouts.

## Editable SQL formatting verification

ADR-0023 adds local PostgreSQL formatting to new-request and draft-edit fields, defaulting to format-on-blur with opt-out, manual formatting and undo. Stored review SQL remains unchanged. The new pure scenarios first observed failures for indentation, incomplete-input preservation and byte admission against the initial no-op implementation, then passed after implementation.
Coverage additionally retains named/native placeholders, casts, exact literal bodies, nested comments and adjacent string literals whose whitespace may carry SQL meaning.

Build, vet, lint (zero issues), all Go tests including Testcontainers coverage, frontend typecheck/lint, 109 frontend tests and load-script typecheck passed during the full verification run. The initial browser run exposed ambiguous substring labels after introducing the Auto-format SQL checkbox; selectors were narrowed to the exact SQL field.
The subsequent complete browser suite passed 10 of 11, including automatic formatting, undo, explicit save/reload and failed formatting in draft edits. Its sole remaining failure is the existing native CSV `canceled` assertion, which remains enabled. README source and actual PNG/GIF files were refreshed together from the real application, with full-page capture and consistent GIF frame dimensions.

## Recorded governance gate — Docker-hosted Chromium

The complete `make verify` command passed with the application and test runner on the host, PostgreSQL owned by Testcontainers, and Chromium supplied by Playwright 1.61.1 in `mcr.microsoft.com/playwright:v1.61.1-noble@sha256:5b8f294aff9041b7191c34a4bab3ac270157a28774d4b0660e9743297b697e48`. The passing code includes commit `345ae70` (lockout scenario correction).
Build, vet, lint (zero issues), frontend typecheck/lint, all 109 frontend unit tests, load-script typecheck and all Go tests passed. All 11 browser E2E scenarios passed in 35.8 seconds.

A standalone eight-byte CSV first passed native saving and saved-file readback in this browser. The real product scenario then passed whole-snapshot CSV preparation, the native download event, filename, `download.failure() === null` and file-stream readback containing exact large integers and formula-safe cells. No saving assertion was disabled or replaced with a prepared-Blob fetch.

The first complete run with this browser exposed an independent lockout-test defect: the previous scenarios could exhaust the shared login IP bucket, while the test treated its uniform error message as a counted password failure. A strengthened real-response assertion observed `429` instead of `401`.
Waiting for one token before the first attempt, as already done between attempts, fixed the scenario; every wrong-password and locked correct-password attempt must now receive `401`. Production admission, lockout timing and security rules are unchanged.

Only the project was mounted into the disposable browser container: source read-only, test downloads writable. The remote endpoint was bound to host loopback, and the Docker socket was not mounted into the browser. Host-native Chromium still reproduced `canceled`, including when both temporary and download paths were project-local; the OS cause remains undiagnosed.
The passing Linux browser gate does not imply host-native macOS saving was repaired. The database harness removes its owned fixtures at shutdown; the disposable browser container is stopped after verification. See the [reproduction guide](../../operations/browser-e2e.md).

## Reproduction

Use a project-local Docker configuration with `{"auths":{}}` to avoid reading unrelated home configuration. Point `DOCKER_CONFIG` at it and `DOCKER_HOST` at the explicitly permitted Docker socket. Set `CI=true` for the noninteractive pnpm install and `PLAYWRIGHT_BROWSERS_PATH` to a project-local browser installation, then run `make verify`.
Playwright's configured download directory is project-local as well. For the passing container-browser setup, follow the [Docker browser E2E guide](../../operations/browser-e2e.md). The Docker socket and Go build cache require the path-specific permissions established for this session.

## Separate qualification and later scope

- The recorded PostgreSQL governance correctness verification passed in the recorded Docker browser environment; the result does not qualify host-native macOS downloads.
- Per [ADR-0020](../../adr/0020-scenario-load-testing.md), 50-user latency targets, a total Go-heap bound and hardware recommendations require separate load/soak qualification.
- This record covers the PostgreSQL governance journey and does not qualify other product capabilities.

## Current milestone status

The evidence above predates the expanded M1 administration boundary.
See [progress](progress.md) for implementation status; these historical passes do not establish current M1 release readiness.

## Verification — 2026-10-05

The full `make verify` gate passed after documentation restructuring and result-control changes: static checks in 47 seconds, uncached Go tests in 101 seconds and browser E2E in 158 seconds.
Result scenarios verified aligned filter controls and complete wrapped Text values at 320, 480, 768 and 1280 px in both light and dark themes. Native CSV download and file readback passed with the same real embedded binary and disposable PostgreSQL fixture.
The tracked capture tools refreshed and visually checked nine application screenshots and eleven GIF frames; login remained visually identical, while first-run setup includes its one-time token and authenticated screens include the current account avatar.
`make supply-chain` passed module verification, both package audits, govulncheck reachable-symbol analysis and fresh-install script rejection. Govulncheck also reported one package/module advisory outside the called symbols; this is not a claim that all transitive advisory records are absent.
Per [scope.md](scope.md), these passes establish the recorded implementation and do not establish full M1 release readiness.

## Verification — 2026-10-18

The dependency-recovery run matching runtime commit `0d9acc4` passed `make verify` on Go 1.27.2 and SolidJS 1.9.17: static checks in 245 seconds, uncached Go tests with required PostgreSQL fixtures in 173 seconds, and all 38 real-binary Docker-browser scenarios in 184 seconds. Web verification passed 310 tests across 31 files; load typecheck, lint, bundling and all 10 contract scenarios also passed. Native CSV saving and saved-file readback remain part of the browser gate.

`make supply-chain` passed both npm audits, fresh-install script rejection, module integrity and reachable-symbol analysis. The npm audits reported no known vulnerabilities and govulncheck reported zero reachable vulnerabilities. The latter still reported one imported-package and one required-module advisory outside called vulnerable symbols; no advisory or installation policy was suppressed.

Linux AMD64 source analysis with a host-built golangci-lint v2.14.0 and Go 1.27.2 completed with zero issues. Analyzer cache-write warnings were nonfatal. On this macOS host, an explicitly permitted disposable OS temporary directory resolved project-directory temporary-write failures; module/build caches and browser downloads remained project-local. Docker used the explicitly permitted socket and project-local configuration, including the installed Buildx plugin. The [Docker browser recipe](../../operations/browser-e2e.md) retains the host-native download qualification boundary.

[ADR-0062](../../adr/0062-role-bindings-and-conditions.md) and [ADR-0063](../../adr/0063-condition-dsl-and-pure-evaluation.md) define the planned authorization replacement. This verification establishes the currently implemented PostgreSQL governance journey and the recovered gates; it does not deliver bindings, a CEL evaluator, condition forms or their migration, and it does not establish full M1 release readiness.

## Binding-authorized administration qualification — 2026-10-18

[ADR-0068](../../adr/0068-binding-authority-account-and-role-management.md)'s account and shared-role mutation scenarios first failed behaviorally against unimplemented services. Actual PostgreSQL qualification then passed independent grantless account creation and setup-link replacement, disable/enable with preserved grants, reusable role creation/update/deletion, binding-subject session revocation, complete delegation and recovery refusals, bounded fanout and atomic rollback of account/role/session/revision/audit/token writes. Qualification databases alone remove the legacy role column; separate scenarios retain it and verify unchanged assignments and grants. Runtime management remains refused.

Database-observed lock waits qualify reauthorization after authority changes, cancellation, post-session-lock timestamps and competing Role/Binding removal of recovery capabilities. An additional scenario reproduced login session insertion timing out because account administration held an incompatible reference lock while waiting for login's coordinator. Replacing that account lock with `FOR NO KEY UPDATE` made the admitted login complete and the subsequent disable revoke its newly inserted session. No production login or legacy administration path was switched.

Six deliberate guard defects were independently detected by the scenarios before restoration: self-account changes, omitted affected binding permissions, omitted old/new role permissions, bypassed recovery and bypassed shared-role fanout limits. These tests qualify the prepared mutation slice; they do not qualify production activation, migrated bootstrap/session admission, authorized collection/output behavior, management APIs or M1 completion.

The shuffled full suite also exposed a shared-fixture execution test recovering expired leases left by other scenarios. Its exact single-lease recovery assertion now runs in a fresh database; production reconciliation and the assertion remain unchanged. A valid-CEL preparation refusal observed while the separate race suite overlapped full verification did not justify widening production evaluator limits; the final full gate runs without that additional competing suite.

## Prepared management API qualification — 2026-10-18

[ADR-0069](../../adr/0069-prepared-administration-rpc-contracts.md)'s raw TLS/Connect scenarios first observed unimplemented user creation and status RPCs. Actual generated clients, sessions, HMAC CSRF, PostgreSQL and CEL then qualified account/setup/status, custom-role and organization/connection Binding journeys. They cover conditional reads, expired/scoped authority refusal, foreign resources, forged actor/organization claims, canonical UUID aliases, explicit edits and stale versions, unknown condition-source preservation, caller-session revocation, recovery races and atomic audit failure rollback. These owner-only databases retire the legacy assignment column for qualification; a separate owner-ready prepared-route scenario retains it and verifies unchanged legacy grants.

Additional red scenarios exposed protected account/condition/role metadata in write responses without the corresponding read permission. Write acknowledgments now contain only account identity/status or policy identity/version; details require their own Get permission. Semantic CEL errors remain invalid input while operational preparation refusal becomes Unavailable. A canceled-read scenario exposed cancellation being replaced by a generic storage refusal; context admission and completion fencing now preserve cancellation with no data.

A coordinated metadata writer between authorization and collection reads proves coherent rows, totals and revision in one repeatable-read snapshot, followed by refusal on a fresh revoked read. Five deliberately weakened guards were independently detected and restored: snapshot isolation, page revision, explicit condition removal, the mandatory pending-production barrier and canonical UUID validation. The browser scenario additionally probes every prepared procedure behind real-binary authentication/CSRF and verifies unchanged Auth.Me authority. Management UI, migrated bootstrap/session admission, governance/collection/output cutover, production backfill and atomic activation remain separate M1 obligations.


## Prepared management UI qualification — 2026-10-18

[ADR-0070](../../adr/0070-prepared-administration-ui.md) adds ordinary user/role/binding workflows using the actual generated binary clients. Initial browser red observed absent Users routes. Deterministic red scenarios observed old page responses winning, protected data surviving reset/refusal, and invalid condition choices yielding empty source; a separate red exposed JavaScript normalizing an invalid calendar date into a different day. A revoked-read red required re-resolving session admission, and a browser red exposed missing generated IDs for create-only operators. These scenarios require latest-response ownership, immediate protected-data reset and explicit restricted condition generation.

UI contract fixtures qualify independent list/Get/create capabilities, direct reloads and history, numbered revision continuations, transient setup receipts, reusable Role bundles, scoped conditional grants, source-preserving edits and explicit condition removal. System roles and unknown condition contracts remain read only, while self changes are absent. Last-administrator refusal, stale versions, service/authority rejection and malformed inputs never imply success or silently retry a write. Deliberately removing lifetime cleanup and uncertain-outcome replay locks independently failed the new scenarios before restoration. Deterministic route-disposal scenarios qualify validation/write and late-token fencing; these fixtures are interaction evidence, not successful production authorization or migration evidence.

The first full gate also detected two initial scripts on sign-in after automatic common-chunk extraction; explicit initial dependency grouping preserves the original lazy-loading assertion.

The separate real-binary management scenario still requires actual session/CSRF checks before every prepared procedure's inactive response, displays unavailable user/role pages and denies Binding navigation without its unseeded capability, then verifies unchanged Auth.Me authority. Existing real PostgreSQL/TLS/CEL API scenarios retain delegation, last-administrator race, session revocation and rollback coverage. Coordinated activation and production UI mutation acceptance remain pending.

## Atomic Binding authorization activation qualification — 2026-10-18

[ADR-0071](../../adr/0071-binding-authorization-activation.md) activates the migration, bootstrap, session capabilities, administration, governance and protected collection/output adapters together. Migration scenarios qualify current/custom/disabled/changed assignments, independent bootstrap, exact grant preservation, the retired assignment constraint and atomic mixed-policy/recovery refusal. Existing delegation, immutable policy history, affected-session revocation and competing last-administrator scenarios remain enabled.

Observed behavioral red scenarios exposed missing workflow authorization evidence, bootstrap token consumption before a policy-lock wait, transaction-start request expiry filtering, stale management metadata after revision/condition changes and session metadata released after revocation. Each passed after its corresponding guard was implemented. Deliberately weakened connection-read, requester-heartbeat, scoped-capability compatibility, whole-read deadline and session-output guards independently failed their scenarios before restoration; these mutation results are distinct from the originally observed red scenarios.

Real PostgreSQL and restricted CEL scenarios qualify organization/connection/additive grants, requester privacy, fresh quorum and requester checks at execution admission and heartbeat, bounded scans and lock deadlines, durable result ancestry, current output authority and cache ownership/TTL. The real-binary browser journey creates a grantless account, reusable Role and conditional Binding, revokes/deletes/disables them, refuses removal of the caller's recovery Binding and verifies preserved caller grants with an advanced revision. It does not qualify password setup consumption.

The complete `make verify` gate passed static checks in 60 seconds, uncached Go tests with required PostgreSQL fixtures in 140 seconds and all 51 real-binary browser scenarios in 170 seconds. Static checks included zero lint issues, generated-source consistency, frontend typecheck/lint, 345 unit tests across 34 files and load contract checks. CSV native saving and exact saved-file readback remain enabled. The first full run exposed a malformed-resource error-contract regression (400 instead of the existing permission-denied response); the passing run includes its correction.

`make supply-chain` passed both npm audits, fresh-install script rejection, module integrity and govulncheck reachable-symbol analysis. The npm audits reported no known vulnerabilities; govulncheck reported zero reachable vulnerabilities while retaining one imported-package and one required-module advisory outside called vulnerable symbols.

A separate uncached shuffled `make test-race` run passed the changed authorization, administration, PostgreSQL and Connect transport packages with their required databases. This supplements the full ordinary suite; it is not a claim that every repository package ran under the race detector. Staged review checked the single grant source, composition-root wiring, lock order, current output checks, protected history, scenario coverage and the absence of private artifact references.

A later full repeat surfaced bounded-read failures in two browser journeys during concurrent qualification. Four focused red cases then exposed active snapshot/overflow/cancellation/deadline failures being classified as Internal at admission. Active reads now retain an operational marker and the original cancellation/deadline identity; admission preserves their distinct wire codes while unknown permission defects remain Internal. The focused PostgreSQL and transport scenarios passed after this correction. Decision/scan budgets and browser assertions remain unchanged.

These passes establish the coordinated authorization activation, not full M1 completion or capacity beyond the configured candidate/time budgets. One-time password setup consumption and remaining result/export/account UX and release acceptance retain separate obligations in [scope.md](scope.md).

## Initial password setup qualification — 2026-10-18

[ADR-0072](../../adr/0072-password-setup-consumption.md) completes initial password registration through the public Auth RPC and a fragment-based setup page. Application success cases first observed the invalid-link placeholder; actual PostgreSQL success cases then observed the same refusal. Generated-client success cases observed unauthenticated public admission before the route was connected. UI lifetime/link scenarios first observed twelve behavioral failures against their initial stubs; a separate throttling scenario observed an uncertain-commit state for a confirmed pre-admission refusal.

Required PostgreSQL scenarios now cover grantless and bound members, retained OIDC identity, session invalidation, unknown/expired/reused/revoked/replaced tokens, disabled and nonmember targets, one concurrent consumer and retry after atomic audit rollback. Lock-wait scenarios use database-observed contention and require a post-lock consumption/password/audit instant. A deliberately substituted pre-expiry instant admitted a token that expired during the wait and failed its scenario; the guard was restored before qualification. Separate focused race qualification passed the changed authentication, PostgreSQL and Connect transport scenarios with required databases.

The UI keeps token/password state within the route, removes the fragment without consuming on GET, clears terminal secrets, prevents duplicate submissions, ignores disposed responses and requires explicit retry for an uncertain write. Confirmed throttling preserves an explicit retry without an automatic credential write. The real-binary browser scenario requires issuer-origin link construction, no-referrer delivery, fragment removal, no browser storage, password confirmation, no automatic session, normal login, replay refusal and foreign-origin refusal. It retains existing password, session, CSRF, Binding and last-administrator contracts.

The first full browser run observed a same-document fragment navigation leaving the previous refused form mounted after a clean-URL reload. Incoming replacement links now dispose the previous flow and clear its inputs before capturing the new fragment; late completion cannot overwrite the replacement flow.

After that correction the dedicated real-binary browser journey passed, including clean-URL reload, same-document replacement, normal login and replay/origin refusal. Further PostgreSQL assertions require foreign-organization refusal, unchanged OIDC identity and preserved sessions/token/password after audit failure. Separate race verification passed the final authentication, PostgreSQL and transport scenarios. `make supply-chain` passed both npm audits, fresh-install script rejection, module integrity and zero reachable vulnerabilities in govulncheck; the existing imported-package/module advisory outside called vulnerable symbols remains reported.

The complete repository and supply-chain gates remain mandatory before commit. This qualification completes password setup consumption; bounded result scrolling, CSV destinations, account UX and release acceptance remain separate M1 obligations in [scope.md](scope.md).

The corrected complete gate passed static checks in 54 seconds, uncached Go tests with required databases in 147 seconds and all 52 real-binary browser scenarios in 170 seconds, including 362 frontend unit tests across 36 files, load checks and zero lint issues. Final staged review additionally strengthened origin admission checks to use a still-valid token before successful registration, so foreign or missing origin evidence cannot be mistaken for a consumed-token refusal. No browser assertion or required database gate was disabled.
