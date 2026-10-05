# ADR-0026: SQL review tools before agent integration

- **Status:** Accepted
- **Date:** 2026-10-03

> **Gateway amendment:** [ADR-0028](0028-agent-neutral-mcp-gateway.md) promotes local/remote standard MCP Gateway to gated M6; browser WebMCP is an optional follow-up. Masking and registration prerequisites remain binding. Earlier remote/headless Later placement is superseded.

## Context

The product owner asks to advance EXPLAIN, SQL analysis and dry-run while delaying MCP. ADR-0025 already prioritizes PostgreSQL/MySQL parity and excludes SQLite. Reviewers need visible evidence before execution; agent integration can follow a mature human workflow.

Primary sources checked on 2026-10-03: [PostgreSQL EXPLAIN](https://www.postgresql.org/docs/18/sql-explain.html), [MySQL 8.4 EXPLAIN](https://dev.mysql.com/doc/refman/8.4/en/explain.html) and [Atlas migration apply](https://www.atlasgo.io/versioned/apply). ANALYZE executes the statement. Atlas migration dry-run previews pending SQL rather than simulating its effects.
Planning is not an unconditional side-effect-free guarantee: preserve parser/function safety and target least privilege.

## Decision

Keep milestone identifiers and the M3 MVP boundary. Amend the ordering in ADR-0024/0025:

- **M2 Bridge:** first finish MySQL governance parity, then add deterministic SQL review information and basic EXPLAIN for supported read statements across PostgreSQL/MySQL.
- **M3 Library:** retain saved-query/reuse workflows and advance the schema preview slice: immutable versioned migration artifact, status, SQL dry-run preview and deterministic impact facts. This slice has no migration apply endpoint.
- **M4 Watch:** retain temporary web access, multistage review and identity expansion. Basic EXPLAIN is no longer deferred here.
- **M5 Forge:** complete schema approval, migration locking, apply/recovery and verification, consuming the artifact/review contracts introduced in M3.
- **M6 Reach:** browser WebMCP becomes a separate integration track, after M5 and the stable query/review APIs. Deployment work remains here. Remote/headless MCP Gateway stays a Later candidate.

Basic EXPLAIN is a separate authorized, audited planning use case, not a request execution or approval. Accept only the supported read allow-list with typed parameters and the existing function/operator/catalog safeguards; construct fixed native plan options server-side, reject ANALYZE and arbitrary user plan options, and never wrap unclassified SQL.
Enforce org/connection access, archived-target checks, connection/config/policy validation, dedicated bounded planning timeout/output and cancellation. Protect plan output like requester-only results: plans may contain sensitive literals.
Tie evidence to SQL/parameter digest, target/config identity, engine version and observation time; invalidate it when inputs change and label costs/rows as estimates. Actual execution still requires its own unchanged approval/lease checks. Add engine-specific rejection and real-engine scenarios before exposure.

Early SQL review shows statement class, identifiable referenced objects and applicable policy/limits, with explicit unknowns; it does not promise affected-row counts, index recommendations or a homemade risk score. Retain fail-closed classification independently of these informational facts.

M3 schema preview reuses ADR-0012's pinned Git commit/files/checksums/Atlas version and per-DB object matrix. The status → dry-run → review slice must be permission-scoped, audited, bounded and tied to one immutable artifact and target. Show catalog fact sources, observation time, estimates and unknowns; revalidation is required before M5 apply.
Dry-run does not run the migration or certify rollback/lock safety. Do not expose apply, create automatic approval, or claim complete schema governance in M3.

EXPLAIN ANALYZE, execute-then-rollback query dry-run and AI review remain separate deferred decisions. They are not prerequisites for MVP. SQLite remains excluded; additional SQL engines still require independent qualification.

## Consequences

Amend both PRD translations, the DB feature matrix and the actual roadmap SVG. M2/M3 now require review/preview acceptance in addition to their existing work; WebMCP is removed from MVP acceptance. Keep ADR-0024's browser security and lifecycle requirements for eventual M6 implementation. This changes scope, not shipped functionality or milestone completion.
