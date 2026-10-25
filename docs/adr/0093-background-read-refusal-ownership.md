# ADR-0093: Background and execution refusal ownership

- **Status:** Accepted
- **Date:** 2026-10-25
- **Refines:** [ADR-0075](0075-local-draft-and-setup-receipt-lifetimes.md)'s interaction ownership and [ADR-0091](0091-independent-execution-stop-actions.md)'s execution action lifetimes while preserving [ADR-0071](0071-binding-authorization-activation.md)'s server authorization and output fences.

## Context

The application shell owns an automatic pending-request count independently of a route's creation form. A protected read can be refused after an organization revision changes, including when the caller's current grants remain valid. Broadcasting that background refusal as a refusal of every mounted interaction destroys an unrelated creation receipt even when its mutation succeeded. Treating each successful Me response object as a new count owner also causes a refused count to issue immediately again after metadata refresh.

An Execute or Stop call can also finish after its action owner has been disposed, or after a successful same-authority Me has resolved a newer session object. Its delayed authentication or permission refusal must not discard a later route's creation state or re-resolve the newer session. Existing action settlement and server-side mutation outcomes remain separate from notification ownership.

## Decision

Attach a tagged, caller-owned purpose to the automatic pending-count RPC through Connect context values. Unmarked calls retain interaction ownership. The purpose selects local disposal behavior and grants no permission, resource access or exemption from server checks.

Mark RequestRowActions Execute and Stop calls as owned interactions using their existing action-attempt and disposed-registry predicate. Capture the session object at each RPC dispatch as an additional refusal-notification fence. A current owned refusal retains ordinary synchronous interaction or session disposal. A stale owned refusal still rejects its own call but cannot notify the current interaction. The additional session-object comparison governs notification only; it does not change successful mutation settlement, existing action feedback, refresh guards or retry policy.

A current background permission refusal immediately clears the protected count and still asks the application session coordinator to resolve Me. It does not itself refuse an unrelated locally composed creation interaction. A changed authenticated identity, authorization hints, sign-in generation or session-metadata failure still disposes that interaction through its existing lifetime. Current interaction permission refusals, including protected catalog reads, retain immediate disposal; existence-hiding NotFound handling remains unchanged. Session loss from any current call retains immediate session and interaction disposal.

Validate the consumed pending total with the existing access-request count schema before retaining it. Keep unavailable count state distinct from a validated zero. Invalid data removes the protected count; an ordinary retrieval outage retains the last valid count under its existing ownership fence.

Capture the current session object separately when each background read starts, together with the polling owner's authorization key, and check both before notifying session coordination or applying its result. A successful same-key Me can follow cookie replacement in another tab; it must fence an older cookie's delayed refusal without immediately reissuing the query. The next explicit or scheduled read captures the fresh session object. A response from a disposed or replaced owner still rejects its own RPC but cannot refuse a later interaction. This boundary does not cancel a server mutation or infer its outcome.

Own count polling by a memoized authorization key, with explicit effect dependencies and untracked reads of the corresponding principal and capabilities. A successful Me with the same key neither replaces that owner nor immediately reissues the count. A changed identity, revision or authority hint replaces it and clears the count. Existing scheduled, online and visibility refresh triggers remain bounded by the in-flight guard; no extra refusal latch, retry loop or automatic mutation replay is introduced.

Protected lists, details, results and server-derived edits retain their existing authorization/revision fences. A preserved local receipt does not authorize reopening protected metadata. Backend policy revision checks, session revocation, result/CSV output checks and uncertain mutation outcomes remain authoritative.

## Consequences

The pending-count query is the only caller marked as a background read. Background refusal can temporarily leave a count absent while local composition stays visible and current authority is resolved. Transport failures retain the existing count behavior, while authentication or permission refusal clears it. Expanding this ownership distinction to another background caller requires identifying its protected state and disposal boundary explicitly.

Owned interaction notification fencing is applied only to RequestRowActions Execute and Stop. Submit, unexecuted-request Cancel and entity-store mutations retain their unmarked behavior; their notification ownership remains a separate follow-up. This decision does not establish global isolation from every obsolete RPC refusal.

Qualify successful count updates, overlapping triggers, ordinary outages, current permission/session refusals, direct interaction refusal, disposed responses, changed authority, same-key Me followed by an obsolete refusal, persistent refusal without immediate Me-triggered replay, and later explicit/scheduled refresh. Browser scenarios hold the background response while a Role receipt is created, then verify preservation only for unchanged authority, removal on session/authority loss or failed Me, non-resurrection after recovery and continued disposal on a direct protected-read refusal. Run the full repository gates before committing.

Qualify both Execute and Stop with held 401 and 403 responses after route departure and after a fresh same-authority Me. Preserve later-route composition, existing action feedback and successful settlement, refuse extra session re-resolution from stale notifications, and verify no mutation replay. Retain current owned and unmarked refusal disposal and the independent Execute/Stop scenario suite.

## Sources

- [Connect Web interceptors and context values](https://connectrpc.com/docs/web/interceptors/), verified 2026-10-25: typed call context travels through the request/response cycle and can select interceptor behavior without changing the wire authorization contract.
- [Solid on](https://docs.solidjs.com/reference/reactive-utilities/on-util), verified 2026-10-25: explicit dependencies leave callback reads untracked.
- [Solid createMemo](https://docs.solidjs.com/reference/basic-reactivity/create-memo), verified 2026-10-25: an unchanged derived value suppresses downstream updates through equality comparison.
