# Portcullis — Product Requirements Document

> **Language:** English · [한국어](prd.ko.md) · [Documentation](../README.md)
> **Translations:** Update requirements and section numbers in both languages in the same change.
> **Target scope:** PostgreSQL/MySQL targets only; SQLite excluded. MySQL parity and SQL review/preview precede deferred M6 MCP Gateway (ADR-0026/0028).
> **Deployment sequencing:** M2 is MySQL parity → Kubernetes (Helm/Kustomize) and CNPG → SQL review/EXPLAIN.
> **Created:** 2026-06-27.
> **Definition:** A self-hosted open-source DevSecOps tool governing database access and changes, and a BI tool for analyzing, visualizing, and sharing queries and results.
> **Role:** The product contract defining MVP scope, policies, and acceptance criteria; detailed implementation choices belong in ADRs.
> **Competitive baseline:** Updated on 2026-09-30 against kviklet 0.9.2, released 2026-09-29 ([ADR-0019](../adr/0019-kviklet-baseline-refresh.md)).

---

## 1. Background and problem

### 1.1 Problem

Human access to production databases, by developers or DBAs, requires two capabilities together.

- **Governance:** Establish who executed what, when, and under which approval, and prevent dangerous changes before execution.
- **Enablement / BI:** Save and reuse approved queries/results, then analyze, visualize, and share them to support team decisions.

The current product hypothesis is that existing tools favor one side.

| Tool | Strengths | Opportunity to validate |
|---|---|---|
| kviklet 0.9.2 | Access governance, improved review UI and request filters, stored results and dry-run, temporary web sessions and Enterprise DB proxy | Validate versioned, parameterized, shared and favorited query assets → new approval requests, integrated with schema governance, using identical tasks |
| Bytebase | Broad integration of access and schema governance | A narrow, simple product usable as a free single self-hosted instance before licensed self-host HA |
| Atlas (Cloud) | Schema governance and a proven diff engine | No access governance; governance features depend on paid SaaS |
| redash | Query saving, sharing, parameters and result grids | Combine query assets with the governance loop; no pre-execution approval or access governance |

### 1.2 Opportunity

Validate **free OSS self-hosting + integrated governance and enablement + lightweight, clear UX** as the product hypothesis.
Portcullis combines governance, associated with kviklet, and query enablement, associated with redash, on one audit timeline and data model.
Since kviklet already offers free self-hosting and stored results, these alone do not establish differentiation.
Portcullis uses Apache-2.0 (ADR-0034). Separate contributor agreements, trademark policy, and paid-feature boundaries remain owner decisions in §12.2.

### 1.3 Positioning

> kviklet-style access governance, the Atlas schema engine, and redash-style query assets, delivered as self-hosted OSS without external SaaS, with unified auditing and clear UX.

The product has two pillars: **DevSecOps database governance** and **BI analysis/sharing**.
Connect access approval, safe execution, change management, and auditing with query assets, result exploration, charts, and dashboards.
Expand BI gradually from MVP saved queries and result grids.

### 1.4 Problem validation

- Interview at least 5 target teams before implementing the MVP.
  - Record recent production DB access, approval times, existing tools, and workarounds.
- Compare identical workflows in kviklet **0.9.2** and Bytebase.
  - Find request → review SQL → approve/reject → execute → inspect result cells → save and reuse past queries.
  - Compare steps, discoverability, and permission constraints against kviklet's improved sidebar, permission-aware UI, and connection/author/date/type filters.
  - Do not infer UX superiority or missing query-asset features from release notes alone.
- Validate the loss of useful queries using re-execution and copying into Slack/documents within the last 30 days.
- Recheck competitor features and licensing against official documentation when planning releases.
- If interviews do not support demand for query assets, reduce Core 2 and prioritize complete access governance.

---

## 2. Goals and non-goals

### 2.1 Goals

- Govern production queries through request → approval → execution → audit.
- Save, bookmark, share, and parameterize approved queries as team assets.
- Provide DevSecOps access/change controls and BI analysis/visualization/sharing in one product flow.
  - MVP provides query assets and result exploration; later stages add charts and dashboards.
- Govern migration files from Git or other remote storage through status → dry-run → review → approve → apply → verify, using the access core's governance pattern.
- Ship Docker Compose first, then Kubernetes via Helm/Kustomize and CloudNativePG integration immediately after MySQL parity in M2; Terraform/OpenTofu follows API stability in M6.
- Deliver Core 1/2 in one Portcullis binary.
  - Images including Schema Governance also contain a pinned Atlas Community CLI.
- Prevent unapproved SQL or SQL changed after approval from reaching the target DB through any execution path.

### 2.2 Explicit non-goals

- **Initial BI scope:** Scheduling/BI alert engines, dozens of visualization types, and embedded/public dashboards are outside initial releases and may be considered in the later roadmap.
- **ETL/CDC:** No real-time streaming or automatic synchronization between connections.
- **Own schema diff engine:** Use the proven Atlas OSS engine.
- **Enterprise IAM or identity store:** MVP provides local email/password accounts, Google OIDC and optional Keycloak OIDC in M3. Portcullis retains application permissions; broader federation, LDAP, SAML, SCIM and group-role sync remain later integrations.
- **Multitenant SaaS in MVP:** Retain model hooks while operating as single-org self-hosting.

### 2.3 MVP release boundary

#### Included

- **Target databases:** PostgreSQL and MySQL; metadata always uses PostgreSQL.
- **Deployment:** Docker Compose quickstart; M2 adds single-instance Kubernetes deployment through Helm/Kustomize and external or CloudNativePG-managed metadata PostgreSQL.
- **Authentication:** Local email/password with argon2id, Google OIDC, optional Keycloak OIDC in M3, server-side sessions, and initial admin bootstrap.
- **Requests:** Approval of one SQL statement against one connection with exact parameter values.
- **Policy:** Per-connection `read`/`write`/`ddl` `required_approvals`, default 1 and 0 for automatic approval, no self-approval, default 24-hour approval validity, and one execution per approval.
- **Execution:** Connection-specific allowed statement classes, default read-only, with admin explicitly enabling write/DDL.
- **Query assets:** Private or organization-shared saved queries, versions, favorites, parameters, and saving from execution history.
- **Results:** Temporary snapshots capped at 10,000 rows and a byte limit, table navigation, and CSV export.
- **Audit:** Structured append-only events for authentication, requests, approvals, execution, and administration.

#### Excluded

- Additional target databases, temporary access sessions, and DB proxy.
- Team/role approval rules, ordered multistage approvals, and break-glass.
  - Quorum approval by N people with the same role is included.
- OIDC providers other than Google and the M3 Keycloak integration, LDAP, SAML, SCIM, and IdP group-role sync.
- Application HA and Terraform/OpenTofu providers. CNPG automatic target discovery remains Later.
- Schema Change Governance, reserved for the **Schema milestone** after the first MVP.

### 2.4 Success criteria

#### Release acceptance

- Execution APIs always reject unapproved, expired, rejected, or changed-payload requests.
- A request with `required_approvals=N` requires N distinct active approvers other than the requester.
  - One rejection produces terminal `rejected`; `N=0` records a system automatic-approval event.
- Pin the canonical JSON of `payload_version + organization_id + requester_id + connection_id + connection_config_version + connection_policy_version + statement_class + normalized SQL + typed parameter values` using **`payload_digest`, keyed HMAC-SHA-256**, and verify immediately before execution.
  - Derive a dedicated payload-integrity key from the master key through HKDF and store its version with the digest.
  - A plain hash exposes low-entropy literals to brute force in long-lived audit records.
  - Normalize only UTF-8 and line endings; do not rewrite whitespace or semantics.
- A connection policy change or archive prevents execution of previously created, unexecuted requests.
- Acquire an execution lease atomically only once per approved request, with unique `request_id` and lease owner/deadline/heartbeat.
  - Record lease acquisition and `EXECUTION_STARTED` in the same metadata transaction.
  - If the server stops before confirming the outcome, do not retry automatically; reconciliation transitions expired `executing` to `outcome_unknown`.
- Audit every transition and execution attempt with actor, time, target, previous/next states, and payload digest.
- Enforce default 30-second query timeout, maximum 5 minutes, maximum 10,000 rows, and a result byte cap on both databases.
- Bound each lock wait of an execution separately (default 5 seconds, operator range 1–60 seconds), so a queued exclusive lock cannot stall other users of the target object for the whole query timeout; a lock-wait refusal is a confirmed failure (ADR-0021).
- Complete bootstrap → connection registration → request → approval → execution within 15 minutes in a fresh Docker Compose environment.
- Target p95 ≤500ms for major APIs excluding query execution time at 50 concurrent users.
- **Performance and sizing, ADR-0020:** Measure browsing, requests, and approvals with k6 first; add execution, result navigation, CSV, and cancellation after the executor exists.
  - Report active users, think time, journeys/s, dataset size, and shared-IP constraints together.
  - Treat combined app/metadata DB sizes of 2 vCPU/4 GiB, 4 vCPU/8 GiB, and 8 vCPU/16 GiB as experimental candidates.
  - Publish recommended sizes and team-size conversion assumptions after repeatable load/soak passes and resource headroom checks.
  - API-only measurements do not establish whole-product user capacity.
- Target median `pending→approved|rejected` decision time ≤30 minutes for requests requiring manual approval.

#### Beta product hypotheses

- At least 3 design-partner teams install in their own environments and use the product weekly.
- At least 30% of completed queries are saved or started from an existing saved query.
- At least 20% of shared saved queries are reused by another user within 30 days; approval policy affects reuse friction (§4.3).
- Median installation-to-first-approved-execution time is ≤30 minutes.

---

## 3. Target users and personas

- **Developer/requester:** Needs production queries, submits requests and waits for approval, and wants to save/reuse common queries.
- **Approver/DBA:** Reviews risk and approves/rejects requests.
  - For schema changes, uses dry-run SQL and deterministic facts such as target objects, table size, and known lock possibilities.
- **Admin:** Registers connections, manages users/roles, and inspects audit records.

Initially target small to medium engineering teams that prefer self-hosting and want query versions, sharing/reuse, and schema changes governed in the same audit flow.
Do not define this audience by assuming a competitor's UX is poor.

---

## 4. Scope by phase

### 4.1 Core 1: Access Governance (MVP)

The core governance loop for production queries.

- **Connections:** Register PostgreSQL/MySQL configurations, test before saving, and encrypt credentials at the application layer.
- **Access requests:** Submit one statement with exact execution parameters; submitted payloads are immutable.
- **Approval:** Distinct active approvers in the same organization review and approve up to the policy quorum, or reject; no self-approval.
- **Execution:** Execute the approved payload once, enforcing server-side timeout and row/byte caps with no automatic retry.
- **Audit:** Record who did what, when, and with which outcome as structured events.

### 4.2 Core 2: Query Enablement (MVP) — differentiation

Turn approved, executed queries into assets.

- **Saved queries:** Name, description, tags, and parameter definitions; edits create new versions instead of overwriting.
  - Limits: nonempty name ≤200 code points, description ≤2,000, ≤10 tags of ≤50 code points each, ≤32 parameters with names ≤64.
  - SQL follows the 64 KiB request-size limit, ADR-0010.
- **Sharing/favorites:** `private`/`organization_shared` visibility, permission-based sharing, and per-user favorites.
- **Save from history:** Convert an execution record into a saved query.
- **Similar-query suggestions (ADR-0046):** Discover reusable history inline while composing SQL; planned Core 2/M3 scope, not a shipped feature.
  - Initially show five ranked authorized requests on the selected connection/dialect with bounded expansion: title, author, status, submitted time and recorded execution time.
  - Separate **View history** and **Use this query**. Preserve composition during navigation; fill authorized SQL and parameter definitions with undo, keeping title/body/connection and requiring fresh parameter values and approval.
  - Compare dialect-aware structure while normalizing formatting/comments and literal differences; same objects/class and recency support ranking, not semantic equivalence.
  - Exclude other users' private drafts and inaccessible sources; recheck source permission at reuse. Label historical target-config changes and validate a reused draft against the current target/policy. Shared saved versions join under Library visibility rules when implemented.
  - Keep SQL encrypted, use keyed scoped index signatures and fence stale responses. Search never executes target SQL or calls external AI.
- **Parameters:** Named values such as `:start_date` become native bind parameters, never string substitution.
  - MVP types: string, integer, decimal, boolean, date, timestamp, UUID, null.
  - Table/column identifiers cannot be parameters.
- **No inherited approval:** Saved queries and versions have no approval state; each execution creates a new request with a connection and exact parameter values.
- **Result grid:** Sort, filter, paginate, and export CSV from the same snapshot without rerunning the query.

### 4.3 Core access policies

- **Approval unit:** An immutable payload combining payload version, normalized SQL, typed parameters, connection, **connection config version**, requester, statement class, and connection policy version.
  - Any change requires a new request.
  - Per [ADR-0014](../adr/0014-connection-registration-and-tls.md) and [ADR-0018](../adr/0018-access-requests-and-approvals.md), approval binds to connection configuration as well as its identifier.
  - Pinning only the ID would let digest verification pass against a different database than the approvers reviewed, contrary to OWASP transaction authorization.
  - Config replacement atomically expires unexecuted pending/approved requests as `expired(connection_changed)`; drafts survive and can be resubmitted against the new config.
  - Renaming preserves approval because it changes the descriptor, not the target; use separate `config_version` rather than descriptor `version`.
- **RBAC, ADR-0008:** SQL-seeded permissions form a Google-IAM-style `resource.verb` catalog; roles are DB-stored permission bundles.
  - Authorize by permission, never role name.
  - Startup refuses to serve when a permission key enforced in code is missing from the loaded catalog.
  - Seed roles are defaults, not a closed set; admins can create custom roles.
  - `requester` manages own requests/assets, `approver` additionally reviews others' requests, and `admin` additionally manages users/connections/policies/audit with all permissions.
  - Admins cannot approve their own requests.
- **Execution permission:** Only the requester can execute their approved request; no admin proxy execution in MVP.
  - kviklet 0.8.0 permits other execute-permission holders to run single-execution requests; Portcullis keeps its requester-bound approval/result contract, ADR-0019.
- **Single-person deadlock:** Keep no-self-approval even for admins; resolve solo self-hosting through per-connection/class `required_approvals=0`.
  - Submit transitions directly to `approved` with a system automatic-approval audit event.
  - Do not add an allow-self-approval toggle.
- **Validity:** Default 24 hours from the Nth approval or system approval, configurable by organization from 15 minutes to 7 days.
  - Both paths set `expires_at` inside the transaction after acquiring the row lock.
  - Computing automatic approval time before locking could consume validity while waiting for policy/archive locks before the request is stored.
- **Statement policy:** Per-dialect parsers establish one statement and its class; uncertain classification is rejected, then connection `read`/`write`/`ddl` permissions apply. PostgreSQL DDL query bodies must pass the full read vocabulary and reject locking/unknown expressions.
  M1 rejects one statement combining DDL with nested DML instead of waiving an independently configured write policy (ADR-0002).
  PostgreSQL RENAME/DROP/COMMENT act only on the object kinds CREATE admits (table, view, materialized view, index, sequence, schema), and ALTER TABLE admits only an explicit subcommand list; roles, databases, routines, triggers, policies, extensions, ownership and trigger/rule/row-security toggles are refused (ADR-0002).
- **Read-only defaults:** New connections use `read=true`, `write=false`, `ddl=false`; admin write/DDL enablement is audited.
- **Reuse friction:** Every saved-query execution creates a new request because approval is not inherited (§4.2).
  - Control friction through per-connection/class approval policy, extending the verified kviklet `numTotalRequired` model.
  - `connection_policy_versions` contains separate `required_approvals` for read/write/ddl, 0–N, default 1.
  - Low-risk read connections can use `read.required_approvals=0` with unchanged audit guarantees; writes/DDL can require higher quorums.
- **Policy snapshot:** Before submission, classify the statement and pin current policy version, applicable quorum, and limits.
  - Policy changes increment the version and expire unexecuted pending/approved requests as `expired(reason=policy_changed)`.
  - Never retroactively combine a new policy with an old approval.
- **Execution consistency:** No distributed transaction between metadata and target DB; no automatic retry after leasing, and unconfirmed outcomes become `outcome_unknown`.
- **Saved-query permissions:** Only owner/admin can edit; organization members may view/fork shared queries but cannot overwrite the original.
- **Result permissions:** Only the execution requester may access snapshots/CSV; approvers/admins may inspect audit metadata but not result rows.
- **Archive and history:** Connection deletion means archive and preserves requests, approvals, executions, and audit.
  - Reject archive while any request executes; successful archive immediately blocks requests, tests, and execution.
  - Cancel drafts and expire pending/approved requests with `connection_archived`, close pools, and discard encrypted credentials.
  - Restoration requires credential re-entry and a connection test.
  - History snapshots contain connection ID, display name, DB type, and target fingerprint, excluding credentials.
  - Only separate retention jobs delete history; runtime cannot update/delete audit events (§8.4).

#### MVP database compatibility contract

Current implementation status is tracked separately in the [DB feature support matrix](database-support.md); required does not mean shipped.

| Feature | PostgreSQL | MySQL |
|---|---|---|
| Connection test and TLS validation | Required | Required |
| Single-statement parsing/classification | Required | Required |
| Typed bind parameters | Required | Required |
| Read/write/DDL policy | Required | Required |
| Timeout and cancellation attempts | Required | Required |
| Row/byte caps, snapshots and CSV | Required | Required |
| Request → approval → execution → audit | Required | Required |

- Do not label a DB supported in the UI until it passes these common acceptance tests.
- ADR-0030 sets PostgreSQL compatibility maintenance to 16/17/18/19: preserve existing Portcullis behavior and fix regressions rather than expand engine-specific features or syntax. Keep 16 when adding 19. Version 19 remains preview until GA and final qualification.
  PostgreSQL explicitly uses four families; MySQL initially has at most three candidates (8.4 LTS/9.7 LTS/26.7 Innovation), excluding 8.0. Publish verified versus pending status, actual patches/digests, failures and skips. Metadata PostgreSQL 18 and capacity qualification remain separate; expansion or retirement requires an explicit scope decision.
- Reject transaction control, session mutation, native file/network I/O, multiple statements, and unclassified statements in MVP.
- Permit implicit-commit statements such as MySQL DDL only under allowed DDL policy and warn approvers that rollback may be impossible.

### 4.4 Access Request state machine

```text
draft ──submit(required=0, system approval)──────────────────────────> approved
  │
  └──submit(required>0)──> pending ──approve(count < required)──────> pending
                              ├──────Nth distinct approval──────────> approved
                              └──────reject─────────────────────────> rejected

draft/pending/approved ──requester cancel───────────────────────────> cancelled
draft ──connection archive──────────────────────────────────────────> cancelled
pending/approved ──policy change | connection archive──────────────> expired
approved ──approval invalid─────────────────────────────────────────> expired
approved ──acquire execution lease──> executing ──> succeeded|failed|cancelled|outcome_unknown
```

- Only drafts are editable; the connection is fixed at creation, and submission freezes SQL, typed parameters, title and body.
- New UI requests require a single-line title ≤200 Unicode code points and allow a plain-text body ≤4,000; legacy/API requests may use an untitled fallback. Narrative shares the 56 KiB payload budget inside the 64 KiB transport limit, is encrypted and is excluded from audit metadata.
- Approvers decide only pending requests, once per approver and without self-approval. Any rejection is terminal; zero required approvals records system approval.
- Revalidate approver activity and permissions at decision time and immediately before execution. Invalid pending approvals do not count; an approved request below quorum expires as `approval_invalidated`.
- Only the requester can cancel an unexecuted request or execute an approved request once. Every admitted attempt records start and outcome, with no automatic retry.
- Recover expired execution ownership as audited `outcome_unknown` at startup and every 30 seconds. A late completion cannot overwrite a recovered outcome.
- Confirmed cancellation/rollback records cancelled/failed; an interruption with an unconfirmed outcome records `outcome_unknown`.
- Reviewers with either approve or reject permission can inspect organization requests and payloads; other requesters see only their own requests, and result rows belong only to the original requester.
- Effective list/count state treats approvals at or past expiry as expired. Navigation and active target selection follow current permissions.
- Refused submission leaves an editable draft, terminal states stay terminal, and manual unknown-outcome resolution adds evidence without rewriting the original outcome.

Per [ADR-0018](../adr/0018-access-requests-and-approvals.md), state enforcement, payload integrity, policy pins and decision timestamps are request contracts.
Per [ADR-0021](../adr/0021-governed-query-execution.md), leases, heartbeats, recovery, cancellation and completion fencing are execution contracts.
Per [ADR-0032](../adr/0032-request-title-and-body.md), narrative is part of the immutable approval payload.

### 4.5 Schema Change Governance (Schema milestone)

**Delivery split (ADR-0026):** M3 provides immutable-artifact status/dry-run/impact preview only; M5 completes approval/apply/recovery/verify. M3 must not expose migration apply.

Govern migration files from Git or other remote storage through the access core's request → review → approve → execution → audit pattern.
Use Atlas Community for status/dry-run/apply; differentiation comes from Git integration, governance approval, impact review, unified auditing, and Argo-style UX, without building lint/pre-check risk engines.

```text
[status] → [dry-run] → [review] → [approve] → [apply] → [verify]
  Atlas      Atlas    Own (+optional AI)  Own gate  Atlas   Atlas (revision)
```

- **Source:** Per-connection remote repo/branch/path; branch is an initial selector only.
  - Support versioned migrations first, declarative workflows later.
- **Immutable artifact:** At request creation, resolve branch to commit SHA and store remote ID, SHA, path, ordered files/checksums, `atlas.sum`, Atlas version/options as an immutable artifact.
  - All subsequent status/dry-run/review/approve/apply use its ID, never reread the branch, preventing branch-change TOCTOU.
- **Safe apply:** Acquire a connection migration lock, revalidate status, and apply only the artifact's file count.
  - Atlas `migrate apply` requires directory/revision-history consistency.
  - Recover locks after server failure through owner/deadline/heartbeat or PostgreSQL advisory-lock rules.
  - Use separate `schema_apply_timeout` for longer operations.
- **Status:** Show applied/pending migrations using `migrate status`.
- **Dry-run:** Preview pending SQL using `--dry-run`; describe it as SQL preview, not simulation of effects, and feed it into review.
- **Review:** Always provide deterministic facts: statement classes, target objects, current table size/estimated rows, and known DB/version-specific lock/rewrite possibilities.
  - Fix each fact's source in dialect-native catalog queries, e.g. PG `pg_table_size` and `pg_class.reltuples`, with static engine/version lock/rewrite lookups.
  - Explicitly mark undefined or uncomputable facts, particularly actual DDL affected rows/rewrite, as `unknown`.
  - Identical input and DB state produce identical summaries; never promise estimates as facts.
  - Optional AI explains/contextualizes facts only; facts remain available with AI off, and there is no own lint rule set.
- **Approve:** Reuse roles, `required_approvals`, no-self-approval, and payload pinning; approval covers migration files, checksums, and target connection.
- **Effective class:** Classify every statement and select highest `ddl > write > read`, using that quorum.
  - Any unknown statement rejects request creation.
  - Include effective class and policy version in the approval payload.
- **Apply:** Run `migrate apply`, auditing attempt/outcome without retry and with the same unknown-outcome rules.
- **Verify:** Re-run status to check only that the revision table reached the target migration version.
  - Drift and data postconditions are excluded; add separate postcondition checks later if needed.
- **Audit:** Put every step transition on the same timeline as access and query assets.
- **Compatibility:** Pin Atlas Community object support for each of PostgreSQL/MySQL in a matrix.
  - Enable each DB only after independent contract tests; explicitly reject unsupported objects during status/dry-run.

### 4.6 Access Governance extensions (Kviklet parity track)

Compare against **kviklet 0.9.2**, distinguishing editions and implementation stages, ADR-0019.
Validate UI/UX and query-asset/schema integration using identical tasks; retain MVP/post-MVP/Later boundaries rather than making every new competitor feature mandatory for alpha.

The baseline includes stored results/transactional dry-run since 0.7, review sidebar/permission-aware UI/request filters since 0.8, and more filters/full-cell viewing/user disabling/credential AEAD since 0.9.
PostgreSQL/MySQL/MariaDB proxy is **Enterprise-only beta since 0.9**, distinct from temporary web access; role review gates and role sync are also Enterprise.
Telemetry defaults on in 0.9 but can be disabled and sends instance URL/version among other information; this does not authorize adding Portcullis telemetry.
Use the 0.9.2 fixes for approved-command replacement and result-log exposure as scenarios for immutable payloads, pre-execution verification, and no result logging.

| Feature | Phase | Portcullis policy |
|---|---|---|
| Single-query request/approve/reject | MVP | Common support for both databases |
| Connection RBAC/read/write/DDL policy | MVP | Default read-only, no self-approval |
| Unified audit | MVP | Same timeline as query assets and schema changes |
| Comments/review suggestions | Post-MVP | Append-only discussion separate from state transitions |
| Temporary SQL access | Post-MVP | Web console session reusing server execution, dialect-independent policy and result grid; audit each statement, matching kviklet `Connection.kt` per-execute `saveEvent`, code checked 2026-06-27; threat model §4.9 |
| Multistage/role review gates | Post-MVP | Start with explicit quorum/role rules before a policy DSL |
| EXPLAIN | M2 (basic read plans) | Distinguish read safety from `ANALYZE` execution per DB |
| Google OIDC | MVP | Server callbacks, no frontend SDK; currently authoritative verified-email linking to admin-created users, no signup, ADR-0007 |
| Keycloak OIDC | M3 | Optional company SSO; explicitly linked existing users, local roles and server sessions, ADR-0057 |
| Other OIDC/LDAP and group-role sync | Post-MVP | External IdP authentication; application authorization remains governed locally |
| Native DB client proxy | Later/deferred | Web console replaces temporary access; reconsider only after strong native-client demand and complete wire policy/audit/credential design; kviklet 0.9 PG/MySQL/MariaDB is Enterprise beta, with separate dialect implementation/validation cost |
| API keys | Later/after validation | Separate scope, expiry, and rotation from UI sessions |

Kubernetes exec, MongoDB/MSSQL, SAML/SCIM are not mandatory for MVP/parity; prioritize from actual demand.

### 4.7 Later

- Additional target databases.
- Team-level sharing/approval policies.
- **BI analysis/sharing:** Connect saved queries and execution results to charts and dashboards.
  - Start with basic bar/line/pie visualizations and expand to shared team analyses.
  - Define result access, retention, and refresh rules when starting the BI milestone.
- Declarative GitOps; versioned Git migrations already belong to §4.5.
- CNPG discovery as a Helm-only option.
- SIEM audit webhooks/JSON export.
- ML anomaly detection from audit risk scores, linked to §4.8.
- SAML/SCIM after demand validation.
- **Agent Gateway integration:** Gradually add a path for agents to access Portcullis capabilities.
  - ADR-0028 promotes the standard local/remote MCP Gateway to gated M6 (§4.13); browser WebMCP (§4.10) is an optional follow-up, not the initial gateway prerequisite.
  - Define agent identity, user delegation, least privilege, approval boundaries, and audit attribution when starting the milestone.
  - Choose the gateway product, protocol, authentication, and implementation sequence through ADRs after demand validation.

### 4.8 AI Review (optional value layer, post-MVP)

Human review time and expertise drive governance cost and approval turnaround (§2.4).
An optional advisory reviewer can reduce it without locking core features; hosted AI is a possible paid offering, with monetization awaiting a separate decision.

- **Placement:** Access-request SQL summaries, risk signals such as full scans/PII/missing WHERE/write scope, and approval rationale.
  - In migration review, explain deterministic facts and suggest online DDL/backfill alternatives, respecting unknown affected-row/rewrite facts.
  - Populate audit `risk_score` (§6.1).
- **Mandatory guardrails:**
  - Advisory only: never replace quorum or automatically approve; audit AI use and output.
  - Explicit opt-in: default off, operator chooses provider (§8.5).
  - Never send rows, plain parameters, credentials, or connection hosts to hosted providers; remove SQL comments and replace literals using parser ASTs.
  - Sending schema/table/column identifiers requires separate opt-in.
  - Treat SQL comments/migration text as untrusted, render escaped output, and never automatically apply generated SQL/policy.
  - Timeout/errors/quota exhaustion never block approval transitions; append `review_unavailable`.
  - Isolate providers behind `AIReviewer`, like SchemaEngine/ResultStore, avoiding vendor lock-in; free self-hosted BYO-key and paid hosting can coexist.


Inputs contain redacted SQL, statement class, fact summary (§4.5), and policy context.

### 4.9 Temporary web SQL console threat model (M4)

Define session privileges, expiry, concurrency, and revocation for the §12.1 web console reusing server execution.
This section replaces the separate PRD previously required to start M4.

- **Grant:** Use the same request/approval loop; payload contains connection, allowed read/write/ddl subset, session TTL, and policy version.
  - Reuse no-self-approval, quorum, and approval revalidation (§4.4); session classes must be a subset of connection-allowed classes.
- **Per-statement gate:** Every statement passes parser classification, ADR-0002, and session/connection policy checks.
  - Reject unknown, disallowed, and always-denied statements; an open session never bypasses checks.
- **Expiry:** Default TTL 60 minutes, org range 15 minutes–8 hours, idle timeout 10 minutes from last statement completion.
  - Values are provisional for M4 revalidation.
  - On expiry, attempt cancellation; an unconfirmed outcome is unknown (§4.4).
- **Concurrency:** One sequential statement per session, preventing policy races; at most 2 active sessions per user, provisional.
- **Immediate revocation:** Disabled user, invalidated granting approval, policy change, archive, or explicit admin revoke closes the session.
  - Handle running statements as on expiry; revoking the UI session also revokes console sessions.
- **Audit:** `SESSION_OPENED`/`SESSION_CLOSED`/`SESSION_REVOKED` plus per-statement STARTED/FINISHED/unknown and redacted SQL/digest (§6.1).
  - Never allow execution without history.
- **Transactions:** Default stateless per statement to avoid idle-in-transaction.
  - Stateful multi-statement transactions are a later connection-policy opt-in restricted to read-only, with automatic ROLLBACK after 60 seconds transaction idle, provisional.

### 4.10 WebMCP query assistance (M6 / Reach)

Browser WebMCP is deferred to M6 after M5 and stable query/review APIs (ADR-0026, amending ADR-0024/0025). It is outside MVP acceptance. PostgreSQL/MySQL parity and human SQL review/preview take priority. SQLite remains excluded. ADR-0028 promotes local/remote MCP Gateway integration to M6; browser WebMCP is an optional subsequent adapter.
WebMCP activation additionally requires M4 masking acceptance and M6 authenticated agent registration/grants (ADR-0027); registration precedes integration.

- Expose connection discovery, visible SQL/typed-parameter composition, explicit draft save/submit, request/approval-state inspection, requester-only approved execution and bounded result-page retrieval as separate tools. Start with read queries; filling a form must not automatically persist or execute it.
- Add schema discovery only through a bounded, authorized and audited catalog use case. Discover/reuse saved queries when Library supplies those assets. Neither path grants arbitrary SQL execution.
- Reuse the current authenticated user/org and existing server permissions, CSRF, ownership, distinct reviewers, quorum, immutable payload/config/policy, single-use execution, cancellation, audit and result limits. No initial automatic approval/rejection tool. Require an explicit user execution request, and distinguish authenticated user attribution from untrusted agent/source labels.
- Keep ordinary work within pages. Provide a visible tool outcome and direct links; revoke registration and fence pending responses on logout, identity/permission changes and route teardown. Bound outputs and treat catalog/SQL/result content as untrusted data. Never export credentials, encryption keys or UI cookies.
- Feature-detect native browser support; keep the normal interface usable without WebMCP. Reverify the evolving API at implementation time. Acceptance requires a real supported-browser query journey, denied/revoked and cross-user cases, replay refusal, cancellation, exact/bounded results and unsupported-browser fallback; a fake registry alone does not prove compatibility.

### 4.11 Early SQL review and schema preview (M2–M3)

ADR-0026 advances review tools before agent integration. In M2, after MySQL parity and the Kubernetes/CNPG deployment gate (ADR-0035), show deterministic statement class, identifiable referenced objects and applicable policy/limits, with explicit unknowns.
Add basic native EXPLAIN only for supported read statements, using typed parameters and fixed server-controlled options; reject ANALYZE, unsafe functions/operators and unclassified forms. Require org/connection authorization, archived-target checks, target/config/policy validation, bounded planning timeout/output, cancellation, requester-only plan access and audit.
Planning does not approve or execute a request. Tie evidence to SQL/parameter digest, target/config, engine version and observation time; invalidate changed inputs and label costs/rows as estimates. Accept only after parser rejection, side-effect defenses, denied/cross-org access, stale inputs, sensitive plan output and real-engine scenarios pass for both DBs.

M3 adds the §4.5/ADR-0012 schema status → dry-run → deterministic review slice over a pinned immutable Git/Atlas artifact. Enforce the per-DB object matrix, permissions, audit and bounded subprocess/catalog operations. Display fact sources, observation time, estimates and unknowns. No migration apply endpoint is exposed until M5; preview does not simulate changes or guarantee rollback/lock safety.
Actual apply must revalidate artifact and target state.

EXPLAIN ANALYZE, execute-then-rollback query dry-run, AI review and index recommendations remain deferred. Basic review facts do not promise exact affected rows or a risk score.

### 4.12 Sensitive-data masking and registered agents (M4 → M6)

Deliver server-side masking/withholding in M4 before registering or connecting agents in M6 (ADR-0027). Existing SQL audit redaction is not result-data masking. Admins manage versioned organization/connection disclosure rules; apply them before serialization across APIs, cells, CSV, SQL/catalog/plan/review metadata and future tools. Start with full redaction/omission.
Requester ownership does not bypass masking; any human raw-view exception requires separate explicit permission and audit. Agents have no raw-view exception initially. Preserve approved SQL/parameters, execution semantics, encrypted originals and existing retention.

Agent output is default-deny and releases only approved protected fields. Never send secrets, credentials, raw SQL/parameters or raw sensitive cells. Withhold unknown lineage/aliases/expressions, unqualified free-text/JSON or unsupported encodings; masking errors refuse safely. Recheck current policy on every read/export/response and invalidate stale exports/transformed caches after rule changes.
Restrict sorting/filtering/search that could disclose hidden values. Audit policy versions/decisions without sensitive values. Automatic detection assists policy setup, not authorization. Acceptance includes UI/API/CSV/full-cell parity, metadata/plan/error leaks, derived values, stale caches, failures, org isolation and canary-secret tests against both DBs.

After masking passes, org admins register agents with stable ID, owner, integration kind, pending/disabled/active/revoked state, allowed tools/connections, protected-output policy and expiring user delegation. Registration starts without grants; activation is explicit. Audit changes and use. Effective access intersects authenticated user, verified agent grant, org/connection and masking policies.
Claimed names/IDs are untrusted. Revocation/expiry/logout prevents later calls and fences pending responses. Retain explicit user execution intent, distinct review, quorum, payload integrity and single-use execution; no automatic approval/rejection tools.

Before integration, a transport ADR must prove caller binding to an active registration/delegation. Native WebMCP alone is not agent authentication; refuse protected capabilities when identity cannot be verified. Registration neither installs executable plugins nor fetches arbitrary endpoints. M6 MCP Gateway authorization follows §4.13/ADR-0028 without browser-token passthrough.
Acceptance includes forged IDs, cross-org access, revoked/expired grants, permission intersection and protected output. This governs Portcullis-mediated disclosure; it cannot constrain an external agent's independent browser/DOM access.

### 4.13 Agent-neutral MCP Gateway and deployment (M6)

After M4 masking and M6 registration/grants pass, provide an opt-in standard MCP Gateway over Streamable HTTP and a local stdio bridge to the same authenticated endpoint (ADR-0028). Target local generic clients, Claude Code and Codex without vendor SDK dependence. A client is supported only after its recorded version, negotiated protocol, authorization and real governed tool journey pass.
Browser WebMCP is an optional follow-up, not the initial Gateway gate. Preserve one Go binary with optional adapter/subcommand and existing application ports; do not run or host agents/models, proxy arbitrary MCP servers, or let the bridge connect directly to target DBs.

Gateway validates authenticated issuer/audience/expiry/scope, active registration and expiring delegation per request; scopes intersect user/org/connection/tool/masking permissions. Standard HTTP authorization/resource discovery uses a tested compatible provider, which may be external. Registration is not OAuth client registration and never grants implicit access.
No unauthenticated local mode, cookie/token passthrough or trusted agent-name headers. Stdio stdout is protocol-only with sanitized stderr; credentials are scoped and excluded from examples/logs. Local HTTP binds loopback by default; deployed access requires TLS/origin controls.
Retain explicit user intent, distinct approval, immutable payload, one-time lease, bounded protected output, cancellation and audit.

Infrastructure owns TLS/ingress, provider hosting, Secret delivery/rotation, network policy and observability deployment; Portcullis retains identity and business-policy enforcement. Provide Helm/Kustomize examples for endpoint and auth-discovery routing, Secret references, probes, resource/body/stream limits and network restrictions.
NetworkPolicy needs an enforcing plugin; Secrets need protected storage and access. Validate Kubernetes routing/discovery, rotation, proxy buffering/timeouts, restart/shutdown and no bypass. Multi-replica claims require shared registration/revocation state and one-time execution tests, not pod-local grants.

Acceptance includes real local/Claude Code/Codex client matrix, protocol compatibility, auth discovery, denied/spoofed/cross-org/expired/revoked access, masking canaries, disconnect/cancel handling and replay refusal. Exact auth-provider and protocol implementation choices require follow-up ADRs before implementation. This is planned M6 scope, outside MVP.

---

## 5. Technical architecture

### 5.1 Stack

| Area | Choice | Notes |
|---|---|---|
| Language | Go | Shared with providers, one binary, small attack surface |
| Router | `net/http`, Go 1.22+ | Minimal dependencies, built-in `GET /x/{id}` routing |
| API transport | Connect RPC/protobuf | `connect-go` on net/http; one schema generates Go server and TS client with end-to-end type safety |
| Real-time | Connect server-streaming | One mechanism for migration views, approval notifications, and later session monitoring; no separate SSE/WebSocket; after reconnect, fetch current state through unary RPC before resubscribing |
| Metadata | PostgreSQL | Compose initially; external PG or CNPG-managed PG through Helm/Kustomize in M2 |
| Metadata access | `sqlc` on `pgx` | Raw SQL with type safety, no ORM |
| Target DB access | Dialect adapters/native drivers | Explicitly isolate PostgreSQL/MySQL differences |
| Authentication | argon2id passwords, Google OIDC, M3 Keycloak OIDC, server sessions | `coreos/go-oidc` + `x/oauth2`, server callbacks without frontend SDK; broader federation later |
| Authorization | Go RBAC/org scope | Repository enforcement and cross-org integration tests; no metadata RLS in MVP, schema remains RLS-ready, ADR-0004 |
| Frontend | SolidJS SPA/Vite | CSR, embedded using `go:embed` |
| UI | Kobalte/Tailwind/TanStack Table | Data grid is central to the product |
| Schema engine | Atlas Community CLI subprocess | Pin version/checksum and isolate behind `SchemaEngine` |

UI requirements are in §7; customization must preserve authentication, authorization and approved execution.
Per [ADR-0036](../adr/0036-ui-customization-boundaries.md), implementation details are maintained with the feature decision.

### 5.2 Metadata storage

**Metadata always uses PostgreSQL**, with the same schema, queries, and sqlc code in Compose or Helm.
Do not split storage by environment and duplicate implementations; do not use etcd or another KV store for metadata.

### 5.3 Database dialect boundary

PostgreSQL and MySQL satisfy the same governance and result acceptance criteria while exposing intentional dialect differences.
Application use cases retain approval, payload integrity, single-use execution, limits and audit across engines; dialect adapters handle native parsing, binding and execution.
Per [ADR-0001](../adr/0001-db-driver-and-parser.md) and [ADR-0025](../adr/0025-sql-database-first-expansion.md), driver/parser choices and supported-target scope are technical decisions.
See [database support](database-support.md) for tested features and engine versions.

### 5.4 Atlas boundary

M3 previews and M5 apply use the same immutable migration artifact and target, preserving review inputs and explicit unknowns.
Atlas Community supplies migration status, dry-run and apply; Portcullis supplies governance and review, without a replacement lint/pre-check engine.
Per [ADR-0012](../adr/0012-schema-governance-ops.md), the subprocess boundary, version/checksum pinning, artifact storage and recovery contract are technical specifications.
Embedding an Atlas Go SDK requires a separate licensing and public-API decision.

### 5.5 License map

| Feature | Source | Notes |
|---|---|---|
| Versioned migration status/apply/dry-run | Atlas Community, Apache 2.0 | Can bundle binary; pin version/checksum |
| Git integration, plan storage, approval | Own implementation | No Atlas Pro `schema plan` dependency |
| Migration lint/risk detection | Excluded | No own rules; humans and optional AI assess risk in review |
| Pre-migration check | Excluded | No assertion gate; use review |
| Deterministic migration facts | Own implementation + optional AI (§4.8) | Dry-run/native analysis; uncertain facts unknown, AI explanation only |
| EXPLAIN | Own implementation | Native DB command, independent of Atlas |
| Audit/approval/deployment history | Own implementation | Product core; Atlas Cloud charges for these |

Do not use Community-excluded declarative plans, migration lint, pre-checks, approval policy, Go SDK, or advanced DB objects, and do not build replacement lint/pre-check engines.
Differentiation comes from Git integration, governance, review, and UX.
Update the compatibility suite and license map together when changing Atlas versions.

### 5.6 Multitenancy

- All core tables contain `organization_id`; self-hosting uses one `default-org`.
- Enforce org scope in every repository query.
- Audit reads and writes name their organization explicitly and never default it; background maintenance visits every organization, ADR-0004/0009.
- Integration-test every endpoint against cross-org access by changing IDs.
- MVP uses shared tables/org IDs; decide schema/database isolation when moving to cloud.
- No MVP RLS under the application-only data-path threat model and operational constraints, ADR-0004; revisit for multitenant SaaS.

---

## 6. Data model overview

Metadata durably records organization identity, authentication, permissions, connections, immutable request/policy versions, decisions, execution outcomes and audit.
M3 adds versioned query assets and sharing; schema milestones add immutable migration artifacts and change history.
Result snapshots are temporary encrypted data: expiry, eviction or PostgreSQL crash/failover may make them unavailable without changing successful execution history or rerunning SQL.

Per [ADR-0004](../adr/0004-metadata-rls.md), [ADR-0009](../adr/0009-audit-integrity.md), [ADR-0011](../adr/0011-result-store-quota-and-eviction.md) and [ADR-0021](../adr/0021-governed-query-execution.md), organization scope, atomic audit and separate snapshot/execution durability are technical contracts.
Current table definitions belong in [tracked migrations](../../migrations/); future schema designs belong in their feature ADRs.

### 6.1 Audit events (ML-ready)

- Record actor, organization, time, action, target, outcome, previous/next states and request/payload evidence in a structured timeline.
- Attribute human and system/service actions explicitly; every admitted target attempt has start evidence and a confirmed outcome or `outcome_unknown`.
- Record refused owner-verified execution attempts without consuming approval, including invalid payload/state/policy, saturation and shutdown.
- Audit may include redacted SQL, statement class, affected rows and duration; raw SQL, comments, literal/parameter values, credentials, tokens and result rows must not appear in audit/logs.
- Archive preserves request, decision, execution and audit history; runtime access cannot update/delete audit, and automatic approval belongs to the system.
- Optional future AI risk data remains advisory; cryptographic protection against a self-hosted database owner is outside MVP.

Per [ADR-0009](../adr/0009-audit-integrity.md), [ADR-0016](../adr/0016-sql-redaction-and-named-binding.md) and [ADR-0021](../adr/0021-governed-query-execution.md), event fields, redaction and atomic execution evidence are technical contracts.

## 7. UX requirements

### 7.1 Results and pagination

- Use reloadable pages with browser history/back links for creation, review, policies and results; reserve modal confirmation for risky/destructive actions. Login returns to a safe same-origin requested page.
- Preserve typed SQL, reasons and last loaded data during refresh; clear data on principal/permission changes. Save draft and Submit are explicit, and navigation never silently saves plaintext or executes SQL.
- Show not-found/recoverable error pages instead of blank routes; optional instance-limit failures do not disable request pages or server enforcement.
- Format editable SQL locally on blur with opt-out, manual formatting and undo. Failures retain input; submitted SQL, parameters, approval evidence and execution input stay unchanged.
- Keep navigation/ordinary controls usable at 320 CSS pixels with keyboard focus; SQL/tables may scroll locally. Inline request progress shows only authorized facts and explicit unknowns.
- Lists have page numbers, ranges and stable order: page sizes 10/20/50/100, default 20, maximum 100; audit has no infinite scroll.
- Execute once and explore the same encrypted snapshot for 15 minutes, subject to eviction. Enforce the lower policy limit before ceilings of 10,000 rows/25 MiB; new policies default to 16 MiB, and additional memory limits may truncate sooner.
- Mark truncation and unavailability. A limited read stops at the ceiling; a returning write still commits the whole approved statement. Cache loss preserves durable execution history without rerunning SQL.
- Preserve exact numeric values, chronological typed temporals, stable ties and NULL-last sorting in either direction; unsupported temporal text follows typed values. Initially preserve query order and allow restoring it.
- Table/Text views and clipboard copy use the visible server page with headers; show copy failure inline. Recorded execution time includes DB connection/execution, collection and storage, excluding approval wait, rendering and later result exploration.
- CSV exports the whole original-order snapshot, independent of page/filter/sort, rechecking owner/org access. Stream within the same caps and escape spreadsheet-formula prefixes by default; raw export needs an explicit warning and opt-in.
- Default result storage is 512 MiB per organization and 64 MiB per user; preserve live results when a new snapshot cannot fit, show expiry/eviction, and bound temporary processing with default concurrency 2 and `429 Retry-After` on saturation.
- No unlimited/re-execution export, resident in-process result cache or claim of universal spreadsheet re-save safety.

Per [ADR-0005](../adr/0005-cellvalue-wire-contract.md), [ADR-0011](../adr/0011-result-store-quota-and-eviction.md) and [ADR-0021](../adr/0021-governed-query-execution.md), wire types, cache processing and execution are technical contracts.
Per [ADR-0022](../adr/0022-page-first-workflows.md), [ADR-0023](../adr/0023-local-sql-formatting.md) and [ADR-0033](../adr/0033-type-aware-result-sorting.md), routing, formatting and sorting implementation follow the corresponding technical contracts.
Per [ADR-0036](../adr/0036-ui-customization-boundaries.md), [ADR-0037](../adr/0037-page-hierarchy-and-responsive-ux.md) and [ADR-0038](../adr/0038-inline-request-workflow.md), customization, responsive navigation and inline progress preserve existing authorization.
Per [ADR-0039](../adr/0039-result-views-and-clipboard.md) and [ADR-0040](../adr/0040-recorded-execution-time.md), clipboard and timing behavior retain the same boundary.

Per [ADR-0055](../adr/0055-result-exploration-layout.md), result controls stay aligned and reachable on narrow screens; Text view wraps long values without dropping content.

- M1 account controls use a profile-image menu for identity, role, supported profile actions and sign-out. CSV export uses a confirmation dialog with scope, spreadsheet safety, progress, cancellation and errors rather than adding a prepared-download toolbar action (ADR-0056).
- M3 adds manually navigated pending-review cards, an authorized pending-review badge and requester status notifications. M3 profile settings use a Google account photo when available or a stable generated fallback, support user-selected images and preserve current account permissions.

### 7.2 Add connection

- Show type-specific fields and hide irrelevant ones.
  - PostgreSQL/MySQL: host, port, database, user, password, TLS mode.
- Common descriptors: environment `development|production`, with an explicit production badge, and optional description ≤500 characters.
- Test before saving.
- Only admins create/edit/test; never return stored credentials or original DSNs through UI/API after creation.

### 7.3 Migration workflow view (Schema milestone)

- Argo-style horizontal linear flow: status → dry-run → review → approve → apply → verify.
- Each step shows colored status, name, duration, and expandable details.
  - Status: applied/pending; dry-run: SQL; review: facts/unknowns; apply/verify: logs.
- Pause on approve with yellow state and approve/reject buttons, using access-core roles/policies.
- Push live updates through Connect streaming without separate SSE/WebSocket.
- Model the fixed six-step state machine directly; Argo/Temporal engines are not dependencies.

### 7.4 Approval notifications (MVP minimum)

- In-app notifications belong to MVP because approval turnaround directly affects the ≤30-minute median decision target (§2.4).
- Approvers see pending badges/lists; requesters see approval/rejection/expiry changes, refreshed through streaming or fallback polling. Fallback polling keeps the first list page current so new requests appear, and otherwise runs only while a shown request can still change; a terminal request's details are not polled (ADR-0037).
- Email/Slack delivery and its infrastructure/settings are post-MVP; the §2.2 alert-engine exclusion refers to BI alerts, not approval notifications.

---

## 8. Security and operations

### 8.1 Secrets and connections

- **Browser response hardening (ADR-0010):** Restrict scripts to the embedded SPA's origin, refuse inline/eval scripts and plugin content, and retain frame/base/form restrictions independently of inline style compatibility. Validate normal built-SPA workflows and refused inline/foreign scripts in a real browser; this does not replace output encoding or server authorization.

- **Encryption:** Store credentials and parameter values using versioned AES-256-GCM envelopes with per-record CSPRNG nonces.
  - Authenticate canonical AAD `portcullis/aad/v1|<record_type>|<organization_id>|<record_id>[|<chunk_index>]`, ADR-0003, to prevent ciphertext swapping.
  - Key version is bound through HKDF-derived wrap-key selection, not AAD; tampering causes decryption failure.
  - Refuse startup without a valid 32-byte master key.
- **Production keys:** Document mounted-secret injection instead of plaintext environment keys, with Compose examples; retain key IDs for re-encryption-based rotation. The local initializer must preserve an existing valid key across concurrent starts, reject invalid existing keys without replacement, and restrict file access to the nonroot runtime (ADR-0003).
  Key rotation must traverse organizations with old envelopes while retaining org-scoped locked reads, updates and audit evidence; administrative discovery returns only an organization identity.
  Key-rotation completion requires a successful count across all organizations proving no nonactive encryption envelope remains; unresolved rows refuse completion and historical integrity keys remain retained (ADR-0003/0004).
- **TLS:** Default PG/MySQL connections to certificate verification; relaxation requires explicit admin choice and auditing.
- **Exposure:** Never expose passwords, original DSNs, session tokens, plaintext parameters, or result rows in APIs/logs/audit.
  - Redact target DB errors before returning them.
- **Target privileges:** Provide least-privilege setup guidance and connection-test warnings matching policy.
- **Connection destinations (ADR-0051):** Connection tests, create/update and governed executions dial only addresses the operator destination policy permits.
  - Resolve the host once at dial time, refuse the target unless every resolved address is permitted, and dial only the checked addresses, so DNS rebinding cannot substitute an unchecked one.
  - Always refuse unspecified, link-local, multicast and broadcast addresses and cloud metadata endpoints; operators add `PORTCULLIS_CONNECTION_DENIED_CIDRS` and may restrict to `PORTCULLIS_CONNECTION_ALLOWED_CIDRS`.
  - Without an allow list, every other address except loopback is permitted; a co-located database requires listing loopback explicitly.
  - Every refusal returns the single `destination-refused` bucket without dialing, so callers cannot map internal networks.
  - Reject hosts that are not canonical IP literals but end in a numeric label, such as decimal, octal, hexadecimal or shortened IPv4 spellings, as invalid targets.
- **Git sources, Schema milestone:** AEAD-encrypt credentials and redact logs.
  - Enforce allowed Git hosts/outbound destinations, redirect restrictions, and DNS-rebinding defenses.
  - Cap clone size, files, and duration; disallow submodules, Git LFS, and symlinks by default.
  - Pin fetched migrations as artifacts for apply without rereading; define retention/access rules.

### 8.2 SQL execution safety

- Accept exactly one classified statement under the current dialect and read/write/DDL policy; reject transaction/session control, native file/network I/O and unknown forms.
- Bind typed parameters natively without interpolation and keep parsing, binding, redaction and target string interpretation consistent.
- Reject unsafe functions/operators, untrusted overloads and unsafe nested DDL expressions; target read-only mode alone is insufficient.
- Isolate execution connections and preserve transactional rollback where supported; disclose MySQL implicit-commit DDL before approval.
- Enforce statement timeout, separate bounded lock waits, row/byte and temporary-memory limits, cancellation and conservative unknown-outcome handling.
- Store/explore bounded snapshots without rerunning SQL; snapshot admission/storage failure preserves the committed statement outcome and reports result unavailability.
- Target-health admission may refuse execution before approval is consumed; cancellation, deadlines and local limits do not falsely mark the target unhealthy.
- Never automatically retry target execution; idempotency retrieves the existing attempt rather than creating another one.

Per [ADR-0002](../adr/0002-statement-classification.md), syntax and function/operator rules are explicit and fail closed.
Per [ADR-0010](../adr/0010-runtime-and-transport-defaults.md), [ADR-0016](../adr/0016-sql-redaction-and-named-binding.md), [ADR-0021](../adr/0021-governed-query-execution.md) and [ADR-0044](../adr/0044-postgresql-string-interpretation.md), timeout, binding, catalog checks, protocol limits and PostgreSQL string settings are technical specifications.

### 8.3 Authentication and sessions

- Store versioned argon2id parameters and rehash at login when policy changes.
- Store session hashes only; rotate on login/privilege escalation and revoke immediately on logout/admin action.
- Streaming can outlive sessions; cap streams at 30 minutes with quiet resubscription, revalidate every 60 seconds, and terminate on disable/revoke, ADR-0010.
  - Fallback notification polling is every 30 seconds (§7.4).
- Tokens contain 32 CSPRNG bytes; cookies use `__Host-`, HttpOnly, Secure, SameSite=Lax, Path=/.
  - State-changing requests validate HMAC-signed, session-bound double-submit CSRF: readable `__Host-` cookie equals `X-CSRF-Token`, plus HMAC verification, ADR-0006.
  - The CSRF token names its master-key version so sessions survive key rotation; a token whose key version is not loaded asks the user to sign in again instead of refusing every request, ADR-0006.
  - Do not use naive double-submit.
- **Host and origin boundary (ADR-0052):** Serve only `Host` headers naming an operator-configured public origin (`PORTCULLIS_PUBLIC_ORIGINS`), refusing others with 421 to defeat DNS rebinding; unset admits loopback hosts only and logs the exposure risk. Health probes are exempt.
  - Refuse unsafe cross-origin browser requests by Fetch Metadata or `Origin`; pre-session `Bootstrap`/`Login` additionally require `Origin` or `Sec-Fetch-Site`.
- Default idle expiry 12 hours and absolute expiry 7 days.
  - Apply IP and per-client account (email + client IP) token buckets from ADR-0010 and progressive account-linked failure backoff/lockout parameters from ADR-0006.
  - Other clients cannot exhaust a user's credential bucket. The shared account backoff can still be triggered by anyone who knows an email; per-device lockout is the documented follow-up and this residual risk is recorded in ADR-0006.
- **Bootstrap:** Only once with no users; disable afterward.
  - Alongside `/bootstrap`, support `PORTCULLIS_BOOTSTRAP_ADMIN_EMAIL` and exactly one `_PASSWORD` or mounted `_PASSWORD_FILE` source, with optional `_DISPLAY_NAME` defaulting to Admin, ADR-0006.
  - Startup invokes the same use case only with zero users and rechecks under lock against interactive races; otherwise log and skip.
  - Interactive `/bootstrap` requires a one-time setup token issued at each start while no user exists, ADR-0052. Store only its hash; deliver it once through the log or an owner-only `PORTCULLIS_SETUP_TOKEN_FILE`; expire it after 24 hours; replace it on restart; consume it atomically with admin creation. Refuse missing, wrong, expired, rotated or used tokens uniformly.
  - Incomplete configuration or weak passwords refuse startup.
  - Display name is required for every account; the account menu shows it with email fallback.
  - Show a stable, locally generated geometric profile image derived from the opaque user ID; reveal account identity through its menu without external fallback-image requests (ADR-0056).
- No public signup; admin-created users get a 24-hour one-time password-setup link, displayed once without MVP email delivery.
  - Store only the token digest, keep one open link per user, consume it atomically, revoke the user's sessions on completion and require a normal login afterwards; replayed, expired or revoked links get one generic refusal. Links are issued only to active users without a password; admin-initiated password reset is deferred (ADR-0053).
  - Admins list, create, disable/enable and assign one role per user, and create, edit and soft-delete custom roles from the seeded catalog; system roles are read-only and an in-use role cannot be deleted (ADR-0053).
  - Nobody grants, strips or locks out permissions they do not hold, and nobody disables or reassigns themselves (ADR-0053).
- **Google OIDC:** Authorization Code + PKCE, mandatory state/nonce, ID-token signature/issuer/audience/expiry and `email_verified` validation.
  - Use server `/auth/google/start` and `/auth/google/callback`; frontend links to backend without Google SDK.
  - Resolve existing `(issuer, subject)` links first. A new link requires verified email, current Google authority (Gmail or signed Workspace `hd`) and an admin-created existing user; historically verified third-party email alone is refused. No automatic signup or implicit third-party-email linking; explicit reauthenticated linking is future work (ADR-0007).
  - Pass short-lived state/nonce/PKCE verifier through an AEAD-encrypted `__Host-` cookie, ADR-0007.
- Never disable/delete/demote the last active admin; audit disabling/role changes and revoke sessions immediately.
- Disabled requesters cannot execute; revalidate unexecuted approvals from disabled users or users who lost approval permission.

- **Keycloak OIDC (M3):** Optional sign-in through an operator-configured Keycloak realm, with server-side Authorization Code + PKCE S256, state/nonce and signature/issuer/audience/expiry validation.
  - Administrators explicitly link the configured issuer and subject to an existing user; email equality alone never links accounts or creates users. Keycloak roles and groups do not grant Portcullis permissions.
  - Issue the existing HttpOnly server session and retain CSRF, account-disable, local logout and authorization checks. Provider tokens stay out of browser storage; Keycloak logout or account disable does not imply immediate revocation of an existing local session.
  - Verify a real Keycloak login, wrong-realm/token rejection, replay, denied linking, local session revocation and provider failure without bypassing authentication. Local administrative recovery remains available; broader federation and upstream session-revocation synchronization are separate scope.
  Per [ADR-0057](../adr/0057-keycloak-oidc-in-mvp.md), Keycloak extends provider scope while preserving the Google-specific linking rules of ADR-0007.

### 8.4 Audit, retention, and privacy

- Runtime cannot update/delete audit; audit every admin setting change.
- No MVP audit-deletion API, default indefinite retention; self-host operators own metadata backups/retention.
- Archive only with no executing requests, treating credential disposal, pool closure, and unexecuted-request expiry as one operation.
  - Preserve linked history; retention is separate from archive (§4.3).
- **Raw SQL:** Inline literals and comments can contain secrets, e.g. `UPDATE users SET token='secret' WHERE email='x'` or `-- customer token: secret`.
  - AEAD-encrypt raw SQL and parameter values in requests, saved-query versions including defaults, and migration artifacts.
  - Decrypt only for authorized users in approval UI.
- **Shared audit/AI redactor:** Remove comments, replace inline literals with typed placeholders, and preserve bind placeholders.
  - On parse/redaction failure, record only digest/class, never original SQL.
  - AI uses the same redactor.
  - Compute digest before encryption/redaction over the canonical whole §4.3 approval unit: version, org, requester, connection/config version, policy version, class, normalized SQL, and typed parameters.
  - Parameter, connection and policy changes require new approval (ADR-0018).
  - Recommend parameters when detecting sensitive literals at submission.
- Default encrypted request SQL/parameter retention is 90 days; deleting ciphertext retains digest/execution metadata.
- Encrypt snapshots with per-result AES-256-GCM DEKs in UNLOGGED storage and delete after default 15-minute TTL.
  - Never copy rows into audit/logs; accept crash/failover loss without automatic target re-execution.

### 8.5 Operations

- Terminate production TLS at proxy/ingress; ignore forwarded headers outside the trusted proxy list.
- Separate `/livez` from metadata-dependent `/readyz`; graceful shutdown blocks new execution, lets admitted executions finish within the drain, and interrupts the rest with an audited cause inside a total budget of drain delay plus shutdown timeout (ADR-0010/0021).
  - Executions still running when the shutdown timeout expires are cancelled and their outcomes recorded before metadata connections close; `EXECUTION_FINISHED` carries `interruption_cause` (`server_shutdown`, `owner_cancel` or `lease_lost`) (ADR-0010, ADR-0021).
- Structured logs/metrics contain request IDs, states, durations, and counts, excluding SQL/parameters/credentials by default.
- Document pre-migration backups/restoration and provide upgrade tests between supported versions.
- The metadata connection pool has configurable bounds on size, acquisition wait, statement, lock and idle-in-transaction time, ADR-0010.
- Metadata migrations refuse edited released files and versions the binary does not ship, wait a bounded time for another instance's migration lock, and bound each file's lock waits and statements, ADR-0009.
- Expose the tag-derived build version only in startup logs, never in Health responses (ADR-0054).
- Default external telemetry off; never transmit queries or usage metadata without user consent.

---

## 9. Deployment

- M1 ships the embedded-SPA container with a Docker Compose quickstart and private HTTPS production topology; network access does not replace application login, authorization, approval or audit.
- M2 adds single-instance Helm/Kustomize deployment after MySQL parity, with external or CloudNativePG-managed metadata PostgreSQL 18 and governed PostgreSQL targets.
- Kubernetes deployment preserves secrets/keys, separates migration-owner/runtime credentials, verifies TLS, supports backup/restore and tolerates lost results without rerunning SQL.
- M6 adds Terraform/OpenTofu after API stability and Gateway deployment after identity/masking acceptance. Kubernetes/CNPG availability alone does not establish application HA.
- Tagged publication verifies functionality and dependency security, then publishes AMD64/ARM64 images with SBOM/provenance, full-version/commit tags, stable minor aliases and no implicit latest. Operators protect tags and pin production digests.
- Release tags belong to the matching release branch, and publication promotes the verified image digest.

Per [M2 scope](../milestones/m2/scope.md), deployment completion criteria and sequencing are defined in the milestone document.
Per [ADR-0035](../adr/0035-kubernetes-cnpg-after-mysql.md), [ADR-0042](../adr/0042-private-network-deployment.md), [ADR-0047](../adr/0047-tagged-container-publication.md), [ADR-0049](../adr/0049-multiarchitecture-container-builds.md) and [ADR-0054](../adr/0054-release-branches-and-documentation-policy.md), packaging, topology and release ownership are technical decisions.
See the [release procedure](../operations/container-releases.md) and [deployment architecture](../operations/recommended-architecture.md).

## 10. Differentiation

| Compared with | Portcullis differentiation |
|---|---|
| kviklet 0.9.2 | Query versions/sharing/parameters/favorites → new approval requests, with schema governance/unified audit; validate UX through identical tasks, treating saved results/pagination/credential encryption as baseline |
| Bytebase | Truly free OSS self-hosting without HA/feature gating, lightweight single-binary Core 1/2, narrow and deep UX |
| Atlas Cloud | Access governance plus self-hosting without external SaaS and unified audit |

The differentiation hypothesis is OSS self-hosting, integration and usability, validated through identical user tasks.

---

## 11. Roadmap

Milestone scope and acceptance are defined in [the milestone directory](../milestones/README.md).
M1 is the first releasable alpha; M3 completes MVP, subject to product validation (§1.4).

| Milestone | Summary | Scope |
| --- | --- | --- |
| M0 | Foundation | [M0](../milestones/m0/scope.md) |
| M1 | PostgreSQL governance + user/role administration | [M1](../milestones/m1/scope.md) |
| M2 | MySQL → Kubernetes/CNPG → SQL review/EXPLAIN | [M2](../milestones/m2/scope.md) |
| M3 | Query assets + schema preview + Keycloak SSO; MVP | [M3](../milestones/m3/scope.md) |
| M4 | Masking → temporary console and identity | [M4](../milestones/m4/scope.md) |
| M5 | Schema approval/apply/recovery/verify | [M5](../milestones/m5/scope.md) |
| M6 | Providers + agent grants/MCP Gateway | [M6](../milestones/m6/scope.md) |
| M7 | Research candidates | [M7](../milestones/m7/scope.md) |

---

## 12. Decisions and open questions

### 12.1 Accepted decisions

- **Approval:** Per-connection/class read/write/ddl quorum 0–N, default 1, distinct active approvers and no self-approval.
  - Zero means audited system approval; pin policy versions and expire unexecuted requests on policy changes (§4.3).
- **Archive:** No hard delete; reject during execution, discard credentials, close pools, expire unexecuted requests, and preserve history (§4.3, §8.4).
- **Results:** UNLOGGED PostgreSQL result_cache with per-result AES-256-GCM DEKs, shared primary and accepted crash/standby loss.
  - Temporarily decrypt sort/filter data in concurrency-bounded workers (§6, §7.1).
- **Release:** Core 1-PG is first alpha; MySQL plus Core 2 complete MVP (§11).
- **Temporary access:** Web console reuses server execution for dialect-independent policy, result grid, and per-statement audit (§6.1).
  - Native DB proxy credentials remain Later pending demand.
  - Terminal-style UI, default stateless per statement; later connection-opted multi-statement transactions are read-only with hard idle timeout/automatic rollback (§4.6).

### 12.2 Decision references

Resolved items; their ADRs are binding specifications.

| Item | Resolution |
|---|---|
| DB versions/drivers/parsers | ADR-0001: pgx/go-sql-driver; PG pgplex/pgparser, MySQL tidb pkg/parser; SQLite scope removed by ADR-0025; ADR-0030 supersedes the open-ended version floor: PG 16/17/18/19 compatibility maintenance (19 preview until GA qualification); MySQL 8.4/9.7 LTS and 26.7 Innovation candidates |
| Statement classes/edge fixtures | ADR-0002: 27 literal fixtures and structural CTE-DML/SELECT INTO detection |
| Master-key format/rotation/loss | ADR-0003: single base64 file, `_PREVIOUS` versions, eager batch rotation, unrecoverable key loss; envelope/AAD/Argon2 parameters |
| Metadata RLS | ADR-0004: excluded in MVP, RLS-ready schema, mandatory cross-org tests |
| Result type contract | ADR-0005: proto/native scan-type→LogicalType mappings, fixed NULL sorting/tie-breakers |
| Result quotas/eviction/rejection/autovacuum | ADR-0011: user 64MiB, expiry→plan own/global LRU→admit atomically or reject preserving live data; provisional pending Core 2 load validation |
| Temporary-access threat model | §4.9: scope, expiry, concurrency, revocation, transaction boundary |
| Atlas pin/distribution/NOTICE/checksum/matrix | ADR-0012: Community v1.2 line, exact patch pinned at M5 start |
| Artifact storage/limits/retention/apply timeout/lock recovery | ADR-0012: metadata PG + AEAD; 1MiB/file, 10MiB/artifact, 500 files, terminal+90 days; 10-minute timeout/max 60 minutes; lease-row recovery |
| Runtime defaults | ADR-0010: timeouts, request limits, rate limiting, startup sequence |
| Cache-loss runbook before Helm | No retry and result_unavailable; Kubernetes deployment acceptance includes recovery procedures (ADR-0035) |

**License:** Portcullis uses Apache License 2.0 (ADR-0034), with the official text in the repository LICENSE and project attribution in NOTICE. Contributions intentionally submitted for inclusion follow section 5 of that license unless explicitly stated otherwise. Third-party components retain their respective licenses and notices.

**Remaining owner decisions:** Any separate CLA/DCO process, trademark policy, and paid-feature boundaries. This license decision does not introduce those policies, transfer copyright, promise foundation affiliation, or change feature scope. Hosted AI Review (§4.8) remains a possible monetization direction, not a decided paid feature.

---

## 13. External assumptions

Official sources were checked on 2026-06-27; revalidate feature/license assumptions before implementation and release.

- **Kviklet rechecked 2026-09-30:**
  - [Latest release API](https://api.github.com/repos/kviklet/kviklet/releases/latest)
  - [0.9.2 security release](https://github.com/kviklet/kviklet/releases/tag/0.9.2)
  - [0.9.0 features/proxy editions](https://github.com/kviklet/kviklet/releases/tag/0.9.0)
  - [0.8.0 UX/execution permissions](https://github.com/kviklet/kviklet/releases/tag/0.8.0)
  - [Pinned README](https://github.com/kviklet/kviklet/blob/0.9.2/Readme.md).
  - Evidence and impact are in ADR-0019.
- [Bytebase HA](https://docs.bytebase.com/get-started/self-host/high-availability): Self-host HA requirements and HA-enabled license.
- [Atlas Community](https://atlasgo.io/community-edition): Apache 2.0 scope and exclusions including declarative plans, lint, pre-checks, and Go SDK.
- [PostgreSQL UNLOGGED tables](https://www.postgresql.org/about/featurematrix/detail/unlogged-tables/): Crash truncation and lack of standby replication explain the result-cache loss model.
