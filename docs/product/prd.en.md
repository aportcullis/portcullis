# Portcullis — Product Requirements Document

> **Language:** English · [한국어](prd.ko.md) · [Documentation](../README.md)
> **Shared revision:** v0.3 / 2026-10-03. Update requirements and section numbers in both languages in the same change.
> **Status:** Draft v0.3 (2026-07-04: resolved §12.2 decisions through ADR-0001–0012, quantified limits and contracts, added the §4.9 temporary-access threat model).
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
Portcullis licensing and paid-feature boundaries remain pre-publication decisions in §12.2.

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
- Ship the MVP through Docker Compose, followed by Helm and Terraform/OpenTofu providers.
- Deliver Core 1/2 in one Portcullis binary.
  - Images including Schema Governance also contain a pinned Atlas Community CLI.
- Prevent unapproved SQL or SQL changed after approval from reaching the target DB through any execution path.

### 2.2 Explicit non-goals

- **Initial BI scope:** Scheduling/BI alert engines, dozens of visualization types, and embedded/public dashboards are outside initial releases and may be considered in the later roadmap.
- **ETL/CDC:** No real-time streaming or automatic synchronization between connections.
- **Own schema diff engine:** Use the proven Atlas OSS engine.
- **Enterprise IAM or identity store:** MVP provides local email/password accounts and Google OIDC; other OIDC providers, LDAP, SAML, and SCIM are later delegated to external IdPs.
- **Multitenant SaaS in MVP:** Retain model hooks while operating as single-org self-hosting.

### 2.3 MVP release boundary

#### Included

- **Target databases:** PostgreSQL, MySQL, and SQLite; metadata always uses PostgreSQL.
- **Deployment:** Docker Compose quickstart with one server instance and PostgreSQL.
- **Authentication:** Local email/password with argon2id, Google OIDC, server-side sessions, and initial admin bootstrap.
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
- OIDC providers other than Google, LDAP, SAML, SCIM, and IdP group-role sync.
- HA, Helm, and Terraform/OpenTofu providers.
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
- Enforce default 30-second query timeout, maximum 5 minutes, maximum 10,000 rows, and a result byte cap on all three databases.
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

- **Connections:** Register PostgreSQL/MySQL/SQLite configurations, test before saving, and encrypt credentials at the application layer.
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
- **Parameters:** Named values such as `:start_date` become native bind parameters, never string substitution.
  - MVP types: string, integer, decimal, boolean, date, timestamp, UUID, null.
  - Table/column identifiers cannot be parameters.
- **No inherited approval:** Saved queries and versions have no approval state; each execution creates a new request with a connection and exact parameter values.
- **Result grid:** Sort, filter, paginate, and export CSV from the same snapshot without rerunning the query.

### 4.3 Core access policies

- **Approval unit:** An immutable payload combining payload version, normalized SQL, typed parameters, connection, **connection config version**, requester, statement class, and connection policy version.
  - Any change requires a new request.
  - The config version was added on 2026-07-27, ADR-0014/0018, because an unchanged connection ID can hide replacement host, port, database, TLS, or credentials.
  - Pinning only the ID would let digest verification pass against a different database than the approvers reviewed, contrary to OWASP transaction authorization.
  - Config replacement atomically expires unexecuted pending/approved requests as `expired(connection_changed)`; drafts survive and can be resubmitted against the new config.
  - Renaming preserves approval because it changes the descriptor, not the target; use separate `config_version` rather than descriptor `version`.
- **RBAC, ADR-0008:** SQL-seeded permissions form a Google-IAM-style `resource.verb` catalog; roles are DB-stored permission bundles.
  - Authorize by permission, never role name.
  - Seed roles are defaults, not a closed set; admins can create custom roles.
  - `requester` manages own requests/assets, `approver` additionally reviews others' requests, and `admin` additionally manages users/connections/policies/audit with all permissions.
  - Admins cannot approve their own requests.
- **Execution permission:** Only the requester can execute their approved request; no admin proxy execution in MVP.
  - kviklet 0.8.0 permits other execute-permission holders to run single-execution requests; Portcullis keeps its requester-bound approval/result contract, ADR-0019.
- **Single-person deadlock:** Keep no-self-approval even for admins; resolve solo self-hosting through per-connection/class `required_approvals=0`.
  - Submit transitions directly to `approved` with a system automatic-approval audit event.
  - Do not add an allow-self-approval toggle.
- **Validity:** Default 24 hours from the Nth approval or system approval, configurable by organization from 15 minutes to 7 days.
  - Both paths set `expires_at` inside the transaction after acquiring the row lock, added 2026-07-26.
  - Computing automatic approval time before locking could consume validity while waiting for policy/archive locks before the request is stored.
- **Statement policy:** Per-dialect parsers establish one statement and its class; uncertain classification is rejected, then connection `read`/`write`/`ddl` permissions apply.
- **Read-only defaults:** New connections use `read=true`, `write=false`, `ddl=false`; admin write/DDL enablement is audited.
- **Reuse friction:** Every saved-query execution creates a new request because approval is not inherited (§4.2).
  - Control friction through per-connection/class approval policy, extending the verified kviklet `numTotalRequired` model, 2026-06-27.
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
  - History snapshots contain connection ID, display name, DB type, and target fingerprint, excluding credentials and full SQLite paths.
  - Only separate retention jobs delete history; runtime cannot update/delete audit events (§8.4).

#### MVP database compatibility contract

| Feature | PostgreSQL | MySQL | SQLite |
|---|---|---|---|
| Connection test and TLS/path validation | Required | Required | Required |
| Single-statement parsing/classification | Required | Required | Required |
| Typed bind parameters | Required | Required | Required |
| Read/write/DDL policy | Required | Required | Required |
| Timeout and cancellation attempts | Required | Required | Required |
| Row/byte caps, snapshots and CSV | Required | Required | Required |
| Request → approval → execution → audit | Required | Required | Required |

- Do not label a DB supported in the UI until it passes these common acceptance tests.
- Reject transaction control, session mutation, native file/network I/O, multiple statements, and unclassified statements in MVP.
- Permit implicit-commit statements such as MySQL DDL only under allowed DDL policy and warn approvers that rollback may be impossible.
- Manage SQLite only as server-local files, with path/symlink checks and concurrent-write restrictions.
- Retain SQLite as a low-cost adapter-contract reference/demo without an external network DB (§5.3), while making its additional path/symlink/write protections mandatory (§8.1).

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

- **Drafts:** SQL/parameters are editable only in `draft`; submit generates the digest and freezes the payload.
  - The connection is fixed during Create under its row lock, which rejects archived targets, amended 2026-07-26, ADR-0018.
  - Changing target changes policy pins, digest, and audit target, so create a new request and cancel the old draft.
- **Cancellation:** Requesters can cancel draft/pending/approved requests; terminal states cannot be undone.
- **Decisions:** Approvers can approve/reject only pending requests and provide a reason.
  - Unique `(request_id, approver_id)`; reject requester self-approval.
  - Stay pending below N distinct active approvals; the Nth approves, and any rejection terminates as rejected.
- **Automatic approval:** `required_approvals=0` uses a state transition and `actor=system` audit event, without an approvals row or virtual user.
- **Approval revalidation:** Recheck approver activity and permissions when recording approval and immediately before execution.
  - Exclude invalid approvals from a pending count; keep pending.
  - If an approved request falls below quorum, refuse execution and transition to `expired(reason=approval_invalidated)`.
- **Leasing:** Only one conditional `approved → executing` update can acquire the lease.
  - `query_executions.request_id` is unique; store `owner` server ID, `deadline`, and `heartbeat`.
  - Lease acquisition and `EXECUTION_STARTED` share a metadata transaction before target execution, guaranteeing a recorded attempt without distributed transactions.
- **Completion:** Record `EXECUTION_FINISHED` and succeeded/failed terminal state; refresh the lease through heartbeats during execution.
- **Reconciliation:** At startup and every **30 seconds**, detect executing requests with expired heartbeat/deadline and transition to audited `outcome_unknown` without retry.
  - Heartbeat every **15 seconds**, extending deadline to `now + 60 seconds`, a 4-heartbeat grace period.
  - This is independent of query timeout; live heartbeats retain ownership, avoiding premature takeover.
  - Reuse the mechanism and values for schema apply locks, ADR-0012.
- **Late completion:** Terminal updates require `state=executing AND owner=? AND attempt_id=?`.
  - If reconciliation already recorded unknown, a late worker cannot overwrite it; append only `LATE_COMPLETION_OBSERVED`.
- **Executing cancellation:** Attempt driver cancellation without guaranteeing success; confirmed cancellation becomes `cancelled`, otherwise `outcome_unknown`.
- **Terminal states:** `succeeded`, `failed`, `outcome_unknown`, `rejected`, `expired`, `cancelled`.
- **Unknown resolution:** Operators manually inspect the target DB and record resolution as another audit event; never rewrite the original unknown outcome.
- **Visibility, added 2026-07-23, amended 2026-07-24, ADR-0018:** Reviewers holding `requests.approve` **or** `requests.reject` can see organization-wide requests and decrypt payloads; other requesters see only their own, enforced server-side.
  - Independent custom-role decision permissions require the union: reject-only users also need visibility.
  - Only the owner/reviewer may decrypt raw SQL/parameters; other viewers receive redacted SQL (§8.4).
  - Lists/counts use **effective state**, treating expired approved requests as expired, matching the UI.
  - Target selection uses a dedicated `requests.create`-gated list containing active connections and form fields, added 2026-07-24, ADR-0008/0018.
  - Default requester/approver roles lack `connections.*`; creation must not depend on the admin connection list.
  - SPA navigation and landing are permission-aware.
- **Submission, added 2026-07-23, amended 2026-07-24, ADR-0018:** `BindNamed(:name→$N, parameter validation) → parse → classify → pin current policy(class permission/quorum) → redact → digest → seal`.
  - HMAC the canonical **whole approval unit (§4.3)** before encryption/redaction, binding SQL, org, requester, connection/config version, policy version, class, and parameters.
  - Pin the policy through a composite `(connection_id, policy_version)` FK to an append-only version row; joined limits remain stable.
  - Execution rebinds stored original text, preserving approved/executed bytes.
  - Create always leaves a draft; only subsequent submit rejects policy violations, leaving the draft editable.
  - Decision processing rechecks active status and action-specific permissions under lock, blocking terminal rejection after permission withdrawal.
- **Payload budget, added 2026-07-26:** SQL plus parameter names/values must together fit the 64 KiB transport limit, as for §4.2 assets, ADR-0010.
  - Domain limit is 56 KiB, reserving the rest for Connect envelope, IDs, and metadata.
  - Counting SQL alone leaves values unbounded; a domain limit larger than transport would be unreachable through the API.
- **Decision time, added 2026-07-26:** Set `approvals.decided_at` using `clock_timestamp()` after locking; derive `updated_at`, expiry, and audit `occurred_at` from that one value.
  - `now()` is fixed at BEGIN before possible lock waits; mixing that default with app-clock expiry produces inconsistent times.
- **Automatic approval time, added 2026-07-26:** Let the DB timestamp approval after the connection row lock; derive request expiry, system APPROVED event time, and its metadata expiry copy from the same instant.
  - An audit timestamp computed before locking could disagree with the stored row.

### 4.5 Schema Change Governance (Schema milestone)

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
- **Compatibility:** Pin Atlas Community object support for each of PostgreSQL/MySQL/SQLite in a matrix.
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
| Single-query request/approve/reject | MVP | Common support for all three databases |
| Connection RBAC/read/write/DDL policy | MVP | Default read-only, no self-approval |
| Unified audit | MVP | Same timeline as query assets and schema changes |
| Comments/review suggestions | Post-MVP | Append-only discussion separate from state transitions |
| Temporary SQL access | Post-MVP | Web console session reusing server execution, dialect-independent policy and result grid; audit each statement, matching kviklet `Connection.kt` per-execute `saveEvent`, code checked 2026-06-27; threat model §4.9 |
| Multistage/role review gates | Post-MVP | Start with explicit quorum/role rules before a policy DSL |
| EXPLAIN | Post-MVP | Distinguish read safety from `ANALYZE` execution per DB |
| Google OIDC | MVP | Server callbacks, no frontend SDK; verified-email linking to admin-created users, no signup, ADR-0007 |
| Other OIDC/LDAP and group-role sync | Post-MVP | External IdP as source of truth |
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
  - Browser WebMCP query assistance is promoted to M2 (§4.10); this Later item covers remote/headless gateway integration beyond that browser workflow.
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

```go
type AIReviewer interface {
    // Inputs pass through the redactor (§8.4), excluding rows, plaintext parameters, and credentials.
    ReviewAccessRequest(ctx context.Context, in AccessReviewInput) (Review, error)
    ReviewMigration(ctx context.Context, in MigrationReviewInput) (Review, error)
}
// Review{Summary string; Risks []RiskSignal; Suggestions []string; Provider, Model string}
// RiskSignal{Kind string; Detail string; Confidence low|medium|high}
// Errors, timeouts, and quota exhaustion return no Review; callers record review_unavailable.
```

Inputs contain redacted SQL, statement class, fact summary (§4.5), and policy context.

### 4.9 Temporary web SQL console threat model (M4 gate, decided 2026-07-04)

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

### 4.10 WebMCP query assistance (M2 / Bridge)

Browser WebMCP is promoted from the broad Later agent direction into the next milestone, after M1's complete release gate (ADR-0024). In M2, implement the PostgreSQL browser-agent journey first, then the existing MySQL/SQLite parity track. This is planned scope, not an available feature. HTTP MCP gateways and remote/headless machine clients remain Later.

- Expose connection discovery, visible SQL/typed-parameter composition, explicit draft save/submit, request/approval-state inspection, requester-only approved execution and bounded result-page retrieval as separate tools. Start with read queries; filling a form must not automatically persist or execute it.
- Add schema discovery only through a bounded, authorized and audited catalog use case. Discover/reuse saved queries when Library supplies those assets. Neither path grants arbitrary SQL execution.
- Reuse the current authenticated user/org and existing server permissions, CSRF, ownership, distinct reviewers, quorum, immutable payload/config/policy, single-use execution, cancellation, audit and result limits. No initial automatic approval/rejection tool. Require an explicit user execution request, and distinguish authenticated user attribution from untrusted agent/source labels.
- Keep ordinary work within pages. Provide a visible tool outcome and direct links; revoke registration and fence pending responses on logout, identity/permission changes and route teardown. Bound outputs and treat catalog/SQL/result content as untrusted data. Never export credentials, encryption keys or UI cookies.
- Feature-detect native browser support; keep the normal interface usable without WebMCP. Reverify the evolving API at implementation time. Acceptance requires a real supported-browser query journey, denied/revoked and cross-user cases, replay refusal, cancellation, exact/bounded results and unsupported-browser fallback; a fake registry alone does not prove compatibility.

---

## 5. Technical architecture

### 5.1 Stack

| Area | Choice | Notes |
|---|---|---|
| Language | Go | Shared with providers, one binary, small attack surface |
| Router | `net/http`, Go 1.22+ | Minimal dependencies, built-in `GET /x/{id}` routing |
| API transport | Connect RPC/protobuf | `connect-go` on net/http; one schema generates Go server and TS client with end-to-end type safety |
| Real-time | Connect server-streaming | One mechanism for migration views, approval notifications, and later session monitoring; no separate SSE/WebSocket; after reconnect, fetch current state through unary RPC before resubscribing |
| Metadata | PostgreSQL | Container in MVP Compose; external PG/Helm later |
| Metadata access | `sqlc` on `pgx` | Raw SQL with type safety, no ORM |
| Target DB access | Dialect adapters/native drivers | Explicitly isolate PostgreSQL/MySQL/SQLite differences |
| Authentication | argon2id passwords, Google OIDC, server sessions | `coreos/go-oidc` + `x/oauth2`, server callbacks without frontend SDK; other OIDC/SAML later |
| Authorization | Go RBAC/org scope | Repository enforcement and cross-org integration tests; no metadata RLS in MVP, schema remains RLS-ready, ADR-0004 |
| Frontend | SolidJS SPA/Vite | CSR, embedded using `go:embed` |
| UI | Kobalte/Tailwind/TanStack Table | Data grid is central to the product |
| Schema engine | Atlas Community CLI subprocess | Pin version/checksum and isolate behind `SchemaEngine` |

### 5.2 Metadata storage

**Metadata always uses PostgreSQL**, with the same schema, queries, and sqlc code in Compose or Helm.
Do not split storage by environment and duplicate implementations; do not use etcd or another KV store for metadata.

### 5.3 Database dialect boundary

Keep placeholders, ASTs, and transaction differences in adapters so Core 1/2 services do not branch by DB.

```go
type QueryDialect interface {
    ParseSingle(sql string) (Statement, error)
    Classify(stmt Statement) (StatementClass, error)
    BindNamed(sql string, params []TypedValue) (boundSQL string, args []any, err error)
    ValidateConnection(ctx context.Context, cfg ConnectionConfig) error
    Execute(ctx context.Context, req ExecutionRequest) (ResultStream, error)
}
// postgresDialect, mysqlDialect, sqliteDialect
```

- Services own payload digest, approval, leases, timeout, row/byte caps, snapshots, and auditing.
- Adapters own connection validation, exact classification, bind syntax, read-only/transaction configuration, cancellation, and error redaction.
- Every DB passes the same contract suite; expose intentional differences in the matrix and UI.
- ADR-0001 fixes drivers/parsers: pgx, go-sql-driver/mysql, modernc.org/sqlite; PG pgplex/pgparser, MySQL tidb pkg/parser, SQLite engine authorizer.

### 5.4 Atlas boundary

Wrap Atlas behind an abstraction; callers know domain types and Atlas exists only behind one implementation.

```go
type SchemaEngine interface {
    Status(ctx, conn, source Source) (MigrationStatus, error)  // applied vs pending
    DryRun(ctx, conn, plan Plan) (Preview, error)              // pending SQL preview
    Apply(ctx, conn, plan Plan) (ApplyResult, error)
}
// v1: atlasSubprocessEngine invokes Community migrate status/dry-run/apply.
```

`Source` contains remote ID, commit SHA, path, ordered files/checksums, `atlas.sum`, Atlas version/options, and immutable artifact handle, matching §4.5.
`Plan` selects the migration set fixed in that artifact.
Review facts come from `Preview` SQL plus native DB analysis; AI only explains them and preserves `unknown`, never asserting uncertain affected rows.
Do not add or rely on own lint/pre-check engines.
An embedded Go integration requires separate Community-license/public-API validation and is not promised in the roadmap.

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
- Integration-test every endpoint against cross-org access by changing IDs.
- MVP uses shared tables/org IDs; decide schema/database isolation when moving to cloud.
- No MVP RLS under the application-only data-path threat model and operational constraints, ADR-0004; revisit for multitenant SaaS.

---

## 6. Data model overview

```text
organizations            (one default organization for self-hosting)
users                    (accounts)
organization_memberships (user-org; role_id → roles)
permissions              (resource.verb catalog; SQL-seeded, loaded at startup)
roles                    (org-scoped; name, is_system; 3 seed defaults plus custom roles)
role_permissions         (role-permission assignments; permission_key → permissions)
auth_methods             (password; separate from users for later authentication methods)
oidc_identities          (user-issuer-subject; Google OIDC; unique(issuer,subject))
sessions                 (opaque token hash, idle/absolute expiry, revoked_at)
oidc_providers           (enterprise, later)

connections              (PostgreSQL|MySQL|SQLite; encrypted config, org_id, current_policy_version, archived_at)
connection_policy_versions (connection, version, per-class required_approvals; one limit set per policy: timeout/rows/bytes, ADR-0015; created_by)
access_requests          (AEAD-encrypted SQL+params, payload_digest, redacted_sql, statement_class, policy_version, required_approvals, state, expires_at)
approvals                (request, approver, decision, reason, decided_at; UNIQUE(request, approver))
query_executions         (unique request_id, lease owner/deadline/heartbeat, attempt_id, outcome, result metadata)
saved_queries            (name, tags, visibility, owner, optional source_request_id; limits in §4.2)
saved_query_versions     (immutable AEAD-encrypted SQL/default values, parameter definitions, created_by)
saved_query_favorites    (per-user favorites)
schema_change_requests   (Schema milestone; commit SHA, artifact handle, plan hash, atlas.sum,
                          Atlas version/options, effective class/policy version,
                          migration lock/attempt/outcome, retention class, review summary, state)
audit_events             (unified timeline, structured for ML features)
```

The access-request row owns `redacted_sql`; audit events copy its value when recorded rather than referencing it, keeping append-only records self-contained and independent of later row changes.

Store result snapshots behind `ResultStore` in PostgreSQL `result_cache`, rather than an in-process cache.
`result_sets`/`result_chunks` are **UNLOGGED**; generate a per-result DEK for AES-256-GCM schema/row-chunk encryption and wrap it with the master key.
Authenticate result ID, chunk index, and owner org as associated data.
Only IDs/owners, row/byte counts, creation/expiry/access times remain plaintext.
Executions retain handle, expiry, row/byte counts, and truncation without a FK to UNLOGGED tables.

Replicas sharing a PostgreSQL primary share the cache, which does not survive crash recovery or replicate to standbys.
On crash/failover loss, show `result_unavailable` without re-executing; durable successful execution history remains.
Accept this loss model deliberately for the 15-minute TTL cache.

### 6.1 Audit events (ML-ready)

Use structured fields from the start for later anomaly detection.

```text
audit_events
  id, organization_id, occurred_at
  actor_type(user|system|service), actor_user_id(nullable), actor_service(nullable)
  action, target_type, target_id, outcome
  previous_state, next_state, payload_digest, request_id
  connection_id, query_type, rows_affected, duration_ms
  risk_score(nullable)                       -- populated later by ML/AI Review (§4.8)
  metadata(jsonb)
```

- **Runtime permissions:** Audit INSERT/SELECT only, no UPDATE/DELETE; separate from the schema migration owner.
- **Actors:** Automatic approval, reconciler, AI review, and late completions use `actor_type=system|service`.
  - User ID is nullable; nonhuman actors carry a stable `actor_service`, e.g. `system:reconciler`, `service:ai-review`.
- **Per-statement invariant:** Every attempt admitted to target execution, including direct web execution and later temporary sessions, has `EXECUTION_STARTED`.
  - Confirmed outcomes record terminal `EXECUTION_FINISHED`; unconfirmed outcomes record `outcome_unknown`.
  - Crashes can prevent proof of reaching the DB; the guarantee is a STARTED event per admitted attempt.
  - Preflight refusals of owner-verified requests, including integrity/state/policy failures, saturation, and draining, record `EXECUTION_REJECTED` without consuming a lease (ADR-0021).
  - Record comment-free, typed-literal-placeholder, bind-preserving redacted SQL plus digest, class, affected rows, and duration.
  - Never record raw SQL, plaintext parameters/literals/comments, or result rows; encrypt originals under §8.4, and fall back to digest/type only if redaction fails.
  - No temporary session can execute without history, matching kviklet `Connection.kt` per-execute `saveEvent`, code verified 2026-06-27.
- **Secrets:** No credentials, session tokens, or result rows in audit; parameter values are encrypted in payloads, with only digest in audit.
- **History:** Archive preserves requests/executions/audit (§4.3); no hard-delete API, with `ON DELETE RESTRICT` FKs.
  - Store connection snapshots excluding credentials/full SQLite paths.
- **Tamper limits:** Cryptographic evidence against direct self-host DB-owner tampering is Later.
  - MVP explicitly documents its append-only/runtime-permission boundary.

---

## 7. UX requirements

### 7.1 Results and pagination

Routine creation, editing, request review, policy settings and result exploration belong within pages; modal confirmation is reserved for risky/destructive actions (ADR-0022). Requests provide directly reloadable creation, detail and result routes with browser history and visible back links. Preserve typed SQL and decision reasons during background refresh, and require explicit Save draft/Submit actions; route navigation must not automatically persist plaintext or execute SQL.

Editable SQL fields format locally on blur by default, with opt-out, manual formatting and undo (ADR-0023). Formatting preserves case and parameter placeholders; failures retain the input. Submitted SQL remains the stored original, and formatting never changes approval evidence or execution input.

Provide explicit navigation position and stable access to one execution's results.
kviklet already has pagination, request filters, stored results, and full-cell views; validate UX through tasks rather than claiming pagination as differentiation, ADR-0019.

- **Request/query/audit lists:** OFFSET pagination for internal administration.
  - `page`, `page_size` in 10/20/50/100, default 20, maximum 100; sort uses allowed columns/direction.
  - Response: `{ items, page, page_size, total_count, total_pages }`.
  - Explicit page controls, numeric jumps, and ranges such as “1–20 of 1,340”; no infinite scroll in audit views.
  - Tie-breakers such as `ORDER BY created_at DESC, id DESC` stabilize boundaries.
  - Locally adopt keyset pagination for high-growth tables if needed, behind the shared list-helper interface.
- **Query snapshots:** Execute once, store AEAD chunks in PostgreSQL UNLOGGED storage for 15 minutes, and paginate/sort/filter without rerunning.
  - Stop at the first snapshot ceiling of 10,000 rows or 25MiB; mark `truncated=true`.
    Apply any lower connection-policy limit first (new policies default to 16MiB, ADR-0015/0021).
    Separate pre-decode cell/row memory admission can truncate wide NULL results earlier even when value payloads are small (ADR-0021).
  - Default global storage 512MiB with expiry/LRU, per-user 64MiB and eviction/rejection order in ADR-0011; display expiry/eviction.
  - Original-order pages/CSV decrypt only required chunks; stream CSV without loading the whole snapshot.
  - Sort/filter through bounded server workers, not PostgreSQL ciphertext queries; decrypt at most 25MiB temporarily and return the requested page.
  - Default worker concurrency 2; saturation returns `429 Retry-After`.
  - No resident Go-heap result cache during TTL; row/byte caps and worker semaphore bound temporary memory.
  - Replicas share one primary; crash/standby failover can lose cache, producing `result_unavailable` without retry.
  - CSV uses identical snapshots/caps and rechecks original user/org permissions for every request.
  - Escape cells starting `=,+,-,@` or leading tab/CR by default against spreadsheet formula injection.
  - Raw CSV requires an explicit option and warning; bytes/JSON/newline/encoding follow §12.2's result type contract.
  - No unlimited or re-execution-based export in MVP.

### 7.2 Add connection

- Show type-specific fields and hide irrelevant ones.
  - PostgreSQL/MySQL: host, port, database, user, password, TLS mode.
  - SQLite: server-local path under admin-configured root, and read-only setting.
- Common descriptors: environment `development|production`, with an explicit production badge, and optional description ≤500 characters, added 2026-07-18.
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
- Approvers see pending badges/lists; requesters see approval/rejection/expiry changes, refreshed through streaming or fallback polling.
- Email/Slack delivery and its infrastructure/settings are post-MVP; the §2.2 alert-engine exclusion refers to BI alerts, not approval notifications.

---

## 8. Security and operations

### 8.1 Secrets and connections

- **Encryption:** Store credentials and parameter values using versioned AES-256-GCM envelopes with per-record CSPRNG nonces.
  - Authenticate canonical AAD `portcullis/aad/v1|<record_type>|<organization_id>|<record_id>[|<chunk_index>]`, ADR-0003, to prevent ciphertext swapping.
  - Key version is bound through HKDF-derived wrap-key selection, not AAD; tampering causes decryption failure.
  - Refuse startup without a valid 32-byte master key.
- **Production keys:** Document mounted-secret injection instead of plaintext environment keys, with Compose examples; retain key IDs for re-encryption-based rotation.
- **TLS:** Default PG/MySQL connections to certificate verification; relaxation requires explicit admin choice and auditing.
- **SQLite:** Restrict real paths to configured root, rejecting symlink escapes, `:memory:`, URI bypasses, devices/special files.
- **Exposure:** Never expose passwords, original DSNs, session tokens, plaintext parameters, or result rows in APIs/logs/audit.
  - Redact target DB errors before returning them.
- **Target privileges:** Provide least-privilege setup guidance and connection-test warnings matching policy.
- **Git sources, Schema milestone:** AEAD-encrypt credentials and redact logs.
  - Enforce allowed Git hosts/outbound destinations, redirect restrictions, and DNS-rebinding defenses.
  - Cap clone size, files, and duration; disallow submodules, Git LFS, and symlinks by default.
  - Pin fetched migrations as artifacts for apply without rereading; define retention/access rules.

### 8.2 SQL execution safety

- Parse exactly one statement per dialect and check its class against policy; never authorize by keywords/regex alone.
- Convert named parameters into native binds without interpolating values.
- PostgreSQL sends declared parameter types as native OIDs, ADR-0016.
  - RFC 3339 `timestamp` maps to `timestamptz`; untyped null relies on SQL context and ambiguous expressions require explicit casts.
- Use a dedicated connection for each execution and enforce transactions/read-only where supported.
  - Document implicit-commit, timeout, and cancellation differences in adapter contracts and approval UI.
- **Read-only is insufficient for function side effects**, added 2026-07-24, ADR-0002.
  - PG `READ ONLY` blocks specified commands, not all disk writes; `SELECT` can invoke `dblink_exec`, `pg_notify`, `set_config`, advisory-lock, and server-file functions.
  - Enforce a classification-time function/operator allowlist, rejecting unknown, user-defined, and schema-qualified names.
  - Apply it to **every class, including DDL**, added 2026-07-25; CTAS, expression indexes, and column defaults can invoke functions.
  - Verify all visible candidate OIDs for explicitly referenced functions/operators during execution using fixed `search_path` and trusted catalogs, alongside least target privilege (§8.1). M1 conservatively rejects any untrusted overload rather than reproducing selected-OID resolution (ADR-0021).
  - `pg_proc.provolatile` is an optimizer promise, not enforcement, corrected 2026-07-25; authors can declare side-effecting bodies STABLE and call volatile functions.
  - Use volatility only as hygiene for honestly declared builtins; read-only transactions are supplemental protection.
- **Size/storage:** Enforce byte and row caps against large-cell exhaustion.
  - ADR-0011 order: delete expired results → own LRU → global LRU preserving at least one result per user → reject only the new snapshot as `result_store_full`, with execution itself completed.
- **Circuit breaker, ADR-0010:** Per connection, more than 5 consecutive failures opens for 60 seconds with one half-open probe.
  - Blocked calls return `Unavailable` before leasing, without retries.
- Attempt driver cancellation on user cancel/context timeout; unconfirmed outcome is `outcome_unknown`, never guessed success/failure.
- Never automatically retry target execution; API idempotency keys only retrieve the existing attempt, never create another execution.

| DB | Read | Write/DDL |
|---|---|---|
| PostgreSQL | Read-only transaction | Commit/rollback transactional statements |
| MySQL | Read-only transaction | DML transaction; warn before possible implicit-commit DDL |
| SQLite | `query_only` + transaction | Bound write-lock waits; transactional DDL only |

Reject server-file/network/session-affecting commands such as `COPY ... PROGRAM`, `SELECT ... INTO OUTFILE`, `LOAD DATA`, `ATTACH/DETACH`, and writable `PRAGMA` until separately designed for safety.

### 8.3 Authentication and sessions

- Store versioned argon2id parameters and rehash at login when policy changes.
- Store session hashes only; rotate on login/privilege escalation and revoke immediately on logout/admin action.
- Streaming can outlive sessions; cap streams at 30 minutes with quiet resubscription, revalidate every 60 seconds, and terminate on disable/revoke, ADR-0010.
  - Fallback notification polling is every 30 seconds (§7.4).
- Tokens contain 32 CSPRNG bytes; cookies use `__Host-`, HttpOnly, Secure, SameSite=Lax, Path=/.
  - State-changing requests validate HMAC-signed, session-bound double-submit CSRF: readable `__Host-` cookie equals `X-CSRF-Token`, plus HMAC verification, ADR-0006.
  - Do not use naive double-submit.
- Default idle expiry 12 hours and absolute expiry 7 days.
  - Apply IP/account token buckets from ADR-0010 and progressive account-linked failure backoff/lockout parameters from ADR-0006.
- **Bootstrap:** Only once with no users; disable afterward.
  - Alongside `/bootstrap`, support `PORTCULLIS_BOOTSTRAP_ADMIN_EMAIL` and exactly one `_PASSWORD` or mounted `_PASSWORD_FILE` source, with optional `_DISPLAY_NAME` defaulting to Admin, added 2026-07-23, ADR-0006.
  - Startup invokes the same use case only with zero users and rechecks under lock against interactive races; otherwise log and skip.
  - Incomplete configuration or weak passwords refuse startup.
  - Display name is required for every account; headers use it with email fallback.
- No public signup; admin-created users get a 24-hour one-time password-setup link, displayed once without MVP email delivery.
- **Google OIDC:** Authorization Code + PKCE, mandatory state/nonce, ID-token signature/issuer/audience/expiry and `email_verified` validation.
  - Use server `/auth/google/start` and `/auth/google/callback`; frontend links to backend without Google SDK.
  - Link by `(issuer, subject)` only when verified email matches an admin-created existing user; no automatic signup.
  - Pass short-lived state/nonce/PKCE verifier through an AEAD-encrypted `__Host-` cookie, ADR-0007.
- Never disable/delete/demote the last active admin; audit disabling/role changes and revoke sessions immediately.
- Disabled requesters cannot execute; revalidate unexecuted approvals from disabled users or users who lost approval permission.

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
  - The 2026-07-24 correction, ADR-0018, replaces the SQL-only description; parameter/connection/policy changes also require new approval.
  - Recommend parameters when detecting sensitive literals at submission.
- Default encrypted request SQL/parameter retention is 90 days; deleting ciphertext retains digest/execution metadata.
- Encrypt snapshots with per-result AES-256-GCM DEKs in UNLOGGED storage and delete after default 15-minute TTL.
  - Never copy rows into audit/logs; accept crash/failover loss without automatic target re-execution.

### 8.5 Operations

- Terminate production TLS at proxy/ingress; ignore forwarded headers outside the trusted proxy list.
- Separate `/livez` from metadata-dependent `/readyz`; graceful shutdown blocks new execution.
- Structured logs/metrics contain request IDs, states, durations, and counts, excluding SQL/parameters/credentials by default.
- Document pre-migration backups/restoration and provide upgrade tests between supported versions.
- Default external telemetry off; never transmit queries or usage metadata without user consent.

---

## 9. Deployment

| Channel | Scope |
|---|---|
| Docker Compose, MVP | Server + PG; separate local quickstart and production mounted-secret examples |
| Helm, post-MVP | In-cluster PG with optional CNPG, or external PG; existingSecret, ServiceAccount/least RBAC, probes, PodSecurityContext |
| Terraform/OpenTofu, after API stability | CRUD product resources such as connections/policies using terraform-plugin-framework + Connect unary HTTP, or REST gateway if needed; publish to both registries |

The container image is the deployment source of truth.
Align Compose `.env` and Helm `values.yaml` keys to reduce documentation/support cost; build providers after the API stabilizes.

Protobuf is the API source of truth: generate Go handlers and SolidJS TS clients together from `proto/`.
The provider plan assumes REST/OpenAPI, so choose direct Connect unary or generate an OpenAPI/REST gateway from protobuf when starting provider work; accept this transition cost after API stability.

---

## 10. Differentiation

| Compared with | Portcullis differentiation |
|---|---|
| kviklet 0.9.2 | Query versions/sharing/parameters/favorites → new approval requests, with schema governance/unified audit; validate UX through identical tasks, treating saved results/pagination/credential encryption as baseline |
| Bytebase | Truly free OSS self-hosting without HA/feature gating, lightweight single-binary Core 1/2, narrow and deep UX |
| Atlas Cloud | Access governance plus self-hosting without external SaaS and unified audit |

The proposed moat is OSS self-hosting, integration, and UX rather than feature count: a positioning strategy.

---

## 11. Roadmap

```text
0  Foundation   Skeleton, authentication, core schema, secret/audit/session foundations
1  Core 1-PG    PostgreSQL connection → request → approve → execute → audit vertical slice
2  Bridge       Browser WebMCP query assistance first; then MySQL/SQLite parity and shared tests
3  Core 2       Saved queries, favorites, sharing, parameters, result grid across all three DBs
   ── MVP ──
4  Access       Sequential validation of temporary web console, multistage approval, EXPLAIN, OIDC/LDAP
5  Schema       Git source, Atlas subprocess status/dry-run/apply, deterministic review/optional AI, Argo-style UI
6  Deployment   Helm/CNPG, Terraform/OpenTofu after API stability
7  Later        BI analysis/sharing (charts/dashboards), declarative GitOps, CNPG discovery, SIEM, ML/AI Review (§4.8), Agent Gateway integration (§4.7)
```

Stage 1, PostgreSQL-only Core 1, is the **first releasable alpha**.
MVP means stage 3 completion, including MySQL/SQLite parity and Core 2, subject to interview-driven Core 2 adjustments (§1.4).
After foundation, develop vertical features with server APIs and SolidJS screens together, because UX is central to differentiation.

**Roadmap management:** Record new directions as Later candidates first, then promote them to concrete milestones after validating demand, goals, and prerequisites.
Before implementation, update PRD scope, acceptance criteria, and required ADRs; adding a candidate does not establish a delivery date or supported capability.

---

## 12. Decisions and open questions

### 12.1 Accepted decisions

- **Approval:** Per-connection/class read/write/ddl quorum 0–N, default 1, distinct active approvers and no self-approval.
  - Zero means audited system approval; pin policy versions and expire unexecuted requests on policy changes (§4.3).
- **SQLite:** Retain for adapter/demo contracts with mandatory path/symlink/concurrent-write protections (§4.3, §8.1).
- **Archive:** No hard delete; reject during execution, discard credentials, close pools, expire unexecuted requests, and preserve history (§4.3, §8.4).
- **Results:** UNLOGGED PostgreSQL result_cache with per-result AES-256-GCM DEKs, shared primary and accepted crash/standby loss.
  - Temporarily decrypt sort/filter data in concurrency-bounded workers (§6, §7.1).
- **Release:** Core 1-PG is first alpha; MySQL/SQLite plus Core 2 complete MVP (§11).
- **Temporary access:** Web console reuses server execution for dialect-independent policy, result grid, and per-statement audit (§6.1).
  - Native DB proxy credentials remain Later pending demand.
  - Terminal-style UI, default stateless per statement; later connection-opted multi-statement transactions are read-only with hard idle timeout/automatic rollback (§4.6).

### 12.2 Decision status (2026-07-04; only licensing unresolved)

Resolved items; their ADRs are binding specifications.

| Item | Resolution |
|---|---|
| DB versions/drivers/parsers | ADR-0001: pgx/go-sql-driver/modernc; PG pgplex/pgparser, MySQL tidb pkg/parser, SQLite authorizer; PG ≥14, MySQL ≥8.0 with 8.4 LTS target |
| Statement classes/edge fixtures | ADR-0002: 27 literal fixtures and structural CTE-DML/SELECT INTO detection |
| Master-key format/rotation/loss | ADR-0003: single base64 file, `_PREVIOUS` versions, eager batch rotation, unrecoverable key loss; envelope/AAD/Argon2 parameters |
| Metadata RLS | ADR-0004: excluded in MVP, RLS-ready schema, mandatory cross-org tests |
| Result type contract | ADR-0005: proto/native scan-type→LogicalType mappings, fixed NULL sorting/tie-breakers |
| Result quotas/eviction/rejection/autovacuum | ADR-0011: user 64MiB, expiry→own LRU→global LRU→reject; provisional pending Core 2 load validation |
| Temporary-access threat model | §4.9: scope, expiry, concurrency, revocation, transaction boundary |
| Atlas pin/distribution/NOTICE/checksum/matrix | ADR-0012: Community v1.2 line, exact patch pinned at M5 start |
| Artifact storage/limits/retention/apply timeout/lock recovery | ADR-0012: metadata PG + AEAD; 1MiB/file, 10MiB/artifact, 500 files, terminal+90 days; 10-minute timeout/max 60 minutes; lease-row recovery |
| Runtime defaults | ADR-0010: timeouts, request limits, rate limiting, startup sequence |
| Cache-loss runbook before Helm | Model settled in §6/§12.1: no retry, result_unavailable; only runbook documentation remains when writing Helm |

**Only unresolved decision, owner approval before OSS publication:** Portcullis license, e.g. Apache-2.0/AGPL, CLA, trademarks, and paid-feature boundaries.
These directly affect the free-OSS positioning (§10) and are business decisions that documentation/implementation cannot substitute for the owner.
Resolve before publication; hosted AI Review (§4.8) is a likely monetization candidate.

---

## 13. External assumptions

Official sources were checked on 2026-06-27; revalidate feature/license assumptions before implementation and release.

- **Kviklet rechecked 2026-09-30:** [Latest release API](https://api.github.com/repos/kviklet/kviklet/releases/latest), [0.9.2 security release](https://github.com/kviklet/kviklet/releases/tag/0.9.2), [0.9.0 features/proxy editions](https://github.com/kviklet/kviklet/releases/tag/0.9.0), [0.8.0 UX/execution permissions](https://github.com/kviklet/kviklet/releases/tag/0.8.0), [Pinned README](https://github.com/kviklet/kviklet/blob/0.9.2/Readme.md).
  - Evidence and impact are in ADR-0019.
- [Bytebase HA](https://docs.bytebase.com/get-started/self-host/high-availability): Self-host HA requirements and HA-enabled license.
- [Atlas Community](https://atlasgo.io/community-edition): Apache 2.0 scope and exclusions including declarative plans, lint, pre-checks, and Go SDK.
- [PostgreSQL UNLOGGED tables](https://www.postgresql.org/about/featurematrix/detail/unlogged-tables/): Crash truncation and lack of standby replication explain the result-cache loss model.
