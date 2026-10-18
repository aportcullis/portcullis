# ADR-0075: Local draft and setup receipt lifetimes

- **Status:** Accepted
- **Date:** 2026-10-19
- **Partly supersedes:** [ADR-0070](0070-prepared-administration-ui.md) and [ADR-0071](0071-binding-authorization-activation.md) for SPA form and setup-receipt disposal on organization revision changes only.
- **Preserves:** [ADR-0074](0074-session-capability-discovery-and-recovery.md)'s metadata failure handling and all server authorization, revocation and output fences.

## Context

An organization revision advances when an administrator changes another account or grant. The SPA currently uses that revision both to invalidate protected reads and to destroy locally owned forms. Creating a user can therefore destroy its only setup delivery on the next Me refresh, even though the administrator's authenticated identity and advisory authority are unchanged. A successful Me does not prove that a previously read resource remains authorized: scoped conditions and resource state are checked separately on the server.

## Decision

Separate two lifetimes without introducing a second authorization decision. Protected lists, details, results, server-derived edit forms and their outstanding responses retain the existing identity/revision/capability fence. A new revision clears those reads and editor state; it never licenses retaining protected metadata because navigation capabilities happen to be unchanged.

Locally composed creation inputs and narrow mutation receipts use a separate authenticated interaction lifetime. Compare the user ID, the full compatibility permission set, evaluated capability set, obsolete role label and complete caller-owned Binding summaries, treating set ordering as irrelevant. An organization revision alone does not change this lifetime. These hints do not prove policy equivalence and never authorize an RPC or resource read. Current affected-session revocation and per-request authorization remain authoritative, including policy changes that do not alter navigation capabilities.

Keep creation inputs in route-owned signals, including user/Role/Binding, connection and new-request composition. Refresh the new request's protected target choices on revision changes independently of its authored SQL and narrative. Isolate the Role form's optional permission catalog under its own revision-owned subtree so neither loaded catalog keys nor delayed catalog responses survive a changed snapshot. Keep a setup reissue receipt in the management route's interaction lifetime, outside the revision-owned user detail subtree. Account state mutations, explicit dismissal and route departure still remove setup delivery. Existing-resource edit drafts remain conservative: a revision change discards them with their protected source; preserving those drafts would require a separate resource-aware reconciliation design.

Any user or advisory-authority change, loss of an authenticated session, session-metadata retrieval failure (including authorization or transport outages), confirmed sign-out, successful new sign-in, or protected-operation admission refusal (including an existence-hiding NotFound) ends the interaction lifetime and fences its outstanding responses. Clear the refused local state synchronously so a later Me response cannot recover the previous interaction. Recovery cannot resurrect discarded inputs or tokens, even for the same account. Route/ID changes also discard local state. A metadata refresh that succeeds for the same interaction keeps local state; it does not replay writes, validation or token issuance. Do not persist drafts or receipts in storage, navigation state, logs or URLs, other than the explicitly delivered setup URL shown inside the volatile receipt.

Condition preparation captures the current authorization snapshot in addition to its interaction lifetime. A revision change during ValidateCondition prevents the subsequent Binding write, even if the authored choices remain on screen. Already issued mutations retain their existing uncertain-outcome and optimistic-version rules; do not infer rollback or retry them automatically.

## Consequences

The browser retains no additional server policy source and no persistent drafts. Revision-owned reads can temporarily show loading while locally produced delivery stays visible. Server-derived edits are still discarded on an unrelated organization revision; this deliberate conservative boundary does not promise seamless editing of existing resources. A refused interaction shows an explicit reload message rather than remounting and automatically issuing the same refused read again. Successful new sign-in advances an in-memory interaction generation even for the same account, so an old authentication lifetime cannot survive a replacement login.

## Qualification

Observe browser failures for creation drafts and setup delivery across successful changed-revision Me responses before implementation. Qualify ordinary same-snapshot refresh, changed-revision creation inputs, creation and reissue receipts, request/connection composition, explicit dismissal and protected-read invalidation. Refusal scenarios cover changed identity, capabilities, compatibility permissions and Binding summaries, metadata/transport failure and recovery, route departure and delayed responses. Test preflight revision changes without issuing a write. Binary API fixtures establish SPA lifetime behavior only; retain the real PostgreSQL/CEL session-revocation and protected-output scenarios in the full repository gates. Require make verify, supply-chain and staged review before committing.

## Sources

- [Solid Show](https://docs.solidjs.com/reference/components/show), verified 2026-10-19: a keyed truthy value change recreates the child block.
- [Solid createMemo](https://docs.solidjs.com/reference/basic-reactivity/create-memo), verified 2026-10-19: stable derived values prevent unnecessary downstream updates.
- [OWASP Authorization Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html), verified 2026-10-19: enforce authorization server-side on every request and fail safely.
