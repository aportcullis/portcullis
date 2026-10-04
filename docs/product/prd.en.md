# Portcullis — Product Requirements Document

> **Language:** English · [한국어](prd.ko.md) · [Documentation](../README.md)
> **Shared revision:** v0.12 / 2026-10-04. Update requirements and section numbers in both languages in the same change.
> **Scope amendment (ADR-0025):** PostgreSQL/MySQL targets only; SQLite excluded. MySQL parity and SQL review/preview precede deferred M6 MCP Gateway (ADR-0026/0028).
> **Deployment sequencing (ADR-0035):** M2 is MySQL parity → Kubernetes (Helm/Kustomize) and CNPG → SQL review/EXPLAIN.
> **Status:** Draft v0.12 (2026-10-04; 2026-07-04: resolved §12.2 decisions through ADR-0001–0012, quantified limits and contracts, added the §4.9 temporary-access threat model).
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
- **Enterprise IAM or identity store:** MVP provides local email/password accounts and Google OIDC; other OIDC providers, LDAP, SAML, and SCIM are later delegated to external IdPs.
- **Multitenant SaaS in MVP:** Retain model hooks while operating as single-org self-hosting.

### 2.3 MVP release boundary

#### Included

- **Target databases:** PostgreSQL and MySQL; metadata always uses PostgreSQL.
- **Deployment:** Docker Compose quickstart; M2 adds single-instance Kubernetes deployment through Helm/Kustomize and external or CloudNativePG-managed metadata PostgreSQL.
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
  - The config version was added on 2026-07-27, ADR-0014/0018, because an unchanged connection ID can hide replacement host, port, database, TLS, or credentials.
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
  - Both paths set `expires_at` inside the transaction after acquiring the row lock, added 2026-07-26.
  - Computing automatic approval time before locking could consume validity while waiting for policy/archive locks before the request is stored.
- **Statement policy:** Per-dialect parsers establish one statement and its class; uncertain classification is rejected, then connection `read`/`write`/`ddl` permissions apply. PostgreSQL DDL query bodies must pass the full read vocabulary and reject locking/unknown expressions. M1 rejects one statement combining DDL with nested DML instead of waiving an independently configured write policy (ADR-0002). PostgreSQL RENAME/DROP/COMMENT act only on the object kinds CREATE admits (table, view, materialized view, index, sequence, schema), and ALTER TABLE admits only an explicit subcommand list; roles, databases, routines, triggers, policies, extensions, ownership and trigger/rule/row-security toggles are refused (ADR-0002, 2026-10-04).
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
- ADR-0030 sets PostgreSQL compatibility maintenance to 16/17/18/19: preserve existing Portcullis behavior and fix regressions rather than expand engine-specific features or syntax. Keep 16 when adding 19. Version 19 remains preview until GA and final qualification. PostgreSQL explicitly uses four families; MySQL initially has at most three candidates (8.4 LTS/9.7 LTS/26.7 Innovation), excluding 8.0. Publish verified versus pending status, actual patches/digests, failures and skips. Metadata PostgreSQL 18 and capacity qualification remain separate; expansion or retirement requires an explicit scope decision.
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

- **Drafts:** SQL/parameters are editable only in `draft`; submit generates the digest and freezes the payload.
  - The connection is fixed during Create under its row lock, which rejects archived targets, amended 2026-07-26, ADR-0018.
  - Changing target changes policy pins, digest, and audit target, so create a new request and cancel the old draft.
- **Cancellation:** Requesters can cancel draft/pending/approved requests; terminal states cannot be undone.
- **Request narrative (ADR-0032):** New UI requests require a single-line title (≤200 Unicode code points) and offer an optional plain-text body (≤4,000 code points) for purpose and review context. The title appears in scoped lists and details; the body is encrypted with SQL/parameters and visible only through authorized request details. Draft replacement saves all fields under the same version token; submission freezes and authenticates the narrative. Legacy/API requests without narrative remain valid with an untitled fallback. Narrative shares the 56 KiB payload budget and is excluded from audit metadata.
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
  - Recover the oldest deadlines first; an attempt whose recovery fails, or that another transaction holds, is left for the next run without blocking later attempts or stopping startup (ADR-0021).
  - Reuse the mechanism and values for schema apply locks, ADR-0012.
- **Late completion:** Terminal updates require `state=executing AND owner=? AND attempt_id=?`.
  - If reconciliation already recorded unknown, a late worker cannot overwrite it; append only `LATE_COMPLETION_OBSERVED`.
- **Executing cancellation:** Attempt driver cancellation without guaranteeing success; confirmed cancellation becomes `cancelled`, otherwise `outcome_unknown`.
  - Cancellation before COMMIT is sent is confirmed `cancelled`, because the transaction cannot commit; the target's statement timeout or a local deadline before COMMIT is a confirmed rollback recorded as `failed`.
  - Only interruptions during or after COMMIT and connection loss remain `outcome_unknown` (ADR-0021).
- **Terminal states:** `succeeded`, `failed`, `outcome_unknown`, `rejected`, `expired`, `cancelled`.
- **Database enforcement, added 2026-10-04, ADR-0018:** A metadata row guard refuses runtime updates outside this graph, edits to the submit snapshot, approval-window changes outside approval, and any terminal-row change except key-rotation rewrapping.
- **Unknown resolution:** Operators manually inspect the target DB and record resolution as another audit event; never rewrite the original unknown outcome.
- **Visibility, added 2026-07-23, amended 2026-07-24, ADR-0018:** Reviewers holding `requests.approve` **or** `requests.reject` can see organization-wide requests and decrypt payloads; other requesters see only their own, enforced server-side.
  - Independent custom-role decision permissions require the union: reject-only users also need visibility.
  - Only the owner/reviewer may decrypt raw SQL/parameters; other viewers receive redacted SQL (§8.4).
  - Lists/counts use **effective state**, treating expired approved requests as expired, matching the UI.
  - An approval expires exactly at its `expires_at` for display, decisions and execution alike, amended 2026-10-04, ADR-0018.
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

### 4.10 WebMCP query assistance (M6 / Reach)

Browser WebMCP is deferred to M6 after M5 and stable query/review APIs (ADR-0026, amending ADR-0024/0025). It is outside MVP acceptance. PostgreSQL/MySQL parity and human SQL review/preview take priority. SQLite remains excluded. ADR-0028 promotes local/remote MCP Gateway integration to M6; browser WebMCP is an optional subsequent adapter. WebMCP activation additionally requires M4 masking acceptance and M6 authenticated agent registration/grants (ADR-0027); registration precedes integration.

- Expose connection discovery, visible SQL/typed-parameter composition, explicit draft save/submit, request/approval-state inspection, requester-only approved execution and bounded result-page retrieval as separate tools. Start with read queries; filling a form must not automatically persist or execute it.
- Add schema discovery only through a bounded, authorized and audited catalog use case. Discover/reuse saved queries when Library supplies those assets. Neither path grants arbitrary SQL execution.
- Reuse the current authenticated user/org and existing server permissions, CSRF, ownership, distinct reviewers, quorum, immutable payload/config/policy, single-use execution, cancellation, audit and result limits. No initial automatic approval/rejection tool. Require an explicit user execution request, and distinguish authenticated user attribution from untrusted agent/source labels.
- Keep ordinary work within pages. Provide a visible tool outcome and direct links; revoke registration and fence pending responses on logout, identity/permission changes and route teardown. Bound outputs and treat catalog/SQL/result content as untrusted data. Never export credentials, encryption keys or UI cookies.
- Feature-detect native browser support; keep the normal interface usable without WebMCP. Reverify the evolving API at implementation time. Acceptance requires a real supported-browser query journey, denied/revoked and cross-user cases, replay refusal, cancellation, exact/bounded results and unsupported-browser fallback; a fake registry alone does not prove compatibility.

### 4.11 Early SQL review and schema preview (M2–M3)

ADR-0026 advances review tools before agent integration. In M2, after MySQL parity and the Kubernetes/CNPG deployment gate (ADR-0035), show deterministic statement class, identifiable referenced objects and applicable policy/limits, with explicit unknowns. Add basic native EXPLAIN only for supported read statements, using typed parameters and fixed server-controlled options; reject ANALYZE, unsafe functions/operators and unclassified forms. Require org/connection authorization, archived-target checks, target/config/policy validation, bounded planning timeout/output, cancellation, requester-only plan access and audit. Planning does not approve or execute a request. Tie evidence to SQL/parameter digest, target/config, engine version and observation time; invalidate changed inputs and label costs/rows as estimates. Accept only after parser rejection, side-effect defenses, denied/cross-org access, stale inputs, sensitive plan output and real-engine scenarios pass for both DBs.

M3 adds the §4.5/ADR-0012 schema status → dry-run → deterministic review slice over a pinned immutable Git/Atlas artifact. Enforce the per-DB object matrix, permissions, audit and bounded subprocess/catalog operations. Display fact sources, observation time, estimates and unknowns. No migration apply endpoint is exposed until M5; preview does not simulate changes or guarantee rollback/lock safety. Actual apply must revalidate artifact and target state.

EXPLAIN ANALYZE, execute-then-rollback query dry-run, AI review and index recommendations remain deferred. Basic review facts do not promise exact affected rows or a risk score.

### 4.12 Sensitive-data masking and registered agents (M4 → M6)

Deliver server-side masking/withholding in M4 before registering or connecting agents in M6 (ADR-0027). Existing SQL audit redaction is not result-data masking. Admins manage versioned organization/connection disclosure rules; apply them before serialization across APIs, cells, CSV, SQL/catalog/plan/review metadata and future tools. Start with full redaction/omission. Requester ownership does not bypass masking; any human raw-view exception requires separate explicit permission and audit. Agents have no raw-view exception initially. Preserve approved SQL/parameters, execution semantics, encrypted originals and existing retention.

Agent output is default-deny and releases only approved protected fields. Never send secrets, credentials, raw SQL/parameters or raw sensitive cells. Withhold unknown lineage/aliases/expressions, unqualified free-text/JSON or unsupported encodings; masking errors refuse safely. Recheck current policy on every read/export/response and invalidate stale exports/transformed caches after rule changes. Restrict sorting/filtering/search that could disclose hidden values. Audit policy versions/decisions without sensitive values. Automatic detection assists policy setup, not authorization. Acceptance includes UI/API/CSV/full-cell parity, metadata/plan/error leaks, derived values, stale caches, failures, org isolation and canary-secret tests against both DBs.

After masking passes, org admins register agents with stable ID, owner, integration kind, pending/disabled/active/revoked state, allowed tools/connections, protected-output policy and expiring user delegation. Registration starts without grants; activation is explicit. Audit changes and use. Effective access intersects authenticated user, verified agent grant, org/connection and masking policies. Claimed names/IDs are untrusted. Revocation/expiry/logout prevents later calls and fences pending responses. Retain explicit user execution intent, distinct review, quorum, payload integrity and single-use execution; no automatic approval/rejection tools.

Before integration, a transport ADR must prove caller binding to an active registration/delegation. Native WebMCP alone is not agent authentication; refuse protected capabilities when identity cannot be verified. Registration neither installs executable plugins nor fetches arbitrary endpoints. M6 MCP Gateway authorization follows §4.13/ADR-0028 without browser-token passthrough. Acceptance includes forged IDs, cross-org access, revoked/expired grants, permission intersection and protected output. This governs Portcullis-mediated disclosure; it cannot constrain an external agent's independent browser/DOM access.

### 4.13 Agent-neutral MCP Gateway and deployment (M6)

After M4 masking and M6 registration/grants pass, provide an opt-in standard MCP Gateway over Streamable HTTP and a local stdio bridge to the same authenticated endpoint (ADR-0028). Target local generic clients, Claude Code and Codex without vendor SDK dependence. A client is supported only after its recorded version, negotiated protocol, authorization and real governed tool journey pass. Browser WebMCP is an optional follow-up, not the initial Gateway gate. Preserve one Go binary with optional adapter/subcommand and existing application ports; do not run or host agents/models, proxy arbitrary MCP servers, or let the bridge connect directly to target DBs.

Gateway validates authenticated issuer/audience/expiry/scope, active registration and expiring delegation per request; scopes intersect user/org/connection/tool/masking permissions. Standard HTTP authorization/resource discovery uses a tested compatible provider, which may be external. Registration is not OAuth client registration and never grants implicit access. No unauthenticated local mode, cookie/token passthrough or trusted agent-name headers. Stdio stdout is protocol-only with sanitized stderr; credentials are scoped and excluded from examples/logs. Local HTTP binds loopback by default; deployed access requires TLS/origin controls. Retain explicit user intent, distinct approval, immutable payload, one-time lease, bounded protected output, cancellation and audit.

Infrastructure owns TLS/ingress, provider hosting, Secret delivery/rotation, network policy and observability deployment; Portcullis retains identity and business-policy enforcement. Provide Helm/Kustomize examples for endpoint and auth-discovery routing, Secret references, probes, resource/body/stream limits and network restrictions. NetworkPolicy needs an enforcing plugin; Secrets need protected storage and access. Validate Kubernetes routing/discovery, rotation, proxy buffering/timeouts, restart/shutdown and no bypass. Multi-replica claims require shared registration/revocation state and one-time execution tests, not pod-local grants.

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
| Authentication | argon2id passwords, Google OIDC, server sessions | `coreos/go-oidc` + `x/oauth2`, server callbacks without frontend SDK; other OIDC/SAML later |
| Authorization | Go RBAC/org scope | Repository enforcement and cross-org integration tests; no metadata RLS in MVP, schema remains RLS-ready, ADR-0004 |
| Frontend | SolidJS SPA/Vite | CSR, embedded using `go:embed` |
| UI | Kobalte/Tailwind/TanStack Table | Data grid is central to the product |
| Schema engine | Atlas Community CLI subprocess | Pin version/checksum and isolate behind `SchemaEngine` |

UI customization (ADR-0036): developers can change project-owned design tokens (light/dark colors, typography, radius, application width/spacing), branding assets and presentational layout slots independently of session, authorization and request/result logic. Preserve current defaults and page-first workflows. This foundation does not promise runtime user settings or organization-specific branding.

UX refinement (ADR-0037): show the current navigation location, reflow navigation and ordinary request controls at 320 CSS pixels, and group request context, SQL and explicit draft/submission actions. Use consistent page hierarchy and visible keyboard focus; SQL/data tables may scroll locally. A wide-screen review guide stacks within the page on narrow screens. Submission never implies execution. Update actual README captures after appearance changes.

Inline progress (ADR-0038): clicking a request title expands Draft → Review → Ready → Execution below the row, using authorized summary facts and explicit unknowns rather than invented history. Results (ADR-0039) offer Table/Text views of the same bounded server page, visible-page clipboard copy with headers and spreadsheet formula escaping, and discoverable column sorting. View switches never execute SQL. Copy failure is reported inline. Initial loading uses fixed three-line skeletons; empty/error states remain distinct, and background refresh preserves known request details.

Execution timing (ADR-0040): label recorded execution time and rows affected in results and original-requester terminal request details. Use durable server metadata through the existing authorization boundary. Explain that time includes DB connection/execution, result collection and snapshot storage, excluding approval waiting, browser rendering and later paging/sorting. Do not show running or unavailable uncertain-outcome durations as completed measurements.

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
// postgresDialect, mysqlDialect
```

- Services own payload digest, approval, leases, timeout, row/byte caps, snapshots, and auditing.
- Adapters own connection validation, exact classification, bind syntax, read-only/transaction configuration, cancellation, and error redaction.
- Every DB passes the same contract suite; expose intentional differences in the matrix and UI.
- ADR-0001 fixes drivers/parsers: pgx and go-sql-driver/mysql; PG pgplex/pgparser and MySQL tidb pkg/parser. ADR-0025 removes SQLite from target scope.

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
- Audit reads and writes name their organization explicitly and never default it; background maintenance visits every organization, amended 2026-10-04, ADR-0004/0009.
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

connections              (PostgreSQL|MySQL; encrypted config, org_id, current_policy_version, archived_at)
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
  - Store connection snapshots excluding credentials.
- **Tamper limits:** Cryptographic evidence against direct self-host DB-owner tampering is Later.
  - MVP explicitly documents its append-only/runtime-permission boundary.

---

## 7. UX requirements

### 7.1 Results and pagination

Routine creation, editing, request review, policy settings and result exploration belong within pages; modal confirmation is reserved for risky/destructive actions (ADR-0022). Requests provide directly reloadable creation, detail and result routes with browser history and visible back links; a direct link that requires sign-in continues at that page afterwards, accepting only same-origin relative return paths (ADR-0022). Preserve typed SQL and decision reasons during background refresh; a transiently failed refresh keeps the last loaded rows labeled as such, clearing them only on a principal change or lost authorization (ADR-0037). Require explicit Save draft/Submit actions; route navigation must not automatically persist plaintext or execute SQL.

No route renders a blank page: unknown addresses show a not-found page inside the application frame, an unexpected rendering failure shows a recoverable error page, and request pages keep working when optional instance limits cannot be read (the server still enforces them; ADR-0022).

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
    A read stops reading the target at the ceiling and cancels the rest of the statement, so rows affected equals the delivered rows; a returning write still drains and commits the whole statement (ADR-0021).
    Row count, truncation and chunk count are authenticated with the encrypted chunks, so altered snapshot metadata reads as an unavailable result (ADR-0003/0011).
    Apply any lower connection-policy limit first (new policies default to 16MiB, ADR-0015/0021).
    Separate pre-decode cell/row memory admission can truncate wide NULL results earlier even when value payloads are small (ADR-0021).
  - Default global storage 512MiB with expiry/LRU, per-user 64MiB and eviction/rejection order in ADR-0011; display expiry/eviction.
  - Admission is serialized per organization and the global cap counts that organization's results (the whole install in single-org deployments); expiry purge skips rows in use, amended 2026-10-04, ADR-0011.
  - Original-order pages/CSV decrypt only required chunks; stream CSV without loading the whole snapshot.
  - Type-aware sorting (ADR-0033): preserve original query order initially; selected columns sort the complete cached snapshot using declared logical types. Keep exact numeric precision, compare canonical temporals chronologically, retain stable ties and NULL-last ordering in either direction. Unsupported temporal text follows typed values as a deterministic fallback. Show column/direction, allow restoring original order, and disclose that CSV exports original snapshot order.
  - Sort/filter through bounded server workers, not PostgreSQL ciphertext queries; decrypt at most 25MiB temporarily and return the requested page.
  - Default worker concurrency 2; saturation returns `429 Retry-After`.
  - No resident Go-heap result cache during TTL; row/byte caps and worker semaphore bound temporary memory.
  - Replicas share one primary; crash/standby failover can lose cache, producing `result_unavailable` without retry.
  - CSV uses identical snapshots/caps and rechecks original user/org permissions for every request.
  - Escape headers/cells with `=,+,-,@` or full-width counterparts after leading whitespace/control characters, and any tab/CR/LF in that prefix, by default (ADR-0005/0039). Preserve the original snapshot and exported text after the escape prefix; this is not a universal spreadsheet re-save safety guarantee.
  - Raw CSV requires an explicit option and warning; bytes/JSON/newline/encoding follow §12.2's result type contract.
  - No unlimited or re-execution-based export in MVP.

### 7.2 Add connection

- Show type-specific fields and hide irrelevant ones.
  - PostgreSQL/MySQL: host, port, database, user, password, TLS mode.
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
- **Production keys:** Document mounted-secret injection instead of plaintext environment keys, with Compose examples; retain key IDs for re-encryption-based rotation. The local initializer must preserve an existing valid key across concurrent starts, reject invalid existing keys without replacement, and restrict file access to the nonroot runtime (ADR-0003). Key rotation must traverse organizations with old envelopes while retaining org-scoped locked reads, updates and audit evidence; administrative discovery returns only an organization identity. Key-rotation completion requires a successful count across all organizations proving no nonactive encryption envelope remains; unresolved rows refuse completion and historical integrity keys remain retained (ADR-0003/0004).
- **TLS:** Default PG/MySQL connections to certificate verification; relaxation requires explicit admin choice and auditing.
- **Exposure:** Never expose passwords, original DSNs, session tokens, plaintext parameters, or result rows in APIs/logs/audit.
  - Redact target DB errors before returning them.
- **Target privileges:** Provide least-privilege setup guidance and connection-test warnings matching policy.
- **Git sources, Schema milestone:** AEAD-encrypt credentials and redact logs.
  - Enforce allowed Git hosts/outbound destinations, redirect restrictions, and DNS-rebinding defenses.
  - Cap clone size, files, and duration; disallow submodules, Git LFS, and symlinks by default.
  - Pin fetched migrations as artifacts for apply without rereading; define retention/access rules.

### 8.2 SQL execution safety

- Parse exactly one statement per dialect and check its class against policy; never authorize by keywords/regex alone.
- PostgreSQL execution pins `standard_conforming_strings=on` at connection startup and verifies the server report before any statement; missing or mismatched reports fail closed (ADR-0044). Parser, binder, redactor and target must share ordinary-string interpretation. Compatibility scenarios verify legacy `off` defaults on PG16–18 and rejection of `off` with preserved query meaning on PG19 preview.
- Convert named parameters into native binds without interpolating values.
- PostgreSQL sends declared parameter types as native OIDs, ADR-0016.
  - RFC 3339 `timestamp` maps to `timestamptz`; untyped null relies on SQL context and ambiguous expressions require explicit casts.
- Use a dedicated connection for each execution and enforce transactions/read-only where supported.
  - Document implicit-commit, timeout, and cancellation differences in adapter contracts and approval UI.
- **Read-only is insufficient for function side effects**, added 2026-07-24, ADR-0002.
  - PG `READ ONLY` blocks specified commands, not all disk writes; `SELECT` can invoke `dblink_exec`, `pg_notify`, `set_config`, advisory-lock, and server-file functions.
  - Enforce a classification-time function/operator allowlist, rejecting unknown, user-defined, and schema-qualified names.
    - The only qualified exception is the exact two-part `pg_catalog.<name>` call the PostgreSQL grammar itself substitutes for SQL-standard syntax (EXTRACT, SUBSTRING, POSITION, OVERLAY, TRIM, AT TIME ZONE, LIKE/SIMILAR … ESCAPE); user-written qualified calls stay rejected, and execution refuses any untrusted pg_catalog candidate of those names, added 2026-10-04, ADR-0002.
  - Apply it to **every class, including DDL**, added 2026-07-25; CTAS, expression indexes, and column defaults can invoke functions.
  - Exclusion-constraint `WITH` operators obey the same operator gate and execution-time catalog check; explicit operator classes are refused, and access methods are limited to the built-in index methods (btree, hash, gist, spgist, gin, brin) and the heap table method, added 2026-10-04, ADR-0002.
  - Verify all visible candidate OIDs for explicitly referenced functions/operators during execution using fixed `search_path` and trusted catalogs, alongside least target privilege (§8.1). M1 conservatively rejects any untrusted overload rather than reproducing selected-OID resolution (ADR-0021).
  - `pg_proc.provolatile` is an optimizer promise, not enforcement, corrected 2026-07-25; authors can declare side-effecting bodies STABLE and call volatile functions.
  - Use volatility only as hygiene for honestly declared builtins; read-only transactions are supplemental protection.
- **Size/storage:** Enforce byte and row caps against large-cell exhaustion.
  - ADR-0011 order: delete expired results → plan own LRU → plan global LRU preserving at least one result per other user → atomically evict and insert only if admission fits; otherwise reject only the new snapshot as `result_store_full`, preserving every live result with execution itself completed.
  - Snapshot persistence after a committed statement runs on its own bounded context, unaffected by the query deadline or a late cancellation; a rejected or failed snapshot keeps `succeeded` and records `result_store_full` or `result_persistence_failed` in `EXECUTION_FINISHED` without error text (ADR-0021).
- **Circuit breaker, ADR-0010:** Per connection, more than 5 consecutive failures opens for 60 seconds with one half-open probe.
  - Blocked calls return `Unavailable` before leasing, without retries.
  - User cancellation, context deadlines, server statement timeouts and local response limits are excluded from target-health measurements without clearing existing failures; execution uncertainty still records `outcome_unknown` (ADR-0010).
- Attempt driver cancellation on user cancel/context timeout; unconfirmed outcome is `outcome_unknown`, never guessed success/failure.
  - The local execution deadline exceeds the statement timeout by a fixed grace, so the target's own timeout fires and rolls back first (ADR-0021).
- Never automatically retry target execution; API idempotency keys only retrieve the existing attempt, never create another execution.

| DB | Read | Write/DDL |
|---|---|---|
| PostgreSQL | Read-only transaction | Commit/rollback transactional statements |
| MySQL | Read-only transaction | DML transaction; warn before possible implicit-commit DDL |

Reject server-file/network/session-affecting commands such as `COPY ... PROGRAM`, `SELECT ... INTO OUTFILE`, `LOAD DATA`, `ATTACH/DETACH`, and writable `PRAGMA` until separately designed for safety.

### 8.3 Authentication and sessions

- Store versioned argon2id parameters and rehash at login when policy changes.
- Store session hashes only; rotate on login/privilege escalation and revoke immediately on logout/admin action.
- Streaming can outlive sessions; cap streams at 30 minutes with quiet resubscription, revalidate every 60 seconds, and terminate on disable/revoke, ADR-0010.
  - Fallback notification polling is every 30 seconds (§7.4).
- Tokens contain 32 CSPRNG bytes; cookies use `__Host-`, HttpOnly, Secure, SameSite=Lax, Path=/.
  - State-changing requests validate HMAC-signed, session-bound double-submit CSRF: readable `__Host-` cookie equals `X-CSRF-Token`, plus HMAC verification, ADR-0006.
  - The CSRF token names its master-key version so sessions survive key rotation; a token whose key version is not loaded asks the user to sign in again instead of refusing every request, ADR-0006.
  - Do not use naive double-submit.
- Default idle expiry 12 hours and absolute expiry 7 days.
  - Apply IP and per-client account (email + client IP) token buckets from ADR-0010 and progressive account-linked failure backoff/lockout parameters from ADR-0006.
  - Other clients cannot exhaust a user's credential bucket. The shared account backoff can still be triggered by anyone who knows an email; per-device lockout is the documented follow-up and this residual risk is recorded in ADR-0006.
- **Bootstrap:** Only once with no users; disable afterward.
  - Alongside `/bootstrap`, support `PORTCULLIS_BOOTSTRAP_ADMIN_EMAIL` and exactly one `_PASSWORD` or mounted `_PASSWORD_FILE` source, with optional `_DISPLAY_NAME` defaulting to Admin, added 2026-07-23, ADR-0006.
  - Startup invokes the same use case only with zero users and rechecks under lock against interactive races; otherwise log and skip.
  - Incomplete configuration or weak passwords refuse startup.
  - Display name is required for every account; headers use it with email fallback.
  - Show a stable, locally generated geometric profile image beside the signed-in user's name, derived from the opaque user ID without external image requests (ADR-0041).
- No public signup; admin-created users get a 24-hour one-time password-setup link, displayed once without MVP email delivery.
- **Google OIDC:** Authorization Code + PKCE, mandatory state/nonce, ID-token signature/issuer/audience/expiry and `email_verified` validation.
  - Use server `/auth/google/start` and `/auth/google/callback`; frontend links to backend without Google SDK.
  - Resolve existing `(issuer, subject)` links first. A new link requires verified email, current Google authority (Gmail or signed Workspace `hd`) and an admin-created existing user; historically verified third-party email alone is refused. No automatic signup or implicit third-party-email linking; explicit reauthenticated linking is future work (ADR-0007).
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
- Separate `/livez` from metadata-dependent `/readyz`; graceful shutdown blocks new execution, lets admitted executions finish within the drain, and interrupts the rest with an audited cause inside a total budget of drain delay plus shutdown timeout (ADR-0010/0021).
  - Executions still running when the shutdown timeout expires are cancelled and their outcomes recorded before metadata connections close; `EXECUTION_FINISHED` carries `interruption_cause` (`server_shutdown`, `owner_cancel` or `lease_lost`) (ADR-0010, ADR-0021).
- Structured logs/metrics contain request IDs, states, durations, and counts, excluding SQL/parameters/credentials by default.
- Document pre-migration backups/restoration and provide upgrade tests between supported versions.
- The metadata connection pool has configurable bounds on size, acquisition wait, statement, lock and idle-in-transaction time, added 2026-10-04, ADR-0010.
- Metadata migrations refuse edited released files and versions the binary does not ship, wait a bounded time for another instance's migration lock, and bound each file's lock waits and statements, added 2026-10-04, ADR-0009.
- Default external telemetry off; never transmit queries or usage metadata without user consent.

---

## 9. Deployment

Tagged container publication (ADR-0047) verifies the tagged commit through CI before publishing one multi-platform image (`linux/amd64` and `linux/arm64`; ADR-0049) to GHCR. Accept `vMAJOR.MINOR.PATCH[-PRERELEASE]` without build metadata; publish full-version and commit SHA tags, with major.minor aliases only for stable releases and no implicit latest. Publication includes SBOM/provenance attestations and does not deploy the application. Operators configure package visibility and protect release tags; production deployments should pin digests. See the [container release guide](../operations/container-releases.md).

The recommended production topology keeps Portcullis and its databases private. Remote users reach only the application's HTTPS endpoint through organization-enrolled Cloudflare WARP with private-network Tunnel routing, or Tailscale with explicit grants. Network membership does not replace application login, RBAC, approval or audit. Operators enforce private exposure, routing, DNS and TLS; this recommendation does not change the local demo configuration (ADR-0042; [deployment architecture](../operations/recommended-architecture.md)).

| Channel | Scope |
|---|---|
| Docker Compose, MVP | Server + PG; IPv4-loopback-only local HTTP publishing; separate local quickstart and production mounted-secret examples |
| Helm + Kustomize, M2 immediately after MySQL parity | Single Portcullis replica; external or CNPG-managed metadata PostgreSQL 18; existing Secrets/mounted keys, verified TLS, least privilege, probes and resource/security settings |
| Terraform/OpenTofu, after API stability | CRUD product resources such as connections/policies using terraform-plugin-framework + Connect unary HTTP, or REST gateway if needed; publish to both registries |

The container image is the deployment source of truth.
Align Compose `.env` and Helm `values.yaml` keys to reduce documentation/support cost; build providers after the API stabilizes.

M2 deployment acceptance (ADR-0035) requires Helm chart and Kustomize base/overlays with equivalent configuration, installation/upgrade/restart scenarios, Secret and certificate rotation, master-key preservation, backup/restore and result-cache-loss runbooks. Separate the migration owner Job from the restricted runtime role; CNPG's generated database-owner credentials must not become runtime credentials. Use the primary read-write Service DNS with certificate verification for metadata and governed PostgreSQL targets, within the existing database-version matrix. Failover must preserve unknown-outcome handling without retrying target SQL; lost UNLOGGED results return `result_unavailable`. Pin and publish the tested Kubernetes/CNPG/tool versions at implementation. CNPG HA does not establish application HA; automatic target discovery remains M7. M6 adds Gateway-specific deployment examples to this M2 baseline.

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
2  Bridge       PostgreSQL/MySQL parity → Kubernetes (Helm/Kustomize) + CNPG → deterministic SQL review and basic read EXPLAIN
3  Core 2       Saved queries + similar-history suggestions/reuse across both DBs; schema status/dry-run/impact preview (no apply)
   ── MVP ──
4  Access       Sensitive-data masking first; temporary web console, multistage approval, OIDC/LDAP
5  Schema       Complete pinned schema approval/apply/recovery/verify using M3 preview contracts
6  Reach        Terraform/OpenTofu after API stability; agent registration/grants then MCP Gateway after M5 and masking (WebMCP optional)
7  Later        BI analysis/sharing (charts/dashboards), declarative GitOps, CNPG discovery, SIEM, ML/AI Review (§4.8)
```

Stage 1, PostgreSQL-only Core 1, is the **first releasable alpha**.
MVP means stage 3 completion, including MySQL parity, Kubernetes/CNPG deployment, SQL review/EXPLAIN and Core 2 with schema preview, excluding MCP Gateway/WebMCP, subject to interview-driven Core 2 adjustments (§1.4).
After foundation, develop vertical features with server APIs and SolidJS screens together, because UX is central to differentiation.

**Roadmap management:** Record new directions as Later candidates first, then promote them to concrete milestones after validating demand, goals, and prerequisites.
Before implementation, update PRD scope, acceptance criteria, and required ADRs; adding a candidate does not establish a delivery date or supported capability.

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

### 12.2 Decision status (2026-10-03)

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
| Cache-loss runbook before Helm | Model settled in §6/§12.1: no retry, result_unavailable; only runbook documentation remains when writing Helm |

**License decided by the owner (2026-10-03):** Portcullis uses Apache License 2.0 (ADR-0034), with the official text in the repository LICENSE and project attribution in NOTICE. Contributions intentionally submitted for inclusion follow section 5 of that license unless explicitly stated otherwise. Third-party components retain their respective licenses and notices.

**Remaining owner decisions:** Any separate CLA/DCO process, trademark policy, and paid-feature boundaries. This license decision does not introduce those policies, transfer copyright, promise foundation affiliation, or change feature scope. Hosted AI Review (§4.8) remains a possible monetization direction, not a decided paid feature.

---

## 13. External assumptions

Official sources were checked on 2026-06-27; revalidate feature/license assumptions before implementation and release.

- **Kviklet rechecked 2026-09-30:** [Latest release API](https://api.github.com/repos/kviklet/kviklet/releases/latest), [0.9.2 security release](https://github.com/kviklet/kviklet/releases/tag/0.9.2), [0.9.0 features/proxy editions](https://github.com/kviklet/kviklet/releases/tag/0.9.0), [0.8.0 UX/execution permissions](https://github.com/kviklet/kviklet/releases/tag/0.8.0), [Pinned README](https://github.com/kviklet/kviklet/blob/0.9.2/Readme.md).
  - Evidence and impact are in ADR-0019.
- [Bytebase HA](https://docs.bytebase.com/get-started/self-host/high-availability): Self-host HA requirements and HA-enabled license.
- [Atlas Community](https://atlasgo.io/community-edition): Apache 2.0 scope and exclusions including declarative plans, lint, pre-checks, and Go SDK.
- [PostgreSQL UNLOGGED tables](https://www.postgresql.org/about/featurematrix/detail/unlogged-tables/): Crash truncation and lack of standby replication explain the result-cache loss model.
