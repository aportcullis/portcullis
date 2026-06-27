# ADR-0004: Metadata RLS — apply or not

- **Status:** Accepted — no RLS in the MVP. Self-host is single-org, so there is no cross-org boundary to enforce yet; schema stays RLS-ready and RLS is revisited at multi-tenant.
- **Date:** 2026-06-27

## Context
Every core table carries an `organization_id`. The product runs single-org when self-hosted and
leaves multi-tenant only as a trace in the data model. The question: should the metadata
PostgreSQL database enforce org isolation with **Row-Level Security (RLS)**, on top of the
application's repository layer?

Threat model:
- The **application is the only path** to the metadata database; there is no second writer.
- The realistic risk RLS mitigates is an **application bug** — a query that forgets its org
  predicate — not a fully compromised process (which could set any RLS session variable anyway).
- RLS has a real cost: the runtime role is a single pooled connection, so every transaction must
  reliably `SET LOCAL` an org context, and policies must cover every table and access path. With
  connection pooling a missed/leaked session variable is its own correctness hazard.

In a single-org MVP the isolation benefit is low; the benefit grows only when true multi-tenant
SaaS arrives.

## Options
- **(A) No RLS now; enforce in the repository layer** + mandatory cross-org endpoint tests.
- **(B) Enable RLS now** as defense-in-depth.
- **(C) Keep the schema RLS-ready** (org_id everywhere, runtime role distinct from the
  migration/owner role) but do not enable policies until multi-tenant.

## Decision
**(A)+(C): no RLS policies in the MVP, but keep the schema RLS-ready.**

- Org scope is enforced in the **repository layer**: every query is parameterized by the
  caller's organization and there is a single choke point that injects the org predicate, so it
  cannot be forgotten per-call.
- **Cross-org endpoint integration tests are mandatory** and independent of this choice: for each
  endpoint, a request that swaps only an id must not reach another org's data.
- The schema stays RLS-ready: `organization_id` on every core table, and the runtime role is
  already separated from the migration/owner role (the audit-table grant split needs this too).
- **Revisit RLS when multi-tenant SaaS is on the table.** At that point RLS becomes worthwhile
  defense-in-depth and the `SET LOCAL` org-context plumbing is justified.

## Consequences
- MVP avoids the pooled-connection `SET LOCAL` complexity while still being structurally ready to
  add RLS without a schema migration.
- The repository choke point (org-scoped query builder/helper) is a load-bearing component and
  must be the only way core tables are read/written; ad-hoc queries that bypass it are a review
  red flag.
- The cross-org test suite is the active control that this decision relies on — it is not
  optional and runs in CI for every endpoint.
