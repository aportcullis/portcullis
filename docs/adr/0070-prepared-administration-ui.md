# ADR-0070: Prepared user, role and binding management UI

- **Status:** Partly superseded by [ADR-0071](0071-binding-authorization-activation.md) for production activation and [ADR-0075](0075-local-draft-and-setup-receipt-lifetimes.md) for local creation inputs and setup-receipt disposal on organization revision changes
- **Date:** 2026-10-18
- **Refines:** [ADR-0069](0069-prepared-administration-rpc-contracts.md)'s prepared transport contracts and [ADR-0063](0063-condition-dsl-and-pure-evaluation.md)'s form-generated conditions.
- **Preserves:** [ADR-0064](0064-binding-schema-preparation-and-activation.md)'s atomic activation boundary.

## Context

The PRD requires independent user membership, reusable permission definitions and scoped conditional grants, ordinary numbered management lists, separate list/detail permissions and page-first forms. ADR-0069 supplies qualified RPCs but intentionally refuses production management calls until authorization readers and writers can switch together. A management UI must not create a second authority or mistake advisory session permissions for current server authorization.

## Options

- Enable production administration to exercise UI against live mutations: violates atomic activation.
- Wait for cutover before implementing any interactions: leaves the UI contracts unqualified.
- Implement actual SPA workflows with explicit unavailable states, qualify interactions with binary API contract fixtures and preserve separate real-server security scenarios (chosen).

## Decision

### Independent pages and affordances

Add Users, Roles and Bindings sections behind one lazy management entry with reloadable collection, creation and ID-detail routes. Pages assemble features; entity adapters own generated-client calls through the existing binary/CSRF transport. Reuse semantic theme tokens, accessible fields, table, loading and confirmation primitives. Navigation and each action use their individual advisory permission; list-only does not imply Get, create-only does not probe a list or catalog and direct Get does not read a list. Server admission remains authoritative for every operation. These session hints are not evidence that a conditional/scoped grant currently matches. Capability discovery and resource-aware navigation remain cutover obligations.

User creation establishes grantless membership. Account enable/disable requires users.disable, setup reissue requires users.update and an authorized active passwordless detail; self status controls are absent. A successful account state change clears displayed setup delivery and suppresses stale reissue controls. Narrow creation receipts display their returned IDs and policy versions so create-only callers can use the new resources without borrowing list/Get authority. Fresh tokens live only in the current route's signals, with private-delivery guidance, expiry and explicit dismissal. Never put them in browser storage, URLs, logs, navigation state or list/detail caches. Password-setup consumption remains pending per ADR-0069; do not fabricate a working setup URL.

Role forms edit name and the complete permission bundle, showing an optional server catalog only with roles.list. This preserves create-only workflows without inventing authority or hardcoding the catalog. System roles have no edit/delete controls; live binding references disable deletion. The server still enforces immutability, complete old/new delegation, references and recovery protection.

Binding forms accept an existing user's ID, role ID, organization/exact-connection scope and explicit condition choice. Use IDs visible through independently authorized lists rather than querying unauthorized pickers or assuming a connection union authorizes organization administration. Lists show IDs, scope, version and conditional/revoked state without fetching protected condition source. Get displays stored title, description, expression and contract versions verbatim as text. Self Binding edits/revocations are absent. Unsupported condition contracts are inspection/revocation only; an older form cannot rewrite them.

### Condition choices and mutation semantics

Generate conjunctions from an inclusive start, exclusive expiry, supported resource type and/or exact canonical resource UUID. Require a title and at least one restriction for conditional input. Require explicit-zone ISO times, reject invalid calendar dates and reversed windows, and normalize valid timestamps to UTC. Resource vocabulary matches the versioned server environment. The browser performs no authorization evaluation; generated conditions undergo ValidateCondition and the mutation's own server preparation. Invalid local choices or server validation never fall through to an unconditional write.

A new grant explicitly offers no condition or form restrictions. Editing defaults to preserving source and never tries to reverse-parse arbitrary existing CEL into fields. Replacement sends a complete generated contract; removal sends explicit true only after a modal warning that the grant broadens. Account disabling/enabling, role deletion and Binding revocation also require confirmation. Explain that grants are additive and a conditional grant cannot narrow another broad grant. Server delegation and last-recovery-administrator checks remain mandatory, including concurrent Role/Binding removal.

Submit expected versions from the displayed resource; do not silently refresh/retry stale writes. Successful policy edits lock their submitted form until an explicit reload. Conflicts and uncertain outcomes lock replay and explain reloading; generic errors never echo CEL, profile or driver diagnostics. Failed preconditions explain recovery/system-role/reference/account constraints without claiming success. Authorization refusals clear protected data/drafts and re-resolve the session.

### Bounded data and request ownership

Use numbered 10/20/50/100 lists with default 20, exact bigint ranges/counts, stable server ordering and page/size/revision in the URL. A continuation sends its read revision, reauthorizes on the server and returns no rows/count on conflict; Refresh explicitly starts at page one. Page size changes reset continuation. Loading is distinct from empty and error. Do not fetch all pages for pickers or totals.

Own reads, mutation feedback, drafts and setup receipts within a route/identity/permission lifetime. Unauthenticated reads re-resolve the session rather than retaining a revoked principal. Latest page wins; reset/cleanup fences outstanding responses and releases loading immediately. Identity or advisory permission changes remount the workspace; route/ID changes discard its data and tokens. A preflight condition validation must check its captured lifetime before issuing the subsequent mutation. Closing a confirmation fences its own callbacks without pretending an already issued mutation was canceled. No automatic write retries or local persistent drafts.

### Production and qualification boundary

Keep initial static dependencies in the Vite/Rolldown entry group while authenticated management forms remain dynamically loaded; retain the existing single-entry-script login regression scenario.

Keep all production API interceptors, catalog seeds, runtime grants, migrations and authority readers unchanged. Existing Users/Roles session hints can expose navigation, but their new API calls still return the mandatory post-authentication/CSRF Unavailable. Binding permission hints remain unseeded. Direct links never bypass server admission. This UI adds no partial activation switch, bootstrap backfill, session-capability conversion or governance/output cutover and does not complete M1.

Observe behavioral red for absent routes and stale/denied state plus invalid conditional inputs, then green. Deterministic unit scenarios qualify response ordering, identity disposal, validation/write fencing, receipt disposal, stale/uncertain replay refusal and generic errors. Browser scenarios run the actual embedded SPA with generated binary API fixtures for interaction qualification: ordinary create/read/edit/revoke journeys, independent capabilities, setup dismissal, source preservation, removal confirmation, pagination conflict, unsupported/system resources and recovery-error feedback. Fixtures do not prove authorization, transaction correctness or successful production activation. Existing real TLS/PostgreSQL/CEL API scenarios retain that evidence; the real binary browser scenario independently verifies inactive pages, sessions/CSRF and unchanged Auth.Me authority. Require full make verify, supply-chain and staged review before commit.

## Consequences

The prepared management workflow is reviewable while production maintains a single authority. Atomic activation still needs migrated bootstrap/login/setup completion, trusted capability discovery and every governance/collection/output reader/writer qualified together. UI contract fixtures must not be reported as end-to-end production mutation acceptance. The existing bilingual PRD product requirements remain unchanged.

## Primary sources (verified 2026-10-18)

- [Vite build customization](https://vite.dev/guide/build) and [Rolldown code splitting groups](https://rolldown.rs/reference/TypeAlias.CodeSplittingGroup): group the initial dependency graph without eagerly including dynamic workflows.
- [Solid onCleanup](https://docs.solidjs.com/reference/lifecycle/on-cleanup) and [createEffect](https://docs.solidjs.com/reference/basic-reactivity/create-effect): scope ownership and reactive cleanup for request lifetimes.
- [OWASP authorization guidance](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html): deny by default and authorize every server operation.
- [Google IAM Conditions](https://docs.cloud.google.com/iam/docs/conditions-overview): conditions belong to grants and cannot narrow an independently unconditional grant.
- [Google conditional binding management](https://docs.cloud.google.com/iam/docs/managing-conditional-role-bindings): preserve versioned source when editing conditional grants.
