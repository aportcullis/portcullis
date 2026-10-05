# M2 · Bridge

**Not started — depends on M1 completion.**

First, complete MySQL's connection → policy → request → distinct review → single-use execution → bounded results/CSV → audit journey alongside PostgreSQL. Users should recognize the same workflow across engines while intentional dialect differences remain visible. SQLite is excluded from supported-target scope.
MariaDB and other SQL engines remain separately qualified candidates, not committed MVP targets.

Immediately after MySQL parity, deliver Kubernetes deployment via Helm and Kustomize with external or CloudNativePG-managed metadata PostgreSQL 18. Include CNPG-managed PostgreSQL targets through their primary Service DNS and verified TLS, retaining the existing engine-version matrix.
Separate migration-owner and runtime credentials; preserve mounted keys, backup/restore and result-cache-loss handling. Start with one Portcullis replica. CNPG automatic target discovery remains Horizon. See [deployment sequencing](../../adr/0035-kubernetes-cnpg-after-mysql.md).

After deployment acceptance, add deterministic SQL review information and basic read-query EXPLAIN: show statement class, identifiable referenced objects, policy/limits and bounded native plans with estimates clearly labeled. Planning must preserve authorization, function safety, target checks, cancellation, audit and confidential output.
It does not approve or execute the request; EXPLAIN ANALYZE stays deferred.

**To complete:** Both adapters pass the shared security/behavior matrix, including MySQL implicit-commit DDL disclosure, exact types, TLS, limits and unknown outcomes. Helm/Kustomize installation, upgrade/restart, Secret/certificate rotation, key preservation, backup/restore and CNPG failover scenarios must pass on a published pinned version matrix; failover must never replay target SQL.
SQL review/planning must pass denied/cross-org access, stale-input, sensitive-output, unsafe-expression and real-engine scenarios. See the [DB feature matrix](../../product/database-support.md).

**Specification:** [Early review and preview](../../product/prd.en.md#411-early-sql-review-and-schema-preview-m2m3), [priority decision](../../adr/0026-review-tools-before-agent-integration.md), [dialect boundary](../../product/prd.en.md#53-database-dialect-boundary), [safe execution](../../product/prd.en.md#82-sql-execution-safety).
