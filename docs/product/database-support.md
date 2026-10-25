# Database feature support

**Scope updated: 2026-10-25; engine evidence retains the dates below.** This table describes Portcullis product support, not whether an engine can run in Docker. PostgreSQL governance has recorded passing evidence; M1 release readiness remains incomplete. MySQL is the next committed target; it is not selectable or supported yet.
[ADR-0025](../adr/0025-sql-database-first-expansion.md) limits committed targets to PostgreSQL/MySQL and removes SQLite from support scope.

## Committed target feature matrix

**Verified** means covered by the recorded PostgreSQL governance gate, not completion of the expanded M1 scope. **Planned** identifies future milestone scope, with no product implementation claim. MySQL governance parity requires the shared acceptance gate; later review/preview features have their own gates. Engine CLI experiments do not count as Portcullis acceptance.

| Feature | PostgreSQL | MySQL |
| --- | --- | --- |
| Register/test/edit/archive connections; encrypted credentials | Verified | Planned |
| TLS certificate verification; explicit audited relaxation | Verified | Planned |
| Single-statement AST classification; fail-closed rejection | Verified | Planned |
| Named, typed parameter binding and SQL redaction | Verified | Planned |
| Read/write/DDL policies and approval quorums | Verified | Planned |
| Draft, immutable submission, distinct approval/rejection | Verified | Planned |
| Requester-only single-use execution; target/policy revalidation | Verified | Planned |
| Read-only enforcement and governed DML/DDL | Verified | Planned; implicit-commit DDL warning required |
| Exact integer/decimal/NULL results and typed cells | Verified | Planned |
| Server-enforced row/byte/protocol limits | Verified | Planned |
| Encrypted expiring snapshots; paging/sorting/filtering | Verified | Planned |
| Requester-only CSV download and saved-file readback | Verified | Planned |
| Timeout/cancel attempts; unknown outcome without automatic rerun | Verified | Planned |
| Organization isolation, permissions and append-only audit | Verified | Planned |
| Real-engine integration tests and real-binary browser E2E | Verified (Testcontainers; Docker Chromium) | Planned (Testcontainers) |
| Saved queries, versions, favorites and sharing | Planned (M3) | Planned (M3) |
| Kubernetes deployment (Helm/Kustomize) | Planned M2 immediately after MySQL parity | Planned M2 immediately after MySQL parity |
| CNPG integration | Planned M2: metadata PG 18 and governed PG targets; no automatic discovery | External MySQL target from Kubernetes; CNPG manages PostgreSQL only |
| Deterministic SQL review facts | Planned (M2 after deployment gate) | Planned (M2 after deployment gate) |
| Basic EXPLAIN for supported read queries | Planned (M2; no ANALYZE) | Planned (M2; no ANALYZE) |
| Schema migration status and SQL dry-run preview | Planned (M3; no apply) | Planned (M3; no apply) |
| Deterministic schema impact facts | Planned (M3; estimates/unknowns) | Planned (M3; estimates/unknowns) |
| EXPLAIN ANALYZE / execute-then-rollback query dry-run | Deferred; no support claim | Deferred; no support claim |
| Initial connector protected output, verified registration and expiring human delegation | Planned M2 after SQL review; default-deny output first | Planned M2 after SQL review; same security gate |
| External ChatGPT / Claude connectors and dual-identity UX/audit | Planned M2; real client/version/protocol/auth journeys required | Planned M2; same compatibility gate |
| Broader server-side sensitive-data masking (cells/API/CSV/metadata) | Planned M4; extends the M2 minimum gate, separate from SQL audit redaction | Planned M4; same disclosure gate |
| Expanded Gateway providers/clients and local stdio bridge | Planned M6 after M5 and broader masking qualification | Planned M6; same security gate |
| Local clients / Claude Code / Codex | M6 expansion targets; not verified | M6 expansion targets; not verified |
| Optional browser WebMCP adapter | Planned M6 follow-up; identity/browser gates required | Planned M6 follow-up; same gates |
| Expanded Gateway Helm/Kustomize deployment examples | Planned M6 on the M2 deployment baseline | Planned M6 on the M2 deployment baseline |
| Charts/dashboards | Later candidate | Later candidate |
| Git-sourced schema migration governance | Planned (M5; object matrix pending) | Planned (M5; object matrix pending) |
| Native DB client proxy / temporary web SQL console | Proxy deferred; console planned (M4) | Proxy deferred; console planned (M4) |

Verified does not mean every SQL statement, extension or engine version is accepted. Only explicitly classified forms pass; transaction/session control, native file/network I/O, multiple statements and unclassified forms are rejected. Cancellation is an attempt, not a rollback guarantee. MySQL support requires the complete shared security/behavior gate before UI exposure.

## Version and evidence boundaries

[ADR-0030](../adr/0030-database-version-qualification-window.md) sets PostgreSQL compatibility maintenance to **16/17/18/19**, preserving current Portcullis behavior without promising engine-specific feature expansion. PostgreSQL has an explicit four-family exception to the initial three-family proposal; MySQL retains at most three candidate families.
Later expansion requires a scope decision and acceptance evidence. Candidates are not supported-version claims. The metadata deployment baseline remains PostgreSQL 18, independently of managed-target coverage.

| Engine | Family | Product evidence / qualification status |
| --- | --- | --- |
| PostgreSQL | 16 (current upstream patch: 16.15) | GA qualification target; full family gate pending |
| PostgreSQL | 17 (17.11) | GA qualification target; full family gate pending |
| PostgreSQL | 18 (18.6) | [M1 correctness evidence](../milestones/m1/validation.md) on the recorded 18 stack; latest-patch requalification pending |
| PostgreSQL | 19 Beta 4 | Included in the maintenance window; preview qualification until planned GA 2026-10-29, then GA requalification; full gate pending |
| MySQL | 8.4 LTS | Priority qualification target; product driver pending. CLI experiment on 8.4.10 is not product acceptance |
| MySQL | 9.7 LTS | Priority qualification target; product driver and full gate pending |
| MySQL | 26.7 Innovation | Third qualification candidate; product driver and full gate pending |
| MySQL | 8.0 | Outside target window; upstream Sustaining Support since 2026-04-21 |

Use current security patches in each qualified family and pin test images by digest. Engine version, patch, test results and skips must accompany support evidence. Parser coverage remains an explicit allow-list; newer server versions do not authorize unclassified SQL. PostgreSQL 19 does not remove 16 from scope.
Qualification status is separate from the maintenance window; no new syntax/extension support is promised. Family retirement or wider coverage requires an explicit scope decision and announced migration guidance.

Primary sources checked 2026-10-03: [PostgreSQL versions](https://www.postgresql.org/support/versioning/), [19 schedule](https://wiki.postgresql.org/wiki/PostgreSQL_19_Open_Items), [MySQL release tracks](https://dev.mysql.com/doc/refman/9.7/en/mysql-releases.html), [26.7 notes](https://dev.mysql.com/doc/relnotes/mysql/26.7/en/) and [8.0 lifecycle](https://www.mysql.com/support/eol-notice.html).

The M1 gate establishes correctness for its recorded environment. Capacity/soak qualification is separate. Library (M3) remains the MVP boundary, including PostgreSQL/MySQL parity, Kubernetes/CNPG deployment, basic SQL review/EXPLAIN, initial M2 external ChatGPT/Claude connectors and M3 schema preview. The M2 order is MySQL parity → Kubernetes/CNPG → SQL review → initial connector acceptance.

Inside that final track, minimum protected output precedes registration, explicit expiring human delegation and client integration; every call intersects current human Binding and verified agent authority. Preserve human review/quorum, no self-approval, immutable single-use execution, revocation/expiry and ongoing-output fencing without SQL replay. Planned identity UI shows the connected human's small avatar on the AI avatar's lower-right corner, exposes both identities accessibly and records both in audit.

These are future M2 acceptance criteria, not shipped or M1 capabilities. Broader masking remains M4; provider/client/stdio and optional browser WebMCP expansion remain M6. See [ADR-0090](../adr/0090-early-delegated-ai-connectors.md) and [PRD §4.13](prd.en.md#413-agent-neutral-mcp-gateway-and-deployment-m2--m6).

## Additional engines and excluded scope

| Engine | Product status | Potential feature scope / condition |
| --- | --- | --- |
| MariaDB | Candidate; native CLI experiment passed | Evaluate the complete MySQL-style governance journey with an independent parser, type, TLS and security matrix; no product support yet |
| ClickHouse | Research candidate | Consider governed analysis reads first; write/DDL and transaction guarantees require a separate decision |
| SQL Server | Research candidate | T-SQL connection/classification/execution/result contracts; appropriate x86-64 CI required |
| TiDB | Research candidate | Independently qualify MySQL-compatible SQL, types, locks and DDL before feature claims |
| CockroachDB | Research candidate | Independently qualify PostgreSQL-family contracts and transaction-retry behavior without automatic SQL reruns |
| SQLite | Excluded | No adapter or roadmap commitment; reintroduction requires a new scope decision |
| MongoDB | Outside the SQL-first scope | A separate document-operation governance model is required |

These candidate scopes are evaluation directions, not delivery promises. See [primary-source research and measured experiments](database-candidates.md) for evidence.
