# ADR-0025: SQL database adapters before browser query assistance

- **Status:** Accepted
- **Decision scope:** milestone sequence; support remains gated
- **Date:** 2026-10-03

> **Sequence amendment:** [ADR-0026](0026-review-tools-before-agent-integration.md) advances M2 SQL review/EXPLAIN and M3 schema preview, defers WebMCP to M6 after M5, and removes WebMCP from MVP acceptance. Earlier sequence text below is historical.

## Context

The product owner chooses SQL-based databases first after the container-backed candidate assessment. PostgreSQL has passed M1. ADR-0024 previously placed WebMCP before the remaining DB adapters.
The [candidate assessment](../product/database-candidates.md) records primary-source research and actual MySQL/MariaDB engine experiments; the experiments establish engine behavior, not Portcullis adapter support.

## Options

- Implement WebMCP first: no longer matches the requested database priority.
- Add every Docker-capable engine at once: expands syntax, types and execution semantics before the shared adapter boundary is established.
- Complete MySQL's SQL governance journey first, qualify additional SQL engines separately, then expose the governed workflow through WebMCP.

## Decision

M2 starts with MySQL's complete connection → policy → request → distinct approval → single-use execution → bounded results/CSV → audit journey and shared security contracts. Complete PostgreSQL/MySQL parity before the browser WebMCP track. This amends the ordering and DB scope in ADR-0024; its browser boundary, approval safeguards, lifecycle and native-browser acceptance remain binding.

Keep metadata in PostgreSQL. Keep SQL governance as the first adapter family. MongoDB/document operations remain Later and require their own request/classification model.
MariaDB is the closest additional compatibility candidate because the [chosen native driver's maintainers support it](https://github.com/go-sql-driver/mysql#requirements); qualify its SQL/parser and security matrix independently before advertising support. ClickHouse, SQL Server, TiDB and CockroachDB remain researched SQL candidates pending demand and their own acceptance scope.
Container availability does not promote them into MVP.

Use the existing ADR-0001 native-driver/parser choices for MySQL and Testcontainers for real engine scenarios. Select adapters from the stored connection DB type through consumer-owned ports, wired in `cmd/portcullis`; an unknown or unregistered type must fail closed rather than fall back to PostgreSQL.
Classification, binding, redaction, connection validation and execution must use the same selected engine. Pin the selected DB type in the submitted approval unit and revalidate it before leasing.

Do not enable a selectable MySQL connection merely because its container starts.
Require classification rejection fixtures, named/typed binding, redaction, TLS verification and explicit relaxation audit, exact result types, read-only enforcement, DDL implicit-commit disclosure, bounded row/byte/protocol admission, timeout/cancellation, requester/org isolation, immutable approval inputs, result persistence/CSV and unknown-outcome handling.
Exercise the real UI and Connect boundaries against a Testcontainers-owned target and retain the PostgreSQL regression gate.

Remove SQLite from the supported-target scope, including MVP and the committed roadmap, at the product owner's explicit request. This supersedes SQLite's inclusion in ADR-0001 and the previous PRD; historical decisions and rejection fixtures may retain SQLite references without establishing support. Do not build or advertise a SQLite adapter.
Reintroduction would require a new scope decision and paired PRD amendment.

## Consequences

Amend both PRD translations and the public roadmap/SVG to put database parity before WebMCP. Continue the domain → consumer port → observed failing scenario → implementation → refactor → adapter wiring cycle in small reviewed commits. Support status follows the passing full gate and engine matrix, with no product claim inferred from the exploratory CLI smoke.
