# ADR-0024: WebMCP query assistance first in Bridge (M2)
- **Status:** Accepted — ordering and DB scope amended by [ADR-0025](0025-sql-database-first-expansion.md) (roadmap scope; implementation follows the M1 release gate)
- **Date:** 2026-10-03

> **Sequence amendment:** [ADR-0026](0026-review-tools-before-agent-integration.md) advances M2 SQL review/EXPLAIN and M3 schema preview, defers WebMCP to M6 after M5, and removes WebMCP from MVP acceptance. Earlier sequence text below is historical.

## Context

The product owner requests Web MCP query assistance in the next milestone. Bridge (M2) currently covers MySQL/SQLite parity; agent integration was a Later candidate. Promote browser WebMCP query assistance into Bridge, before adapter expansion, while retaining milestone identifiers and the existing alpha/MVP release boundaries. The initial interpretation of "Web MCP" is browser WebMCP; a remote HTTP MCP service is a separate scope decision.

The [Chrome WebMCP guide](https://developer.chrome.com/docs/ai/webmcp), [imperative API](https://developer.chrome.com/docs/ai/webmcp/imperative-api), [tool security guidance](https://developer.chrome.com/docs/ai/webmcp/secure-tools), and [community-group draft](https://webmachinelearning.github.io/webmcp/) were verified on 2026-10-03. Current documentation exposes tools through `document.modelContext`; WebMCP remains a proposed browser API with an origin trial and local testing flag. Reverify the API and browser support at implementation time. It differs from [HTTP MCP transport](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports) and does not by itself provide remote/headless client connectivity.

## Options

- Leave all agent integration in Later: conflicts with the requested next-milestone priority.
- Build an HTTP MCP gateway first: adds remote authentication and deployment scope without establishing the requested browser workflow.
- Add browser WebMCP to Bridge first: reuse the visible, authenticated query workflow; extend the same contracts to additional database adapters afterward.

## Decision

Bridge has two ordered tracks: browser WebMCP assistance over the PostgreSQL governance loop, then the existing MySQL/SQLite parity work. Gate (M1) must still pass before Bridge starts. Library (M3), including Bridge, remains the MVP boundary; a roadmap entry is not a shipped feature.

The first complete journey is connection discovery → SQL/typed-parameter composition → explicit draft save/submit → approval-state inspection → requester-only approved execution → bounded result-page inspection. Start with read queries. Schema discovery is useful to composition but requires a bounded, authorized catalog use case and audited adapter contract before exposure; it must not become an unrestricted execute-SQL tool. Saved-query discovery/reuse follows the Library model when saved assets exist.

Use an imperative browser adapter behind a small consumer-owned tool-registry port. The application composition root injects registration and existing feature/use-case actions. Shared browser infrastructure imports no feature/domain store. Tools call existing authorized Connect APIs rather than SQL drivers or a second execution path. Each state-changing tool is a separate action with explicit inputs and a visible page outcome; filling SQL does not automatically save, submit or execute. Keep ordinary interactions inline and reserve focused confirmation for consequential actions.

Tools operate under the current authenticated user and organization. Preserve backend permissions, CSRF, ownership, distinct-reviewer/quorum rules, pinned payload/config/policy checks, cancellation, single-use execution, result TTL/quotas and exact-value handling. Do not expose automatic approval/rejection tools in the initial scope. Execution requires the user's explicit requested action and an approved request; tool annotations are hints, never authorization. Unregister tools and fence pending callbacks on logout, principal/permission changes and route teardown. Do not export UI cookies, target credentials or encryption keys.

Audit attributes the actual user/org/request/execution. Browser-provided agent/source labels are untrusted context, not authenticated agent identity. Treat SQL comments, catalog descriptions and result cells as untrusted data; provide bounded structured outputs and appropriate tool hints. Result disclosure is limited to the user's explicit tool request and current access. Portcullis does not silently introduce an external model provider.

Feature-detect native support and preserve the ordinary web workflow without it. Pin implementation-time browser/API evidence and test registration, schemas and lifecycle through the port, then verify the complete journey in an actual supported browser. Do not claim native compatibility based only on a fake registry. A remote HTTP MCP gateway, separate machine identity, OAuth delegation and headless clients remain Later pending their own ADR and acceptance gates.

## Consequences

Amend both PRD translations, the named roadmap and its actual SVG. M2 acceptance now includes agent-assisted query usability, denied/revoked access, cross-user isolation, replay refusal, cancellation, bounded results and unsupported-browser fallback, plus the existing three-DB contract/security matrix. Capture a real WebMCP walkthrough when implemented. No WebMCP runtime, dependency, external endpoint or supported-client claim is introduced by this planning change.
