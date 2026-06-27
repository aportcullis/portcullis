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
