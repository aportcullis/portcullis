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

Build, vet, lint (zero issues), frontend typecheck/lint, 104 frontend unit tests, load-script typecheck and all Go tests passed during `make verify`. Targeted race checks passed for execution, result processing, circuit admission, PostgreSQL persistence, Connect execution/security, and governed target limits. Behavioral coverage includes tampered wire payload refusal before leasing, concurrent ownership, audit rollback, overdue-owner refusal, unknown outcomes without retry, cache loss preserving success, precision/NULL/non-finite sorting, formula escaping, key rotation, oversized cells, wide NULL-row admission and returning writes committing the whole statement despite result truncation.

At the original full-gate run, the Chromium suite passed 7 of 8 tests. The new execution scenario passes execution, exact-value paging/sorting/filtering, generated whole-snapshot CSV content and suggested filename checks; `download.failure()` then returns `canceled`, so actual file saving is not verified. The assertion and file-content readback remain enabled.

A standalone eight-byte Blob download and a standalone ordinary HTTP attachment both reproduce `canceled` with a project-local download directory; regular Chromium and headless shell both reproduce the Blob failure. This establishes a failure outside the product-specific export path, but does not establish the operating-system cause. An independent Firefox attempt also did not provide a successful download confirmation. No outside temporary download directory was accessed; the user requires project-local paths.

During README capture, long SQL was found to push request-detail actions beyond the dialog. A new browser scenario first failed with the Submit button ending at x=8122 outside the dialog's x=896 edge. Allowing the detail body to shrink fixed the layout while keeping the SQL scrollable. The staged snapshot then passed frontend typecheck/lint, 104 unit tests and all five focused authentication/request browser tests, including that regression. The complete browser gate has not been rerun after adding this scenario; the native-download gate remains unresolved.

The README walkthrough capture additionally exercised a distinct requester and reviewer, single-use approved execution, exact-value result paging/sorting/filtering and preparation of all 30 synthetic CSV rows despite a 10-row filter. Screenshots and GIFs document those interactions; they do not certify native file saving or substitute for `make verify`.

## Reproduction

Use a project-local Docker configuration with `{"auths":{}}` to avoid reading unrelated home configuration. Point `DOCKER_CONFIG` at it and `DOCKER_HOST` at the explicitly permitted Docker socket. Set `CI=true` for the noninteractive pnpm install and `PLAYWRIGHT_BROWSERS_PATH` to a project-local browser installation, then run `make verify`. Playwright's configured download directory is project-local as well. The Docker socket and Go build cache require the path-specific permissions established for this session.

## Remaining gate and qualification

- Resolve native browser file saving under the project-only filesystem boundary and pass the entire `make verify` gate.
- Run ADR-0020 load/soak qualification before claiming 50-user latency targets, a total Go-heap bound or hardware recommendations.
- MySQL/SQLite, saved queries and later milestones are outside this implementation.
