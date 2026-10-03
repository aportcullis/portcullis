# Architecture Decision Records (ADR)

These records capture the *how* of Portcullis's implementation — the binding technical choices behind the product requirements.
Each ADR states its own context and constraints so it can be read on its own.

## Status values
`Proposed` → `Accepted` → (when replaced) `Superseded by ADR-XXXX`

## Pre-implementation gate
These must be `Accepted` before Core 1 coding begins.

| ADR | Topic | Status |
|---|---|---|
| [0001](0001-db-driver-and-parser.md) | Managed-DB driver & SQL parser | Accepted |
| [0002](0002-statement-classification.md) | Statement classification table & test fixtures | Accepted |
| [0003](0003-key-management.md) | Key management (master key, rotation, payload digest) | Accepted |
| [0004](0004-metadata-rls.md) | Metadata RLS — apply or not | Accepted (no RLS) |
| [0005](0005-cellvalue-wire-contract.md) | CellValue / ColumnMeta wire contract | Accepted |

## Feature ADRs
| ADR | Topic | Status |
|---|---|---|
| [0006](0006-authentication-and-sessions.md) | Authentication & sessions (cookies, CSRF) | Accepted |
| [0007](0007-social-login-google-oidc.md) | Social login via Google (OIDC) | Accepted |
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
| [0024](0024-webmcp-next-milestone.md) | WebMCP browser boundary (original M2 sequence) | Accepted; ordering amended by 0025/0026 |
| [0025](0025-sql-database-first-expansion.md) | SQL database parity first; exclude SQLite from target scope | Accepted |
| [0026](0026-review-tools-before-agent-integration.md) | Early SQL review/EXPLAIN and schema preview; defer WebMCP to M6 | Accepted |
| [0027](0027-masking-before-agent-registration.md) | Sensitive-data masking before agent registration and integration | Accepted |
| [0028](0028-agent-neutral-mcp-gateway.md) | Agent-neutral local/remote MCP Gateway and Kubernetes infrastructure boundary | Accepted |
| [0029](0029-dependency-supply-chain-controls.md) | Dependency installation, provenance and immutable tooling controls | Accepted |
| [0030](0030-database-version-qualification-window.md) | PostgreSQL 16–19 compatibility maintenance and version qualification | Accepted |
| [0031](0031-typescript-seven-native-tooling.md) | TypeScript 7 and native lint enforcement without the classic compiler API | Accepted |

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
Follow-up work and any items left to close with a POC.
```
