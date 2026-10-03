# M1 implementation and validation — 2026-10-03

The PostgreSQL M1 implementation is present, but its release gate is not complete. Native CSV file saving remains a failing browser assertion; do not label this checkout as a verified releasable alpha.

## Implemented scope

- Single-use approved execution with digest, pinned policy/configuration, quorum and requester checks; atomic lease/start evidence; heartbeat, recovery and late-completion fencing.
- Target timeout, read-only reads, cancellation, conservative catalog candidate proof, row/payload/protocol limits, separate pre-decode cell-memory admission, two execution workers and per-connection circuit breaking.
- Encrypted UNLOGGED result chunks with owner/org boundaries, TTL, quotas and eviction; bounded exact-value sorting/filtering/paging and formula-safe CSV streaming.
- Result grid, full cells, execution controls and 30-second approval/state fallback polling.
- Resumable multi-version key rotation, with historical keys retained for digest evidence.

See [ADR-0021](../adr/0021-governed-query-execution.md), [quickstart](pg-alpha-quickstart.md) and [key rotation](key-rotation.md).

## Verification evidence

Build, vet, lint (zero issues), frontend typecheck/lint, 104 frontend unit tests, load-script typecheck and all Go tests passed during `make verify`. Targeted race checks passed for execution, result processing, circuit admission, PostgreSQL persistence, Connect execution/security, and governed target limits. Behavioral coverage includes tampered wire payload refusal before leasing, concurrent ownership, audit rollback, overdue-owner refusal, unknown outcomes without retry, cache loss preserving success, precision/NULL/non-finite sorting, formula escaping, key rotation (including a newly observed cancelled-call red scenario fixed by checking cancellation before batch admission), oversized cells, wide NULL-row admission and returning writes committing the whole statement despite result truncation.

At the original full-gate run, the Chromium suite passed 7 of 8 tests. The new execution scenario passes execution, exact-value paging/sorting/filtering, generated whole-snapshot CSV content and suggested filename checks; `download.failure()` then returns `canceled`, so actual file saving is not verified. The assertion and file-content readback remain enabled.

A standalone eight-byte Blob download and a standalone ordinary HTTP attachment both reproduce `canceled` with a project-local download directory; regular Chromium and headless shell both reproduce the Blob failure. This establishes a failure outside the product-specific export path, but does not establish the operating-system cause. An independent Firefox attempt also did not provide a successful download confirmation. No outside temporary download directory was accessed; the user requires project-local paths.

During README capture, long SQL was found to push request-detail actions beyond the dialog. A new browser scenario first failed with the Submit button ending at x=8122 outside the dialog's x=896 edge. Allowing the detail body to shrink fixed the layout while keeping the SQL scrollable. The staged snapshot then passed frontend typecheck/lint, 104 unit tests and all five focused authentication/request browser tests, including that regression. A subsequent complete gate passed build, vet, lint (zero issues), all Go tests, frontend checks and 104 unit tests. Its browser suite passed 8 of 9 scenarios, including this regression; native CSV saving alone still returned `canceled`. Both Go DB tests and the browser database now use Testcontainers. The browser harness publishes ephemeral target coordinates over a loopback-only fixture endpoint and cleans up its owned database and server. An earlier coordinate-file attempt was refused by this execution environment even inside the project; this is evidence of a file-write restriction, not proof of its operating-system cause.

The README walkthrough capture additionally exercised a distinct requester and reviewer, single-use approved execution, exact-value result paging/sorting/filtering and preparation of all 30 synthetic CSV rows despite a 10-row filter. Screenshots and GIFs document those interactions; they do not certify native file saving or substitute for `make verify`.

## Page-first workflow verification

ADR-0022 is implemented: request composition, details/review and results have reloadable routes and back links; connection creation, descriptor details/edits and policy settings use in-page panels. Only credential-destroying connection archive confirmation remains modal. The request navigation scenario first failed because New request stayed at `/requests`, then passed after routing was implemented. The connection scenario first failed because creation rendered one dialog, then passed after in-page conversion. Browser verification also exposed a rapid edit re-entry race; duplicate edit admission is now disabled until the current panel closes, and the scenario waits for actual save completion.

The final `make verify` after these changes passed build, vet, lint (zero issues), all Go tests including Testcontainers database coverage, frontend typecheck/lint, 104 frontend tests and load-script typecheck. Browser E2E passed 9 of 10, including direct reload/history, long SQL, draft recovery and connection/policy flows. Its sole remaining failure is native CSV saving returning `canceled`; suggested filename and prepared whole-snapshot content pass, and native file-content readback remains enabled. The actual README screenshots and the six-frame workflow/five-frame result GIFs were regenerated and inspected for these page layouts.

## Reproduction

Use a project-local Docker configuration with `{"auths":{}}` to avoid reading unrelated home configuration. Point `DOCKER_CONFIG` at it and `DOCKER_HOST` at the explicitly permitted Docker socket. Set `CI=true` for the noninteractive pnpm install and `PLAYWRIGHT_BROWSERS_PATH` to a project-local browser installation, then run `make verify`. Playwright's configured download directory is project-local as well. The Docker socket and Go build cache require the path-specific permissions established for this session.

## Remaining gate and qualification

- Resolve native browser file saving under the project-only filesystem boundary and pass the entire `make verify` gate.
- Run ADR-0020 load/soak qualification before claiming 50-user latency targets, a total Go-heap bound or hardware recommendations.
- MySQL/SQLite, saved queries and later milestones are outside this implementation.
