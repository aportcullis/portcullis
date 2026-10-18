# ADR-0068: Account and shared-role management through binding authority

- **Status:** Accepted
- **Date:** 2026-10-18
- **Refines:** [ADR-0062](0062-role-bindings-and-conditions.md)'s account and shared-role administration, using [ADR-0067](0067-binding-management-transactions.md)'s transaction boundary.
- **Preserves:** [ADR-0064](0064-binding-schema-preparation-and-activation.md)'s inactive production composition and atomic activation.

## Context

Binding management already qualifies unrestricted delegation, complete-catalog recovery protection and atomic session/revision/audit changes. Existing user and role administration still derives both actor authority and affected permissions from a single membership-assigned role. Reusing those services for activation would ignore additional, scoped or conditional grants and revoke the wrong shared-role subjects' sessions.

The PRD already requires independent membership, all affected binding permissions, unconditional organization delegation and one complete recovery administrator. No product requirement changes are needed. Google IAM documents reusable permission bundles granted by bindings; OWASP requires current server-side authorization, and PostgreSQL transaction locks support Portcullis's stronger immediate revocation contract.

## Options

- Add a legacy/binding mode to existing repositories: mixes grant sources and leaves single-role commands available at activation.
- Activate the new account and role paths before other readers: violates the agreed atomic cutover.
- Add separately injected account and shared-role mutation services over the same locked policy transaction, and qualify them independently until activation (chosen).

## Decision

### Independent membership and complete delegation

Add a PolicyAccount value containing account data and password presence, with no assigned role. AccountPolicyService creates membership without implicit grants and atomically issues the existing digest-only, 24-hour setup token. The new mutation surface has no AssignRole command. Optional atomic initial bindings, authorized list/get surfaces and transport forms remain later API work; creating a grantless account satisfies the existing PRD option, and explicit Binding management assigns grants separately.

Disable, enable and setup-link reissue load the target independently of the legacy role column. Require the appropriate unconditional organization management gate and every permission in every non-revoked binding's live role, across all scopes and including false or expired conditions. The predicate's current value cannot reduce the permissions an account mutation affects. A revoked binding contributes nothing. Disabled actors, conditional or connection-only actor grants, self changes and foreign targets are refused. A repeated status is still fully authorized and recovery-checked, but produces no mutation, session revocation, revision or success event.

RolePolicyService creates, updates and soft-deletes custom definitions using the current transaction catalog. Editing requires every old and new permission at unrestricted organization scope. Deletion also compares the old set, refuses system roles and stale/nonpositive/overflowing versions, and refuses every non-revoked binding reference, including expired conditions. Historical revoked versions do not prevent deletion. BindingCount reports live binding references independently of legacy MemberCount. Shared-role edits may affect the actor's own binding; the complete permission and recovery checks still apply and its sessions are included in revocation.

After every tentative account or role mutation, require an active member holding all current catalog `users.*`, `roles.*` and `bindings.*` permissions through unconditional organization bindings, aggregating across roles. Reuse the same recovery query as Binding management; no legacy two-key predicate or role-name shortcut is allowed.

### Transaction, clock and evidence

Use consumer-owned account and role repository/transaction ports with constructor injection. Both PostgreSQL adapters share Binding management's read-committed transaction, organization administration lock and locked authorization revision. Re-read authority after waiting; do not authorize from a pre-lock checkpoint. Canonical UUID validation prevents aliases from splitting locks or bypassing self checks.

Lock account state with `FOR NO KEY UPDATE`, which serializes status changes while allowing the key-share reference locks taken by an already admitted login session insertion. A stronger `FOR UPDATE` lock would deadlock with login holding its session coordinator while administration waits for that coordinator. Load shared-role subjects through independent membership and current binding heads. A role change finds each affected subject once, including disabled accounts and conditional/connection bindings, irrespective of its legacy role assignment. Lock subject session coordinators in canonical user order and each subject's unrevoked session rows in identity order, then observe one database wall-clock instant. Use that instant for account/membership creation, token validity, permission removal, role deletion, session revocation and audit. Newly created, uncommitted accounts cannot already have sessions; their clock is observed after policy admission and before insertion.

Revoke affected sessions, increment the organization revision exactly once and append success audit in the same transaction. Account disable also revokes open setup links; enable retains binding and credential history. Setup-link replacement revokes prior links and affected sessions. Audit records account status changes or role version/permission differences, affected-subject count, revision and revoked-session count, without emails, raw tokens or condition source. Errors strip driver details; privilege refusals use the existing bounded detached audit policy. Cancellation, deadline, storage, recovery, CAS or evidence failure rolls back all writes, with no automatic replay after uncertain commit.

Account work uses the existing Binding management timeout. Shared-role work uses isolated `PORTCULLIS_ROLE_POLICY_TIMEOUT` (default two seconds, maximum five) and `PORTCULLIS_ROLE_POLICY_MAX_SUBJECTS` (default 1024, maximum 4096). SQL reads at most the configured number plus one distinct subject; overflow refuses before mutation rather than partially revoking sessions. Both locks and queries remain under the transaction deadline. Recovery stays a database predicate and does not materialize organization membership in application memory.

### Inactive composition and activation obligations

Introduce no migration, production catalog grant, revision seed, backfill, runtime privilege or production route change. Account creation deliberately requires the future independent-membership schema; it never supplies a compatibility role to satisfy the current required legacy column. Disposable owner-only databases remove that column to qualify the new services. Other scenarios retain the column and prove existing assignments and effective grants survive the qualified operations.

Activation must still switch bootstrap, password-setup completion and login/session admission, every governance reader/writer, authorized collections/output, API/UI capability contracts and runtime privileges together. Existing production user/role services remain solely on legacy authority until that cutover; they are not fallbacks for these new services. The future cutover must remove their single-role mutation surface and preserve current grants and recovery through migration validation. This slice does not activate Binding administration or complete M1.

## Consequences and acceptance

Real PostgreSQL scenarios first observe behavioral failures for the unimplemented services, then qualify grantless account creation, setup replacement, disable/enable, unchanged grants, empty/custom role creation, permission/name updates, binding-subject session fanout and deletion after revocation. Refusals include conditional/scoped authority, missing old/new/target permissions, self and foreign IDs, disabled actors, password-bearing/disabled setup targets, incomplete recovery, immutable system roles, stale versions, live expired references and fanout overflow.

Inject audit, revision, session and token failures to prove full rollback. Coordinate database-observed lock waits for current reauthorization, cancellation and post-session-lock time. Race shared-role removal against Binding revocation to preserve the final recovery administrator. Coordinate an admitted login holding the session coordinator and prove its session insertion completes and the subsequent account disable revokes that new session. Mutation checks must detect weakened guards. Require uncached database/race tests, full `make verify`, separate `make supply-chain` and staged review before commit; existing browser scenarios qualify the active legacy system only.

## Primary sources (verified 2026-10-18)

- [Google IAM overview](https://docs.cloud.google.com/iam/docs/overview): roles are permission bundles, bindings grant them, conditions constrain individual grants, and Google's eventual-consistency model differs from Portcullis.
- [OWASP authorization guidance](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html): least privilege, deny by default and current server-side authorization checks.
- [PostgreSQL explicit locking](https://www.postgresql.org/docs/current/explicit-locking.html): advisory and row lock behavior and consistent acquisition order.
