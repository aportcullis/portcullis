# ADR-0069: Prepared user, role and binding administration RPCs

- **Status:** Accepted
- **Date:** 2026-10-18
- **Refines:** [ADR-0066](0066-resource-binding-authorization-snapshots.md)'s organization administration reads and [ADR-0067](0067-binding-management-transactions.md)/[ADR-0068](0068-binding-authority-account-and-role-management.md)'s mutation services.
- **Preserves:** [ADR-0064](0064-binding-schema-preparation-and-activation.md)'s atomic activation and existing production grants.

## Context

Prepared mutation services already enforce complete delegation, recovery protection, version checks and atomic session/revision/audit changes. They need a typed, independently qualified transport surface before atomic authorization activation. Mounting an active management API while bootstrap, sessions and governance still authorize legacy memberships would create competing authorities.

Connect supports shared interceptor chains; protobuf explicit presence preserves omitted edits. PostgreSQL repeatable-read snapshots can keep administration authorization, rows and totals coherent. The existing PRD requires numbered ordinary lists, separate resource permissions, server validation and immediate session revocation. These technical contracts implement those requirements without changing either PRD translation's product scope.

## Options

- Activate management RPCs immediately: violates the agreed atomic cutover.
- Publish schemas without executable handlers or database qualification: leaves transport semantics and protections unverified.
- Implement generated clients and real application adapters, qualify them in disposable databases, and mount production routes behind a mandatory inactive boundary (chosen).

## Decision

### RPC contracts and transport admission

Add Users, Roles and Bindings services in one administration schema, generating Go handlers/clients and TypeScript descriptors with the existing pinned toolchain. Shared SPA clients use the existing binary Connect transport and session-bound CSRF interceptor; this adds no management screen or navigation affordance.

| Service | Operations |
| --- | --- |
| Users | List, Get, Create, Disable, Enable, IssueSetupLink |
| Roles | List, Get, ListPermissions, Create, Update, Delete |
| Bindings | List, Get, Create, Update, Revoke, ValidateCondition |

Create establishes grantless membership and returns a one-time setup token. Grants are explicit Binding operations; there is no AssignRole or client-supplied principal/organization authority. Optional atomic initial bindings remain a later creation workflow. Password-setup consumption, bootstrap and session capability conversion remain separate activation obligations.

Derive the organization from server composition and the actor from the authenticated session context. Require the existing HMAC double-submit CSRF pipeline on every management procedure, including reads. Require canonical UUIDs and positive expected role/binding versions; never normalize an alias into a different lock or identity spelling. Mutation handlers call consumer-owned application ports directly, retaining locked current authority, all affected permissions and complete-catalog recovery protection. They never authorize through Auth.Me's legacy permission array.

Apply existing request-body limits and error/rate/origin boundaries in the production composition. All management responses, including session/CSRF refusals and setup-token responses, carry `Cache-Control: no-store`. Invalid inputs yield generic InvalidArgument, missing/inaccessible details share NotFound, delegation/self changes yield PermissionDenied, stale versions yield Aborted, and recovery/system-role/reference/lifecycle refusals yield FailedPrecondition. Storage and inactive-boundary failures yield generic Unavailable. Messages expose neither condition source nor driver diagnostics; freshly issued setup tokens appear only in successful issuance responses.

### Coherent authorized reads

Add a consumer-owned read repository and snapshot port. One PostgreSQL repeatable-read, read-only transaction loads current catalog, membership, database time, revision and bounded grants, evaluates the existing resource authorization service, then reads authorized metadata and totals. Reuse the resource snapshot loader inside this transaction, rather than completing a checkpoint in a different transaction. Missing catalog/revision, candidate overflow, storage, cancellation or deadline failure returns no page or count.

Per ADR-0066, Users.List and Roles.List target the organization; individual Get operations target their actual member or role. Define Bindings.List at organization scope and Bindings.Get at the stored binding. Conditional organization read grants may authorize these targets; connection grants do not cover organization administration. Lists contain the complete authorized organization collection, not a per-connection union or a hidden-row postfilter. Every request re-evaluates the current authenticated principal. ListPermissions requires roles.list and returns catalog data, without granting those permissions.

Mutation responses return only account identity/status or policy identity/version receipts, so a no-op status change or condition-preserving edit cannot bypass separate list/Get permissions. Return summaries separately from details: user lists omit password presence, role lists omit permission bundles, and binding lists omit condition source. Get requires its distinct permission and returns protected detail, preserving unknown condition contracts verbatim for inspection. Binding lists include current heads with explicit revoked state; immutable version history remains protected storage. Role usage reports live binding references independently of legacy member counts.

Use stable ID ordering and numbered pages. Zero selects page one; supported sizes are 10, 20, 50 and 100, with default 20. Page two and later require an expected authorization revision; any supplied revision must match the same read snapshot or the client reloads page one after Aborted. This revision is a consistency marker, not a bearer capability: every request checks its own session/principal and organization grant afresh. There are no client-controlled filters or ancestry fields in this first contract. Browser identity/filter/cache fencing remains UI work.

At most 100 records are materialized per page. SQL counts and page queries remain under the existing management transaction timeout, default two seconds and maximum five. Resource grant queries retain their configured candidate/deadline bounds, and conditions retain their parser, deterministic evaluation and operational limits. Database query plans may inspect additional rows under the deadline; these bounds do not establish capacity qualification or a workflow lease.

### Explicit conditions and validation

Create distinguishes an absent condition from a present empty/invalid condition. Update uses an optional role field, message presence for scope and a condition-edit oneof: absence preserves, replacement supplies complete source/contracts, and removal requires explicit true. Present-empty roles and false removal flags are invalid. Unsupported existing contracts cannot be silently preserved into a rewrite, replaced or stripped by an older client; authorized Get still retains their exact source.

ValidateCondition prepares the same restricted CEL accepted by writes, without evaluating a predicate or resolving caller-supplied attributes. Require current unconditional organization bindings.create or bindings.update authority in the existing locked policy transaction. Validation changes no binding, session, revision or success audit. Source/type/contract/budget errors are InvalidArgument; operational preparation cancellation/deadline refusal is Unavailable unless the enclosing request itself was canceled or expired. Apply this distinction to condition-bearing mutations too, keeping raw diagnostics outside errors and logs.

### Mandatory inactive production composition

Wire actual query, account, role, Binding and CEL adapters in the composition root. Append an explicit pending-administration interceptor after normal authentication/CSRF and rate/error handling. It returns Unavailable before every handler, even if an owner manually populated prepared policy data. This release exposes the contract while keeping every new read and mutation inactive; there is no environment switch enabling a partial cutover.

Do not add a migration, catalog seed, backfill, runtime write grant or legacy/new permission union. Existing production grants, bootstrap, sessions, governance and UI continue using their sole legacy authority. The pending interceptor can be removed only in the coordinated activation concern after migration equivalence/recovery, all readers/writers, bootstrap/session admission, bounded output and capability/API/UI contracts are qualified together. Runtime immutable-version UPDATE/DELETE restrictions remain mandatory. These API contracts do not complete M1.

## Consequences and acceptance

Observe behavioral red against unimplemented handlers before connecting the use cases. Raw generated Connect clients over real TLS, sessions, CSRF, PostgreSQL and CEL qualify successful account/setup/status, custom-role and scoped/conditional Binding journeys. Cover malformed/foreign IDs, forged actor/organization claims, missing/forged CSRF, canonical aliases, old/new delegation, self changes, stale versions, explicit edit presence, unknown source contracts, session revocation, audit rollback and competing Role/Binding removal of recovery capabilities. Preparation tests distinguish operational refusal from invalid source. Dedicated red scenarios reproduce account password-presence and condition-source disclosure through write responses without Get permission, then require narrow write receipts, also excluding profile data, binding scope/subject and role read metadata.

Read scenarios coordinate a committed authority/data change between snapshot authorization and data retrieval, demonstrating coherent rows/count/revision and refusal on the next fresh read. Budget, inactive, foreign and canceled queries return no data. Deliberately weaken guards and verify the scenarios detect defects before restoration. The actual embedded binary's browser scenario probes all eighteen prepared procedures, verifies authentication/CSRF before Unavailable and confirms unchanged Auth.Me authority. Disposable owner-ready API fixtures qualify transport behavior independently; they do not qualify migrated login/bootstrap or production activation.

Require full uncached `make verify`, separate `make supply-chain`, appropriate race scenarios and staged review before commit.

## Primary sources (verified 2026-10-18)

- [Connect Go v1.20.0 options](https://github.com/connectrpc/connect-go/blob/v1.20.0/option.go): interceptor composition and ordered repeated options in the pinned runtime.
- [Protocol Buffers field presence](https://protobuf.dev/programming-guides/field_presence/): optional/message/oneof presence distinguishes omission from explicit edits.
- [PostgreSQL transaction isolation](https://www.postgresql.org/docs/current/transaction-iso.html): repeatable-read snapshots keep authorization metadata and data reads coherent.
- [Google API pagination guidance](https://google.aip.dev/158): bounded list responses and continuation metadata; Portcullis retains the PRD's numbered pages and independently checks each request's authority.
- [OWASP authorization guidance](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html): deny by default and current server-side checks for protected operations.
