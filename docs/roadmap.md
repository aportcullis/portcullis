# Portcullis roadmap

**Govern access. Build trust. Turn queries into reusable analysis.**

Portcullis is evolving from a PostgreSQL governance tool into a self-hosted platform for database access, changes, and analysis. This roadmap explains the outcomes we are working toward, how they depend on one another, and what must be true before a milestone is complete.

![Portcullis roadmap: Foundation baseline; Gate verified; Bridge adds MySQL parity, Kubernetes/CNPG deployment and SQL review; Library adds schema preview and completes MVP; Watch, Forge, and Reach follow; Horizon contains longer-term candidates](roadmap/overview.svg)

**Updated: 2026-10-03. Gate (M1) verified; Bridge (M2) is next.** The complete `make verify` gate passed with Docker-hosted Chromium, including native CSV saving and saved-file readback. See the [validation evidence](operations/m1-validation.md) for the tested environment and separate capacity qualification.

## Three directions

| Direction | The outcome we want | Milestones |
| --- | --- | --- |
| Govern | Every access decision and database change has a policy, a reviewer where required, and durable evidence | Foundation, Gate, Bridge, Watch, Forge |
| Reuse | A useful query becomes a repeatable, shareable analysis asset | Bridge, Library, Horizon |
| Operate | Teams can run and integrate Portcullis in their own infrastructure | Foundation, Bridge, Reach, Horizon |

The names below describe product outcomes. The M0–M7 identifiers retain the milestone boundaries in [PRD §11](product/prd.en.md#11-roadmap); they are not a new delivery schedule. The map is a direction of travel rather than a calendar commitment.

## Foundation · M0

**Baseline established — the platform beneath every later milestone.**

Authentication, organization-scoped permissions, encrypted secrets, append-only audit foundations, server sessions, and an embedded web interface give the governance loop a common base. Foundation work is carried into later release checks; it is not a separate product release.

**Evidence:** [Architecture](ARCHITECTURE.md), [authentication and sessions](adr/0006-authentication-and-sessions.md), [RBAC](adr/0008-rbac-roles-and-permissions.md), and [audit integrity](adr/0009-audit-integrity.md).

## Gate · M1

**Verified — first PostgreSQL alpha boundary.**

Give teams one complete path from connection registration through policy, request, distinct review, single-use execution, result exploration, and audit. Preserve exact result values and explicitly report uncertain outcomes without automatically rerunning SQL.

**Acceptance evidence:** The PostgreSQL vertical slice passed the full `make verify` gate: build, vet, lint, all Go tests, 109 frontend tests and all 11 real browser scenarios, including result exploration and native CSV saving/readback. The quickstart describes the first governed execution. See the [M1 validation record](operations/m1-validation.md) for the Docker-hosted browser environment and the undiagnosed host-native saving failure. Performance/soak qualification remains separate.

**Explore:** [Actual workflow and screenshots](../README.md#request-review-execute-once), [alpha quickstart](operations/pg-alpha-quickstart.md), [execution contracts](adr/0021-governed-query-execution.md).

## Bridge · M2

**Next — Gate verification is complete.**

First, complete MySQL's connection → policy → request → distinct review → single-use execution → bounded results/CSV → audit journey alongside PostgreSQL. Users should recognize the same workflow across engines while intentional dialect differences remain visible. SQLite is excluded from supported-target scope. MariaDB and other SQL engines remain separately qualified candidates, not committed MVP targets.

Immediately after MySQL parity, deliver Kubernetes deployment via Helm and Kustomize with external or CloudNativePG-managed metadata PostgreSQL 18. Include CNPG-managed PostgreSQL targets through their primary Service DNS and verified TLS, retaining the existing engine-version matrix. Separate migration-owner and runtime credentials; preserve mounted keys, backup/restore and result-cache-loss handling. Start with one Portcullis replica. CNPG automatic target discovery remains Horizon. See [deployment sequencing](adr/0035-kubernetes-cnpg-after-mysql.md).

After deployment acceptance, add deterministic SQL review information and basic read-query EXPLAIN: show statement class, identifiable referenced objects, policy/limits and bounded native plans with estimates clearly labeled. Planning must preserve authorization, function safety, target checks, cancellation, audit and confidential output. It does not approve or execute the request; EXPLAIN ANALYZE stays deferred.

**To complete:** Both adapters pass the shared security/behavior matrix, including MySQL implicit-commit DDL disclosure, exact types, TLS, limits and unknown outcomes. Helm/Kustomize installation, upgrade/restart, Secret/certificate rotation, key preservation, backup/restore and CNPG failover scenarios must pass on a published pinned version matrix; failover must never replay target SQL. SQL review/planning must pass denied/cross-org access, stale-input, sensitive-output, unsafe-expression and real-engine scenarios. See the [DB feature matrix](product/database-support.md).

**Specification:** [Early review and preview](product/prd.en.md#411-early-sql-review-and-schema-preview-m2m3), [priority decision](adr/0026-review-tools-before-agent-integration.md), [dialect boundary](product/prd.en.md#53-database-dialect-boundary), [safe execution](product/prd.en.md#82-sql-execution-safety).

## Library · M3

**Planned — completes the MVP after Bridge.**

Turn useful SQL into reusable assets: saved queries, versions, favorites, organization sharing, typed parameters, and execution history. Reusing a query creates a new governed request rather than inheriting an earlier approval.

**Discover while composing:** Inline similar-query history suggestions show authorized titles, authors, timestamps and status, with separate View history and Use this query actions. Preserve composition during history navigation and provide undo for filling SQL; parameter values and previous approval are not inherited. Match within the selected connection/dialect and current permissions, and disclose historical target-config changes. Search requires neither target execution nor external AI. This core reuse flow remains planned; see [ADR-0046](adr/0046-similar-query-history-suggestions.md).

**Also deliver:** Immutable Git migration artifact → status → SQL dry-run preview → deterministic impact facts, with source/observation time, estimates and unknowns. This preview slice exposes no migration apply endpoint; apply follows Forge.

**To complete:** Similar-history discovery must pass visibility/revocation, stale-search, bounded-ranking, keyboard, draft-preservation and fresh-approval scenarios. Saved-query workflows and the result grid must work across both committed databases under the PRD's ownership, sharing, versioning, and approval rules. Schema preview must pass artifact/target pinning, permission, audit, bounded-output and per-DB object acceptance. Core 2 scope remains subject to the product-validation work in [PRD §1.4](product/prd.en.md#14-problem-validation).

**Release boundary:** Gate is the first PostgreSQL alpha. Library, including Bridge, is the MVP.

## Watch · M4

**Planned — access governance beyond a single request.**

First implement versioned sensitive-data disclosure policies and server-side masking/withholding across result APIs, full cells, CSV and SQL/catalog/plan metadata. Existing SQL audit redaction is a separate capability. Validate leak-free outputs, policy changes/cache reuse and fail-closed handling before any agent integration. Then validate temporary web-console access, multistage approval and broader identity integration sequentially. A session must retain per-statement governance and immediate revocation; it must not become a way around the request policy.

**To advance:** Revalidate demand and the console threat model before implementing each capability. The [temporary-access contract](product/prd.en.md#49-temporary-web-sql-console-threat-model-m4-gate-decided-2026-07-04) defines expiry, concurrency, revocation, transaction boundaries, and audit. Native database proxy access remains a Later candidate.

## Forge · M5

**Planned — governed schema changes.**

Complete the M3 preview workflow with distinct approval → apply → verify and recovery. Reuse its pinned Git artifact, status, dry-run and deterministic review contracts. Use Atlas Community through the defined subprocess boundary and review a pinned, immutable migration artifact.

**To advance:** Validate the engine version, licensing, distribution, and database matrix; preserve immutable review inputs, approval checks, execution recovery, and audit. Optional AI explanations must keep uncertain findings uncertain.

**Specification:** [Atlas boundary](product/prd.en.md#54-atlas-boundary), [schema-governance operating contract](adr/0012-schema-governance-ops.md).

## Reach · M6

**Planned — operating and integrating Portcullis after Forge.**

Build on the M2 Kubernetes/CNPG deployment baseline and provide Terraform/OpenTofu integration after the API is stable. Keep metadata, keys, upgrades, and recovery manageable for self-hosted operators.

**Deferred integration track:** After M5 and the M4 masking gate, provide organization-owned agent registration and expiring scoped grants, then a standard MCP Gateway for local clients, Claude Code, Codex and other compatible clients. Use Streamable HTTP plus a local stdio bridge over the same governed application use cases. Publish a real client/version/protocol/auth compatibility matrix before support claims. Browser WebMCP is an optional follow-up, not the initial Gateway prerequisite. MCP remains outside MVP.

**Infrastructure boundary:** Portcullis owns identity validation, masking, authorization, human approval, single-use execution and audit. Operators may supply ingress/TLS, a compatible authorization provider, Secret rotation, network restrictions and observability. Provide Helm/Kustomize deployment examples with discovery routing and proxy limits; do not build an agent runner or assume Kubernetes infrastructure replaces app policy enforcement. See [Gateway direction](adr/0028-agent-neutral-mcp-gateway.md) and [PRD §4.13](product/prd.en.md#413-agent-neutral-mcp-gateway-and-deployment-m6).

**To advance:** Establish API stability and the operational contracts for deployment, backup, upgrades, and accepted result-cache loss before promising provider compatibility. Deployment options do not by themselves imply high availability.

## Horizon · M7

**Exploration — candidates with no delivery commitment.**

Longer-term directions include charts and dashboards, declarative GitOps, CloudNativePG discovery, SIEM integration, ML/AI Review. They remain candidates until demand, goals, prerequisites, and acceptance criteria are validated. Adding an idea here does not make it an available feature.

## How the roadmap evolves

Start new directions as Horizon candidates. Promote them into a milestone only after validating the problem and updating the PRD and required ADRs. Update both PRD translations when scope changes, and update this overview when milestone status changes. Use passing acceptance evidence to mark completion; do not substitute a percentage or a merged implementation for a release gate.

This presentation takes inspiration from the named upgrades and evolving near-/long-term plans in the [Ethereum roadmap](https://ethereum.org/roadmap/). Portcullis scope and sequencing remain defined by its own PRD. Contributor workflow belongs in the [development guide](development.md).
