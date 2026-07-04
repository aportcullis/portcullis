# Architecture Decision Records (ADR)

These records capture the *how* of Portcullis's implementation — the binding technical
choices behind the product requirements. Each ADR states its own context and constraints
so it can be read on its own.

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
