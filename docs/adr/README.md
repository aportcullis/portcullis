# Architecture Decision Records (ADR)

These records capture the *how* of Portcullis's implementation — the binding technical choices behind the product requirements.
Each ADR states its own context and constraints so it can be read on its own.

## Status values
`Proposed` → `Accepted` → (when replaced) `Superseded by ADR-XXXX`

Status describes whether a decision is proposed, in force or replaced; it does not describe implementation or test completion.
Track implementation and validation in the relevant milestone records.
Per [MADR guidance](https://adr.github.io/madr/), status metadata is optional; Portcullis retains these three values to identify the currently applicable decision.

## Changing an accepted decision

Accepted decision bodies remain unchanged; a new ADR supersedes a changed decision and explains the transition.
Update the old record only with its replacement status/link, except for narrow factual corrections and prose formatting.
Preserve existing amendment history rather than rewriting it in bulk.
Per [ADR-0054](0054-release-branches-and-documentation-policy.md), PRDs define product requirements, milestone scopes define completion criteria and ADRs define technical decisions.

## Foundation ADRs
These decisions establish the Core 1 foundation.

| ADR | Topic | Status |
|---|---|---|
| [0001](0001-db-driver-and-parser.md) | Managed-DB driver & SQL parser | Accepted |
| [0002](0002-statement-classification.md) | Statement classification table & test fixtures | Accepted |
| [0003](0003-key-management.md) | Key management (master key, rotation, payload digest) | Accepted |
| [0004](0004-metadata-rls.md) | Metadata RLS — apply or not | Accepted |
| [0005](0005-cellvalue-wire-contract.md) | CellValue / ColumnMeta wire contract | Accepted |

## Feature ADRs
| ADR | Topic | Status |
|---|---|---|
| [0006](0006-authentication-and-sessions.md) | Authentication & sessions (cookies, CSRF) | Accepted |
| [0007](0007-social-login-google-oidc.md) | Social login via Google (OIDC) | Superseded by ADR-0057 |
| [0008](0008-rbac-roles-and-permissions.md) | RBAC — permissions in SQL catalog, roles in DB | Accepted |
| [0009](0009-audit-integrity.md) | Audit integrity — same-tx delivery, runtime role boundary | Accepted |
| [0010](0010-runtime-and-transport-defaults.md) | Runtime & transport defaults (timeouts, caps, rate limits, boot) | Accepted |
| [0011](0011-result-store-quota-and-eviction.md) | Result store quota & eviction (gates Core 2) | Accepted |
| [0012](0012-schema-governance-ops.md) | Schema governance ops — Atlas pin, artifacts, apply safety (gates M5) | Accepted |
| [0013](0013-spa-ui-foundation.md) | SPA UI foundation — light FSD, vendored solid-ui | Accepted |
| [0014](0014-connection-registration-and-tls.md) | Connection registration, credential envelope & TLS validation | Accepted |
| [0015](0015-connection-policies.md) | Connection policies — per-class approvals, limits, immutable versions | Accepted |
| [0016](0016-sql-redaction-and-named-binding.md) | SQL redaction & named-parameter binding (token-rebuild, fail-closed) | Accepted |
| [0017](0017-runtime-settings-store.md) | Runtime settings store — DB-backed operator-tunable operational config | Accepted |
| [0018](0018-access-requests-and-approvals.md) | Access requests & approvals — state machine, immutable payload, quorum | Accepted |
| [0019](0019-kviklet-baseline-refresh.md) | kviklet 0.9.2 baseline — workflow comparison, execution and logging safeguards | Accepted |
| [0020](0020-scenario-load-testing.md) | Scenario load testing — k6 capacity gates and measured deployment sizing | Accepted |
| [0021](0021-governed-query-execution.md) | Governed PostgreSQL execution, leases, catalog gate, and results | Accepted |
| [0022](0022-page-first-workflows.md) | Page-first workflows, routed request composition/review/results | Accepted |
| [0023](0023-local-sql-formatting.md) | Local PostgreSQL formatting for editable requests | Accepted |
| [0024](0024-webmcp-next-milestone.md) | WebMCP browser boundary (original M2 sequence) | Accepted |
| [0025](0025-sql-database-first-expansion.md) | SQL database parity first; exclude SQLite from target scope | Accepted |
| [0026](0026-review-tools-before-agent-integration.md) | Early SQL review/EXPLAIN and schema preview; defer WebMCP to M6 | Accepted |
| [0027](0027-masking-before-agent-registration.md) | Sensitive-data masking before agent registration and integration | Accepted |
| [0028](0028-agent-neutral-mcp-gateway.md) | Agent-neutral local/remote MCP Gateway and Kubernetes infrastructure boundary | Accepted |
| [0029](0029-dependency-supply-chain-controls.md) | Dependency installation, provenance and immutable tooling controls | Accepted |
| [0030](0030-database-version-qualification-window.md) | PostgreSQL 16–19 compatibility maintenance and version qualification | Accepted |
| [0031](0031-typescript-seven-native-tooling.md) | TypeScript 7 and native lint enforcement without the classic compiler API | Accepted |
| [0032](0032-request-title-and-body.md) | Access request titles and explanatory bodies | Accepted |
| [0033](0033-type-aware-result-sorting.md) | Type-aware result sorting and explicit query-order restoration | Superseded by ADR-0058 |
| [0034](0034-apache-two-project-license.md) | Apache License 2.0 for Portcullis | Accepted |
| [0035](0035-kubernetes-cnpg-after-mysql.md) | Kubernetes/CNPG immediately after MySQL parity | Accepted |
| [0036](0036-ui-customization-boundaries.md) | UI design values and layout independent of governance | Accepted |
| [0037](0037-page-hierarchy-and-responsive-ux.md) | Page hierarchy and responsive request workflows | Accepted |
| [0038](0038-inline-request-workflow.md) | Inline request workflow from authorized summary facts | Accepted |
| [0039](0039-result-views-and-clipboard.md) | Bounded result views, clipboard export and loading | Superseded by ADR-0059 |
| [0040](0040-recorded-execution-time.md) | Recorded execution time in request and result views | Accepted |
| [0041](0041-default-profile-identicons.md) | Stable local default profile images | Superseded by ADR-0056 |
| [0042](0042-private-network-deployment.md) | Private-network deployment through WARP or Tailscale | Accepted |
| [0043](0043-load-package-imports.md) | Portable package-root imports for TypeScript k6 tests | Accepted |
| [0044](0044-postgresql-string-interpretation.md) | Pin execution string interpretation and reject mismatched server reports | Accepted |
| [0045](0045-testcontainers-package-scheduling.md) | Serialize package processes while retaining scenario concurrency and race detection | Accepted |
| [0046](0046-similar-query-history-suggestions.md) | Authorized similar-query discovery with history navigation and draft reuse | Accepted |
| [0047](0047-tagged-container-publication.md) | Verified tag releases to GHCR | Accepted |
| [0048](0048-commit-based-changelog.md) | Reviewed Conventional Commit changelogs with offline git-cliff | Accepted |
| [0049](0049-multiarchitecture-container-builds.md) | AMD64/ARM64 cross-compilation and native image smoke gates | Accepted |
| [0050](0050-private-vulnerability-reporting.md) | GitHub private vulnerability reporting | Accepted |
| [0051](0051-connection-destination-policy.md) | Operator destination policy for connection dials | Accepted |
| [0052](0052-pre-session-origin-and-setup-boundary.md) | Pre-session Host, origin and setup-token boundary | Accepted |
| [0053](0053-user-and-role-administration.md) | User and role administration with one-time password setup links | Accepted |
| [0054](0054-release-branches-and-documentation-policy.md) | Release branches, tag ownership and documentation policy | Accepted |
| [0055](0055-result-exploration-layout.md) | Result controls, responsive alignment and wrapped Text values | Superseded by ADR-0058 |
| [0056](0056-account-menu-and-csv-dialog.md) | Avatar account menu and bounded CSV export dialog | Superseded by ADR-0061 |
| [0057](0057-keycloak-oidc-in-mvp.md) | Optional Keycloak OIDC sign-in in M3 with local authorization | Accepted |
| [0058](0058-header-only-result-sorting.md) | Optional column-header sorting with SQL result order as default | Accepted |
| [0059](0059-bounded-result-scrolling.md) | Bounded virtual scrolling for result tables | Accepted |
| [0060](0060-request-comments-and-replies.md) | Request comments and replies in M3 | Accepted |
| [0061](0061-csv-export-dialog-destinations.md) | Clipboard and file destinations in the CSV export dialog | Accepted |

## Template
```markdown
# ADR-XXXX: <title>
- **Status:** Proposed
- **Date:** YYYY-MM-DD

## Context
What must be decided and why. Constraints that bound the choice.

## Options
Candidates considered, with trade-offs.

## Decision
What is chosen and why. (Settled items.)

## Consequences
Effects, constraints and trade-offs of the decision; execution task lists stay local.
```
