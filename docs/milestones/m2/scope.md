# M2 · Bridge

**Not started — depends on M1 completion.**

First, complete MySQL's connection → policy → request → distinct review → single-use execution → bounded results/CSV → audit journey alongside PostgreSQL. Users should recognize the same workflow across engines while intentional dialect differences remain visible. SQLite is excluded from supported-target scope.
MariaDB and other SQL engines remain separately qualified candidates, not committed MVP targets.

Immediately after MySQL parity, deliver Kubernetes deployment via Helm and Kustomize with external or CloudNativePG-managed metadata PostgreSQL 18. Include CNPG-managed PostgreSQL targets through their primary Service DNS and verified TLS, retaining the existing engine-version matrix.
Separate migration-owner and runtime credentials; preserve mounted keys, backup/restore and result-cache-loss handling. Start with one Portcullis replica. CNPG automatic target discovery remains Horizon. See [deployment sequencing](../../adr/0035-kubernetes-cnpg-after-mysql.md).

After deployment acceptance, add deterministic SQL review information and basic read-query EXPLAIN: show statement class, identifiable referenced objects, policy/limits and bounded native plans with estimates clearly labeled. Planning must preserve authorization, function safety, target checks, cancellation, audit and confidential output.
It does not approve or execute the request; EXPLAIN ANALYZE stays deferred.

After SQL-review acceptance, provide the initial external ChatGPT and Claude connectors. Within this track, qualify minimum server-side protected output first, then verified agent registration, explicit expiring human delegation and client integration. Output is default-deny: release only explicitly allowed protected fields, withhold unqualified content and refuse unsafe disclosure. This bounded M2 gate precedes the broader application-wide masking in M4.

Registration grants no authority. Every call intersects the current human's Binding authority with the verified agent grant and organization, connection, tool and protected-output scopes. Preserve explicit human intent, distinct human review, no self-approval, quorum, immutable payloads and single-use execution; different clients acting for one human count as that same human. Refuse revoked or expired delegation and fence ongoing output and late responses without replaying SQL. Do not forward browser sessions or incoming client tokens to downstream services.

Show the AI/client avatar with the connected human's small avatar overlaid on its lower-right corner; expose both identities through hover, keyboard focus and touch, and attribute audit records to the verified client/registration and human. Branding or a claimed model label does not prove provider/model identity. Display only established connection/work/outcome/revocation states; unavailable identity remains explicit. These are planned M2 interactions, separate from the M1 account menu.

**To complete:** Both adapters pass the shared security/behavior matrix, including MySQL implicit-commit DDL disclosure, exact types, TLS, limits and unknown outcomes. Helm/Kustomize installation, upgrade/restart, Secret/certificate rotation, key preservation, backup/restore and CNPG failover scenarios must pass on a published pinned version matrix; failover must never replay target SQL.
SQL review/planning must pass denied/cross-org access, stale-input, sensitive-output, unsafe-expression and real-engine scenarios. See the [DB feature matrix](../../product/database-support.md).

Initial connector acceptance requires real ChatGPT and Claude client/version/protocol/authentication journeys, protected-output and authority-intersection refusals, revocation/expiry during output, human review and replay refusal, accessible identity states and dual-actor audit. Publish the qualified compatibility matrix before claiming support. Provider/authentication choices require implementation decisions; local stdio, wider clients/providers and browser WebMCP remain M6 expansion.

**Specification:** [Early review and preview](../../product/prd.en.md#411-early-sql-review-and-schema-preview-m2m3), [initial connector sequence](../../adr/0090-early-delegated-ai-connectors.md), [Gateway and deployment](../../product/prd.en.md#413-agent-neutral-mcp-gateway-and-deployment-m2--m6), [priority decision](../../adr/0026-review-tools-before-agent-integration.md), [dialect boundary](../../product/prd.en.md#53-database-dialect-boundary), [safe execution](../../product/prd.en.md#82-sql-execution-safety).
