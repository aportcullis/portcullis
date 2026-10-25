# ADR-0091: Independent execution and stop actions

- **Status:** Accepted
- **Date:** 2026-10-25
- **Refines:** [ADR-0018](0018-access-requests-and-approvals.md)'s request actions and [ADR-0021](0021-governed-query-execution.md)'s governed cancellation.

## Context

The Execute response can remain pending while the server has already admitted the request and a refreshed list shows it executing. A shared mutation guard then blocks Stop until Execute completes, preventing the requester from stopping the work through that control. Refreshing list rows must not discard an outstanding action or its feedback.

## Decision

The shared row-action control group owns two action slots per request: one mutually exclusive primary mutation slot and one independent execution-stop slot. Stop calls the existing governed Cancel RPC when the displayed server state and current authority permit it, even while Execute remains pending. Each slot prevents its own concurrent duplicate; this does not permit a second execution or weaken server admission.

Retain each slot's pending state, attempt identity and error across replacement list rows within the same authorized request lifetime. Capture the request ID before asynchronous work. Apply completions only to that request and the still-current route, authenticated identity and authorization snapshot. Route departure, identity or authority changes and protected-state disposal fence obsolete completions; a refresh alone does not become a new action lifetime.

Keep Execute and Stop feedback independent. Starting or completing one slot must not erase the other slot's error or release its pending guard. Refresh server-owned state after an applicable completion without automatically replaying either mutation.

Validate decoded request lists and details before retaining them in application state, including the requester required by ownership decisions. Loaded views consume that required model; an unloaded detail view has an explicit state rather than an empty request. Invalid responses fail the read with safe feedback and remain retryable. Destructure response snapshots and stable controllers within their owning callbacks; preserve reactive prop reads inside accessors or tracked computations.

Stopping a browser fetch with AbortController and requesting governed cancellation are separate operations. Neither a closed connection nor a successful cancellation request proves target rollback or a terminal outcome. Preserve the server's confirmed cancellation, failure and outcome_unknown semantics, durable evidence and single-use execution; never rerun SQL to recover a lost response.

## Consequences

Qualify a held Execute response followed by an executing-state refresh and an admitted Stop call, independent failures and pending guards, repeated row replacement, duplicate attempts and stale route/identity/authority completions. Retain real-server cancellation and single-execution scenarios alongside browser interaction coverage. Complete functional/security gates, running-application QA and staged review before the focused commit. This repairs the existing cancellation contract without changing PRD scope or introducing another execution endpoint.

## Sources (verified 2026-10-25)

- [Solid createSignal](https://docs.solidjs.com/reference/basic-reactivity/create-signal).
- [Solid props and reactivity](https://docs.solidjs.com/concepts/components/props).
- [Zod schemas](https://zod.dev/api).
- [MDN AbortController](https://developer.mozilla.org/en-US/docs/Web/API/AbortController).
