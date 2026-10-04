# ADR-0018: Access requests & approvals — state machine, immutable payload, quorum

- **Status:** Accepted
- **Date:** 2026-07-22

## Context
PRD §4.4 fixes the access-request state machine and §4.3 fixes the approval unit: an immutable payload (normalized SQL, typed parameter values, connection, requester, statement class, connection policy version) pinned at submit, N distinct active approvers with self-approval forbidden, `required_approvals=0` as system auto-approval, and a validity window from the Nth approval.
ADR-0015 deferred exactly one contract to this slice: a policy update must expire the connection's un-executed `pending`/`approved` requests in the same transaction as the policy pointer-bump.
This ADR fixes the schema, the concurrency mechanics, the payload cryptography, the expiry model, and the API surface for the M1 slice that ends at `approved`/terminal — execution (`executing` and its lease, PRD §4.4) is the next slice, but its states and transition edges are vocabulary this ADR already pins so the follow-up adds behavior, not schema.

The `requests.*` permission keys (`list/get/create/execute/approve/reject`) were seeded in migration 0002 with no enforcement site; this slice adds the enforcement.
The kviklet model the PRD cites was re-verified against 0.9.2 (2026-09-30): approvals are counted per distinct reviewer against a per-connection `numTotalRequired`; EDIT events reset prior approvals (execution errors also reset non-temporary requests).
Portcullis uses an immutable submitted payload; a change is a new request.
Since kviklet 0.8.0, other execute-right holders may execute approved single-execution requests; Portcullis deliberately retains requester-only execution and result access (ADR-0019).

## Decision

### Domain shape
- New domain package `internal/domain/access` (aggregate `Request`, entity `Approval`, enums `State`/`Reason`/`Decision`, value objects `Payload`/`SealedPayload`), app package `internal/app/accessrequest`, following the `connectionpolicy` naming precedent.
- The request stores the policy-vocabulary class `connection.StatementClass` (its doc comment already designates this reuse); the app service maps the parser's `query.StatementClass` through one explicit `mapClass` seam, and a pin test asserts the two three-value vocabularies stay aligned.
- **All ten states are defined now** — `draft, pending, approved, rejected, expired, cancelled, executing, succeeded, failed, outcome_unknown` — in the domain enum, the DB CHECK, and the proto enum, with the full transition table including the execution edges.
  This slice implements every transition up to and around `approved`; `approved→executing→…` stays unreachable until the execution slice.
  Lease fields (owner/deadline/heartbeat/attempt) are deliberately **not** columns here — they belong to the future `query_executions` table (PRD §6), so nothing is reserved speculatively.

### Immutable payload & cryptography
- The payload is the **raw SQL text plus the typed parameter values** — exactly the approval unit of §4.3.
  Bound SQL (`:name→$N`) is **not** stored; binding re-runs at execution time from the decrypted payload, so what approvers saw and what executes are byte-identical by construction.
- At rest the payload is AEAD-encrypted (§8.4) with the ADR-0003 envelope: a new record type `access_request_payload` in the canonical AAD `portcullis/aad/v1|access_request_payload|<org_id>|<request_id>`, stored in the same four-column sealed shape as connection credentials (`payload_key_version/wrapped_dek/nonce/ciphertext`).
  The plaintext is versioned JSON `{v, sql, params}`; the request id is app-generated (canonical UUID) before sealing, the ADR-0014 lesson.
- `payload_digest` = the keyring HMAC (`Keyring.Digest`, ADR-0003) over the **canonical serialization of the FULL approval unit** (amended 2026-07-24): payload-format version, organization, requester, connection, pinned policy version, statement class, normalized SQL (line endings + NFC only — no whitespace/semantic rewrite, §4.3), and the typed parameter values (sorted by name).
  Computed before encryption and redaction (§8.4), stored with its key version (the all-or-nothing pairing audit_events already uses).
  The digest thus binds *everything that was approved*, not just the SQL, so a change to a parameter, the connection, or the policy version yields a different digest and requires a new request — the OWASP transaction-authorization stance the executor slice re-verifies just before running (the earlier "exact SQL only" reading contradicted §4.3 and is corrected; PRD §8.4 is reconciled to §4.3 alongside this).
  The canonical builder is `access.CanonicalPayload`; the codec's `Digest` takes the canonical bytes.
- `redacted_sql` (ADR-0016 redactor) is stored on the row — the canonical copy; audit events **copy** it into their metadata (§6, self-contained audit).
- Submit pipeline order (corrected 2026-07-24 to match the code and PRD §4.3): `BindNamed → ParseSingle → Classify → mapClass → policy gate (pinned) → Redact → Digest → Seal`. **BindNamed runs first** — `:name` is not PostgreSQL syntax, so the grammar cannot parse the raw payload; the lexer pass rewrites `:name → $N` (validating parameter names/arity/types on the way) and the parser reads that.
  The bound string is not stored: execution re-binds from the decrypted payload, so approved and executed bytes match by construction.
  Unclassifiable, multi-statement, or rejected statements refuse submit (§4.3); parameters must match the statement's named placeholders exactly (BindNamed's `ErrUnknownParameter`/`ErrUnusedParameter` are submit-time failures).

### Policy snapshot — pinned by reference
- Submit reads the connection's **current** policy once; that read becomes the pin: `policy_version` and the applicable `required_approvals` are copied onto the request row.
- The execution limits are **not** copied: `FOREIGN KEY (connection_id, policy_version) REFERENCES connection_policy_versions (connection_id, version)` makes the pin referential, and policy version rows are append-only (ADR-0015), so joining the pinned row is stable forever and three limit columns of duplication are avoided.
- Every later stage (executor included) reads its class gate, quorum, and limits from the pinned version, never the current one (ADR-0015 consequence).

### State machine enforcement & quorum concurrency
- Requests and approvals are **never deleted**; terminal states are the preservation mechanism (§4.3 내역 보존), and `approvals` is append-only sensitive evidence (the `connection_policy_versions` posture: runtime `UPDATE` revoked, privcheck pins SELECT/INSERT-only).
- Every state-changing operation runs in one transaction: `SELECT … FOR UPDATE` on the request row, domain-validated transition, then a **conditional UPDATE guarded by the expected state** (`WHERE id=$1 AND state='pending'`), with zero-rowcount disambiguated the `missingArchivedOrPolicyConflict` way.
  PostgreSQL serializes `FOR UPDATE` on the same row (web-verified), so quorum arithmetic under the lock is race-free — *against other decisions on this request*.
  It says nothing about the identity tables; see the re-check's boundary below.
- Approve = one transaction: expire-if-overdue → lock row → re-validate the approver (below) → `INSERT` the approval (`UNIQUE (request_id, approver_id)`; a 23505 maps to `ErrAlreadyDecided`) → count **valid** approvals → if `count ≥ required_approvals`, the `pending→approved` UPDATE fires with `expires_at = now() + validity`.
  The Nth distinct approval transitions exactly once because the count and the transition share the row lock.
- One rejection is terminal (`pending→rejected`); the rejection reason is required and lives on the approval row (≤1000 chars).
  Requester cancel is allowed from `draft`/`pending`/`approved` and needs no reason.
- **Under-lock approver re-check** (amended 2026-07-24): every decision re-validates, *inside the locked transaction*, that the approver is still `active` and still holds the action's permission — `requests.approve` for an approval, `requests.reject` for a rejection (`ApproverEligible`; ineligible → `ErrApproverIneligible` → `PermissionDenied`).
  The handler's coarse permission gate is a check-then-act window; without the re-check a permission revoked after it could still terminally **reject** a request.
  Approve is separately protected by the valid-count (a stale approval never counts), but reject is a single terminal act with no count to protect it — the re-check closes that gap.
  (§8.3 revokes sessions on role change, which shrinks the window to an already-in-flight request; the re-check is the contract-level guarantee §4.4 requires.)
- **Database lifecycle guard** (amended 2026-10-04): a `BEFORE UPDATE` row trigger on `access_requests` (migration 0018, modelled on the `query_executions` evidence guard) refuses with SQLSTATE `42501` any edge outside `access.State.CanTransitionTo`, any change to identity columns, any change to a submitted row other than state, reason, approval expiry, version, `updated_at` and the rotatable payload envelope, any change to a terminal row other than a key-rotation envelope rewrap, a submit snapshot stamped on a draft, an `expires_at` change outside the approving transition, and a reason change without its transition.
  Without it the runtime role's `UPDATE` grant alone allowed reviving a terminal request, extending an approval window or rewriting the redacted SQL approvers saw.
  The trigger runs as the invoking role, so it exempts principals holding the table owner's privileges: they could disable it anyway, and boot verification already refuses a runtime connection with that reach (ADR-0009).
  A pin test walks every state pair as the runtime role against the domain graph, and the store's transition helper also checks `CanTransitionTo` before its conditional update (`ErrInvalidTransition`).
- Draft edits (`UpdateDraft`) and `Submit` carry `expected_version` against the request's own `version` bigint — the connections optimistic-token pattern; a mismatch is `ErrConflict → Aborted`.

### Approver validity — computed, never stored
An approval **counts** iff, at count time: the approver's user row is `active`, the approver still resolves the `requests.approve` permission through the live membership→role→role_permissions join (the `PermissionsForUser` shape), and `approver_id ≠ requester_id`.
Approval rows are immutable; validity is a predicate evaluated when it matters (approve-time counting now, the pre-execution re-check in the next slice), so §4.4's rule falls out structurally: an invalidated approval in `pending` simply stops counting and the request stays `pending`; in `approved`, the pre-execution re-check finding `count < required` expires the request (`approval_invalidated`) instead of executing.
A pin test compares the SQL predicate against `PermissionsForUser` results so the two cannot drift.

### Expiry — lazy observation, no sweeper
- `expires_at` is set only by the `→approved` transition (Nth approval or auto-approval): `now() + validity`.
- Reads never mutate: `Get`/`List` derive an **effective state** (`approved` with `expires_at <= now()` renders as `expired`/`ttl_expired`), so read RPCs stay idempotent and cheap.
- **One boundary rule** (amended 2026-10-04): the window closes exactly at `expires_at` and an approval without a deadline counts as closed (`access.Request.ApprovalExpiredAt`).
  The read badge, list filters and counts, the lazy expiry under lock, and the execution gate all apply it; the reads and the lazy expiry used a strict `<` while the gate already expired at equality, so a request at its deadline could be shown approved and refused execution in the same instant.
- The observed transition (conditional UPDATE + same-tx `ACCESS_REQUEST_EXPIRED` event, actor=system) happens at the next mutating touch: `Approve`/`Reject`/`Cancel` run the expire-overdue statement **inside their own transaction, immediately behind the `FOR UPDATE` read**, and the execution slice's admission re-check is the authoritative gate.
  Amended 2026-07-25 (external review round 4): the observation used to run in a separate transaction *before* the acting one, which was a check-then-act — a TTL lapsing while the actor waited for the row lock went unnoticed and the request was recorded as cancelled or decided instead of expired.
  Two things make the in-transaction version correct: when the expiry fires the transaction still **commits** (the transition is the call's real outcome; the refused mutation is reported afterwards), and the deadline is compared against `clock_timestamp()`, not `now()` — `now()` is the *transaction's start* time and would judge by an instant preceding the lock wait.
  Trade-off accepted: the stored state may lag the displayed state until touched; no background sweeper exists in this slice.
- `state_reason` vocabulary (system-caused transitions only, CHECK-tied to `expired`/`cancelled`): `policy_changed`, `connection_archived`, `approval_invalidated`, `ttl_expired`.
  Requester cancels and approver rejections carry no reason column value.

### Auto-approval (`required_approvals = 0`)
Submit against a class whose pinned quorum is zero transitions `draft→approved` directly: no `approvals` row, no virtual user (§4.4).
The transaction writes two events: `ACCESS_REQUEST_SUBMITTED` (the requester) and `ACCESS_REQUEST_APPROVED` with `actor_type=system` (`actor_service=system:auto-approval`), and sets `expires_at` the same way an Nth approval would.

### Validity duration — a Tier-C setting
New registry descriptor `approval_validity` (`KindDuration`, default **24h**, bounds **[15m, 168h]** — §4.3), env seed `PORTCULLIS_APPROVAL_VALIDITY` consumed at boot through `platform/config` like every Tier-C tunable.
The DB-override path arrives with the settings-store slice (ADR-0017); until then the env seed is the operative org setting.

### Cascade hooks (same-transaction, ADR-0015 §Deferred discharged)
- **Policy update** (`ConnectionPolicyStore.UpdatePolicy`): immediately after the pointer-bump UPDATE, expire the connection's `pending`/`approved` requests (`expired`/`policy_changed`) via one UPDATE … RETURNING, and append one `ACCESS_REQUEST_EXPIRED` event per row (the `completeArchiveEvents` derivation precedent) — all inside the existing transaction.
- **Connection archive** (`ConnectionStore.Archive`): in the same transaction, `draft→cancelled` and `pending`/`approved`→`expired` (both `connection_archived`), with derived events; and archive now **refuses** when an `executing` request exists (§4.3) — the guard is added in this slice even though the state is not yet reachable, so the execution slice inherits it.
- Correlation: derived events copy `RequestID`/`SourceIP` from the triggering admin event, so the audit timeline links cause and effect.

### Audit vocabulary
`ACCESS_REQUEST_CREATED / UPDATED / SUBMITTED / APPROVED / REJECTED / CANCELLED / EXPIRED`, target type `access_request`.
From `SUBMITTED` onward every event fills the reserved execution-path columns — `connection_id`, `query_type` (the statement class), `payload_digest` + key version — and copies `redacted_sql` into metadata.
This requires extending `audit.Event` and the audit store's insert/list/get with those four columns (they exist in the schema since 0001 but were never written); the audit UI does not surface them yet.

### Transaction boundaries — the connection row lock (amended 2026-07-24)
Reading the connection's state and writing the request are two different transactions, and PostgreSQL Read Committed starts **each command with a new snapshot** (verified), so an application-level pre-read is a check-then-act race.
Both request writes therefore take a `SELECT … FOR SHARE` on the `connections` row inside their own transaction:

- `CreateDraft` re-checks `archived_at` under that lock.
  An archive committing between a pre-read and the insert has already run its cascade sweep, so the draft would survive on an archived connection — violating §4.3's "archive blocks new requests immediately".
- `Submit` re-checks that `connections.current_policy_version` still equals the version the pipeline pinned; a mismatch is `ErrPolicyConflict` (`Aborted`, "refresh and retry").
  The classify → gate → digest pipeline must run before the write, so the pin is an **optimistic token** here — the same `expected_version` idiom used elsewhere.
  Without this, a policy update committing after the pin would leave a request pinned to a superseded quorum that the cascade sweep never sees (the sweep only touches `pending`/`approved`, and the row was still a draft).

`FOR SHARE` is exactly the right strength: concurrent request writers do not block each other, while the policy bump and the archive — both `UPDATE`s on that row — wait for the commit.
Either they land first (we observe the new state) or we land first and their sweep processes our fresh row.
No window either way.

### Visibility & API surface
- **Reviewer = holder of `requests.approve` OR `requests.reject`** (amended 2026-07-24, user decision).
  A reviewer sees the organization's requests and can decrypt any payload; approve and reject are *separate* permissions a custom role can grant independently (they are first-class, §4.3), so someone who can only reject must still see what they decide on — visibility is the union of the two decision permissions.
  UI buttons are gated per key (Approve by `requests.approve`, Reject by `requests.reject`).
- **List scope:** a reviewer sees the organization's requests; every other caller's list is filtered server-side to their own requests (least exposure).
  The badge itself is **computed by the query and returned with the row** (`effective_state`/`effective_reason`, amended 2026-07-26): filtering in SQL and then re-deriving the badge in the application was two clocks answering one question, and at the TTL boundary — or under any skew between the database and the app server — a row counted as `approved` could be rendered `expired`.
  The response now carries what the query decided; `Request.EffectiveState` remains the domain's statement of the rule.
  `List`/`Count` filter and count on the **effective state** (an overdue approved request reads as `expired`), so the filter and totals match the badge the UI shows (amended 2026-07-24 — the stored state lagged under lazy expiry).
  Each list row carries its **valid-approval count** (computed in the query), so the list shows the true `N/M`.
  `List` follows the §7.1 contract (`page_size` allowlist `{10,20,50,100}`, default 20 for any off-list value; `created_at DESC, id DESC` tie-breaker; `total_count/total_pages`) plus a state filter.
- **Payload decryption:** `Get` returns the decrypted SQL and typed parameters only to the requester or a reviewer (approve∪reject) (§8.4's "approval UI, authorized users"); everyone else — and `List` always — gets `redacted_sql` only.
- **Complete mutation responses** (amended 2026-07-24): `Create`/`UpdateDraft`/`Submit`/`Cancel` return the full read view (connection name, requester fields, valid count) fetched in the mutation's own transaction, not a bare request.
- **Picking a target — `ListRequestableConnections`** (amended 2026-07-24): the seeded requester and approver roles hold `requests.create` but **not** `connections.list`, so the request form cannot use the admin connection list — a fixed dependency on it makes the default roles unable to request anything at all.
  A dedicated RPC gated by `requests.create` returns the **active** connections with only `{id, display_name, db_type, environment}` (no description, version, or archive state).
  In the app layer it is a **separate port** (`RequestTargets`) rather than another `Repository` method: enumerating targets is a different capability from storing requests (ISP); the postgres store satisfies both structurally.
  The SPA's landing route and nav are likewise permission-aware, decided by the **union of the capabilities each page offers** (`web/src/app/navigation.ts`, amended 2026-07-26): the requests section opens for `requests.list` **or** `requests.create`, and the list itself renders only for `requests.list`.
  Gating the page on the list permission alone would strand a custom role that may create but not list — ADR-0008 allows any combination, so the UI may not assume the seeded bundles (no such role can be created before the M4 role API, which is why this was latent).
- **The detail read is one snapshot too** (amended 2026-07-26): `Get` reads the request row (whose valid-approval count is computed in SQL) and the approval list as two statements, so it runs them inside the same `withReadSnapshot` — otherwise an approval committing between them answers with a count of zero beside a listed approval.
  The **page-overflow clamp moved into the repository** for the same reason: it used to be a second `List` call in the service, and two calls are two snapshots, so a set that shrank in between could leave `page=3` above `totalPages=1`.
  `RequestPage.Page` is now the page the store actually read.
- **A page and its total come from ONE snapshot** (amended 2026-07-26, corrected the same day): the list carries `count(*) over ()` instead of a separate `COUNT`, so the common path settles rows and total in a single statement.
  A window function sees the rows `WHERE` selected, before `LIMIT`/`OFFSET` — pinned by an integration test rather than assumed.
  The first version of this entry added a separate `COUNT` for the one case the window cannot serve (an empty page, where the service still needs a total to clamp a page overflow back into range, §7.1) and claimed "with no rows, the contradiction cannot arise". **That was wrong, and is withdrawn**: it only looked in one direction.
  A request created between the two statements gives `items=[]` with a positive total, and on page 1 the clamp does not fire, so the empty list sits above a positive count.
  The whole read therefore runs inside a **read-only REPEATABLE READ transaction** (`withReadSnapshot`), which takes the snapshot once and reuses it for both statements.
  That costs nothing: PostgreSQL is explicit that "only updating transactions might need to be retried; read-only transactions will never have serialization conflicts" — the earlier rebuttal of this approach ("it adds serialization-failure retries") was mistaken.
- **The capability-union routing rule is not requests-only** (amended 2026-07-26, external review round 12): the Connections section opens on `connections.list` **∪** `connections.create` and its table renders only for `connections.list`, exactly as the requests section does.
  Registering a connection stands on its own permission, so a create-only role was previously unable to reach the one page it could use — and typing the URL fired a list RPC that could only come back denied.
  `web/src/app/navigation.ts` holds the rule for both sections; its tests pin which keys do *not* open a page (update/delete/test alone, like `requests.approve` alone).
- **An action lives where its own key can reach it** (amended 2026-07-26, external review round 11): the owner's **Submit and Cancel sit on the list row**, not only inside Details.
  Their RPCs need `requests.create`; Details needs `requests.get`.
  Keeping them behind Details meant a role with `list` but not `get` could watch its own draft and never send or withdraw it — `ConnectionList` had this right all along (every row action carries its own key), and the requests list was the exception.
  Edit stays behind Details because it needs the decrypted payload only `Get` returns.
  For the same reason **"Save draft" is withheld from a caller without `requests.list`**: a saved draft is reached again *through the list*, so without it the button creates a row that needs its owner and can never be seen by them again.
  Submit stays offered — it hands the request to the approval flow and asks nothing further of the requester.
  (Whether an owner should get a narrow, owner-scoped `Get` instead is a server-contract change and stays an M4 question.)
- **UI affordances follow the atomic keys** (amended 2026-07-26): Details is gated on `requests.get`, and the owner's Edit/Submit/Cancel on `requests.create` — the keys their RPCs require.
  ADR-0008 lets a custom role hold any combination, so a role with `list`+`get` that owns a request would otherwise see three buttons the server always refuses.
  Reviewer actions already carried `requests.approve`/`requests.reject` individually.
- **Payload budget** (amended 2026-07-26; JSON note added the same day): `MaxPayloadBytes` (56 KiB) bounds the SQL **plus every parameter name and value**, not the statement alone — values are equally sealed, shipped, parsed, and executed, so a statement-only cap leaves the real input unbounded.
  The number follows the transport's 64 KiB request cap (ADR-0010) rather than standing on its own, the rule PRD §4.2 already states for saved queries: a domain limit above the request cap is unreachable, since the handler refuses before the validator runs.
  A transport contract test pins that the domain's maximum actually travels through the API, and the e2e harness mounts the same `WithReadMaxBytes` + `MaxBytesHandler` the composition root does — without them the harness would accept requests production refuses, and the relationship between the two limits would be untestable.
  The budget counts **raw bytes**, which is what the binary wire carries — so the SPA sends binary (`useBinaryFormat`, ADR-0013) rather than connect-web's JSON default.
  On JSON the same payload can be larger than the bytes the domain counted: escaping doubles a quote or backslash and turns a control character into six.
  A JSON client is still first-class, and a contract test runs the budget through one; it just has to live with the arithmetic — a payload of quotes at the budget is refused by the request cap, as `ResourceExhausted`, before the validator sees it.
- **The approval unit binds the connection's CONFIGURATION, not just its id** (amended 2026-07-27, external review round 13).
  The canonical unit gained `connection_config_version` and `CanonicalPayloadVersion` went to **2**; the submit snapshot also records `connection_fingerprint`.
  The id survives a config replacement while host, port, database, TLS mode and credential do not, so an approved request kept its approval and the executor's digest re-check would have waved it through — the approvers reviewed one database and the statement would have run on another (OWASP transaction authorization: *"If transaction data is modified, the code could invalidate any previously entered authorization data"*; PRD §4.3 says the same in its own words).
  Three parts hold it up: **(a)** `ReplaceConfig` expires the connection's un-executed `pending`/`approved` requests as `expired(connection_changed)` in its own transaction — the same hook the policy cascade uses, with its own reason and actor. **Drafts are deliberately untouched**: the connection is still there and a draft carries no approval, so it is simply submitted against the new configuration (unlike archive, which takes the connection away and cancels them). **(b)** `Submit` re-verifies the pinned config version against the locked connection row, exactly as it does the policy pin, and refuses with `ErrConnectionChanged` → `ABORTED`.
  A replacement committing between the pipeline and the write cannot land a request against a target nobody classified. **(c)** The fingerprint is **evidence, not a guard**: connections keep no config history, so without it an expired request could not say what it had been approved for.
  `canonical_test` pins the unit's exact field set, so widening or narrowing it forces a decision about the version constant.
  Execution-time re-verification remains the executor slice's seam (nothing calls `Execute` yet, so this was latent — but the write path that creates the hazard exists today, which is why the pin and the cascade land now).
- **Cascade and request writes date themselves after their locks** (amended 2026-07-26): the archive/policy cascade, and every request write that can park on a lock (`CreateDraft`, `UpdateDraft`, `Submit`, `Cancel`), take one instant *after* the lock and stamp the row and the audit events with it.
  `now()` — the transaction's start — put a cascade above its own cause in the trail.
  The full rule, the two PostgreSQL guarantees it rests on, and the test that enforces it are in **ADR-0009**; `UpdateDraft` gained an explicit `FOR UPDATE` statement of its own so the wait happens before the stamp rather than inside it.
- **UpdateDraft guards the version it READ** (amended 2026-07-26): same rule as Submit below, and the last place that was missing it.
  The checks the service makes (owner, still a draft) describe the row it read, so the write is guarded with **that** version and a client token naming another is `ErrConflict`.
  Handing the token through let a concurrent edit that landed at exactly that version be silently overwritten by a caller who never saw it — a lost update, which is what the optimistic token exists to prevent, and which the API contract (version mismatch → `Aborted`) promises not to allow.
  Blast radius is the owner's own draft, hence P3.
  The connection and policy services were checked and already guard on the version they read.
- **Submit guards the version it READ** (amended 2026-07-26): the pipeline classifies, gates, redacts and digests one payload — the one `GetSealed` returned — so the write is guarded with **that row's version**, and a client token that disagrees is `ErrConflict` before any of it runs.
  Passing the caller's number straight through let a requester name a version the pipeline never read: a concurrent `UpdateDraft` landing at that version satisfied the guard, and the row ended up holding payload B under payload A's class, digest and redacted SQL.
  Execution stays fail-closed (the digest re-check catches it), but approvers would already have reviewed — and the audit trail already recorded — a statement the request no longer holds.
- **The target is fixed at Create** (amended 2026-07-26): a draft's `connection_id` cannot change; `UpdateDraft` carries SQL and parameters only.
  Create is where the target is decided — it locks the connection row and refuses an archived one — and changing it afterwards would move the policy pin, the digest and the audit target at once, which is a different approval unit by any reading.
  PRD §4.4 said otherwise and is corrected alongside this (user decision 2026-07-26); the UI locks the picker once a draft is saved and offers "cancel and start a new request" instead.
- **Decision timestamps** (amended 2026-07-26, extending the auto-approval rule below to the manual path): `InsertApproval` stamps `decided_at` with `clock_timestamp()` and returns it, and the transition's `updated_at`, the validity window, and the APPROVED/REJECTED event's `occurred_at` are all derived from that one value.
  A decision transaction can sit on the request row lock, and `now()` is frozen at `BEGIN`, so defaults would date the decision to before the wait — while `expires_at` came from the app's clock, a different clock than the row's.
  Four facts about one moment, two clocks, no way to tell which is right.
- **The under-lock approver re-check has a boundary** (amended 2026-07-26): it guarantees that a permission revoked **before** the check cannot decide.
  It does not serialize against a revocation that commits *after* it — the request row lock does not cover the identity tables, and Read Committed gives each statement its own snapshot.
  Serializing needs both sides to lock the same row, and the revoking side does not exist yet (role management is M4).
  The decision path therefore takes its half now (`LockApproverMembership`, `for share`), so the M4 revoke path only has to take `for update` on the same row; until then that lock is a no-op by design.
  Stating this is the point: the earlier "race-free" wording promised a guarantee the code cannot deliver alone.
- **Auto-approval timestamps** (amended 2026-07-26): the quorum-0 approval instant is stamped by the DB inside the transaction that holds the connection row lock, and the row's `expires_at`, the system APPROVED event's `occurred_at`, and that event's metadata copy of `expires_at` are **all derived from it** (`SubmitAccessRequest` stamps `clock_timestamp() + validity`; the store recovers the instant as `expires_at - validity`).
  The application cannot know the instant — it builds the event before the lock is taken — so it deliberately omits `expires_at` from the metadata and the store fills it.
  Three facts about one moment must not be sourced from two clocks.
- **List consistency in the SPA** (amended 2026-07-24): after any mutation the store **re-reads the current page** rather than merging the response into the local list.
  A merge cannot know that the row no longer matches the active state filter (approving under "pending"), that it belongs on another page, or how the totals moved — the server's page is the source of truth (§7.1).
  The principal-generation fence still gates the re-read.
- Separate Connect service `AccessRequests`: `Create` (a draft), `UpdateDraft`, `Submit`, `Cancel`, `Approve`, `Reject`, `Get`, `List`; the six seeded `requests.*` keys are enforced per-RPC at the handler (`Approve`→`requests.approve`, `Reject`→`requests.reject`, mutation trio→`requests.create`, reads→`list`/`get`).
  Draft-first is also the UX: the dialog offers both "Save draft" and "Submit"; a draft persisted in a dialog session is reused (not re-created) on a subsequent save/submit, and its owner can edit it from the detail view (user decision 2026-07-22).

## Consequences
- The execution slice only adds: the `query_executions` table + lease, the breaker-gated executor consuming the sealed payload via `BindNamed`/`Execute`, the authoritative pre-execution revalidation (`approval_invalidated`), the **executor-side function resolution** (ADR-0002 "Function effects": resolve every referenced function/operator to an OID under a pinned `search_path` and match a trusted catalog — the classification-time name allow-list is a coarse pre-filter, and `provolatile` is a hygiene check, not a boundary, since PostgreSQL treats volatility as "a promise to the optimizer" it never enforces), the digest re-verification just before running (§4.3), and the `executing`-edge implementations — every contract it needs (pinned policy join, effective-state derivation, archive guard, digest) is fixed here.
- Approval evidence is append-only and validity is derived, so "who approved, and did it still count" is answerable from history without trusting mutable rows.
- The lazy-expiry model means a dashboard can show `expired` while the row still says `approved`; every path that could *act* on the request observes the TTL under the row lock it holds (see above), so the lag is cosmetic only.
- A policy change or archive atomically invalidates in-flight requests with a first-class audit trail — no window where an old quorum approves against a new policy.

## Sources (checked 2026-07-22)
- kviklet 0.9.2 review model (re-checked 2026-09-30) — per-connection `numTotalRequired`, distinct-reviewer counting, EDIT/error resets and executor authorization: https://github.com/kviklet/kviklet/blob/0.9.2/backend/src/main/kotlin/dev/kviklet/kviklet/service/dto/ExecutionRequest.kt (ADR-0019).
- PostgreSQL explicit locking — `SELECT FOR UPDATE` blocks/serializes concurrent locks on the row; READ COMMITTED re-evaluates after the wait: https://www.postgresql.org/docs/current/explicit-locking.html
- PostgreSQL error codes — `23505 unique_violation`: https://www.postgresql.org/docs/current/errcodes-appendix.html
- PostgreSQL trigger behavior — a trigger runs as the role that queued the event unless `SECURITY DEFINER`, and its error rolls back the statement (checked 2026-10-04): https://www.postgresql.org/docs/current/trigger-definition.html
- ADR-0003 (HMAC digest, AAD layout) · ADR-0015 (pinned policy versions, deferred expiry hook) · ADR-0016 (redaction contract) · ADR-0017 (Tier-C settings)
