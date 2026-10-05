# ADR-0027: Sensitive-data masking before agent registration and integration

- **Status:** Accepted
- **Date:** 2026-10-03

> **Gateway amendment:** [ADR-0028](0028-agent-neutral-mcp-gateway.md) promotes local/remote standard MCP Gateway to gated M6; browser WebMCP is an optional follow-up. Masking and registration prerequisites remain binding. Earlier remote/headless Later placement is superseded.

## Context

The product owner requires sensitive-data protection first, followed by registering agents before connecting them. Existing M1 SQL literal/comment redaction and encrypted snapshots do not mask query result cells, identifiers, plan contents or arbitrary tool output.
ADR-0026 defers browser WebMCP to M6; this decision adds mandatory prerequisites without advancing it or changing the PostgreSQL/MySQL MVP boundary.

Verified primary guidance: [OWASP AI Agent Security](https://cheatsheetseries.owasp.org/cheatsheets/AI_Agent_Security_Cheat_Sheet.html) calls for scoped tools, data protection and output validation.
[MCP authorization](https://modelcontextprotocol.io/specification/draft/basic/authorization) and [security practices](https://modelcontextprotocol.io/docs/draft/tutorials/security/security_best_practices) inform future authenticated integration; they do not authenticate native WebMCP clients automatically. Reverify draft protocols when implementing the transport.

## Decision

Deliver in this order:

1. **M4 Watch:** organization/connection-scoped sensitive-data policies and server-side masking/withholding for governed output. Prove the policy boundary independently of any agent.
2. **M6 Reach, after M5:** agent registration and authenticated grants, only after masking acceptance passes.
3. **M6 Reach:** enable registered-agent integration only after registration/revocation and protected-output scenarios pass. Remote/headless Gateway remains Later under a separate transport ADR.

### Masking boundary

Admin-managed, versioned rules designate sensitive fields and permitted recipients. Begin with complete value redaction/field omission; optional partial display needs explicit policy and separate acceptance. Enforce decisions in application-owned output ports before serialization, with adapters implementing transformation.
Do not rely on CSS, frontend-only masking, model instructions or automatic PII detection as authorization.

Apply the same disclosure decision to result APIs, full-cell views, CSV, SQL/catalog/plan/review metadata and future tool payloads. Preserve original approved SQL/parameters and target execution semantics; masking governs disclosure, not the approved query. Keep originals encrypted under existing retention and owner rules.
A raw-view exception, if supported for a human, needs a separate explicit permission and audit; agents receive no raw-data bypass initially. Ordinary requester ownership does not bypass a masking rule.

For agent outputs, default deny: release only explicitly allowed fields after protection. Secrets, credentials, raw SQL/parameters and raw sensitive cells are never released. Unknown lineage, aliases/expressions, computed/free-text/JSON content, unsupported encodings and masking errors are withheld or cause a sanitized refusal; a column-name regex is insufficient.
Detection can assist rule setup but cannot certify an arbitrary result as safe. Recheck current policy at every read/export/tool response; policy changes invalidate prepared exports and stale transformed caches. Version and audit policy decisions without copying sensitive values. Masked sorting/filtering/search must not provide a raw-value oracle; restrict unsupported operations.

Acceptance must cover human UI/API/CSV parity, full-cell paths, plan literals, catalog/comment/error text, aliases/derived/JSON data, unknowns, policy changes/cache reuse, org isolation, raw-view denial, masking failure and leak-free audit/logging. Use synthetic canary secrets and actual serialization/export boundaries against both committed DBs. No protection claim precedes these tests.

### Agent registration boundary

Org admins create a registration with stable ID, name/description, accountable owner, integration kind, disabled/pending/active/revoked state, allowed tools/connections, protected-output policy and expiring user delegation. Register first with no active grants; activation requires an explicit authorized grant. Record registration/change/activation/revocation/use as audit metadata without secrets.
The registration is a local trust record, not an executable plugin install or an arbitrary outbound URL fetch.

Effective permission is the intersection of authenticated user permissions, current agent grant, org/connection policy and masking policy. Attribute both authenticated user and verified registration. A supplied agent name/ID/source label proves neither identity nor permission.
Disable/revoke/expiry/user logout fences pending calls and prevents subsequent responses; no automatic approval or rejection tool, no changed quorum, no execution retry or raw result access.

Before exposing protected tools to native WebMCP, a transport-specific ADR must establish how the caller is bound to an active registration and delegation. If the browser API cannot prove that binding, refuse protected registration-specific capabilities rather than treating a client label as authentication.
Do not borrow the human cookie as agent identity or promise control over an external agent's independent browser/DOM access. HTTP MCP must use its own validated authorization, never browser session cookie/token passthrough; that transport remains Later.

Registration acceptance includes forged IDs, cross-org access, expired/revoked grants, privilege intersection, pending-response fencing and masked outputs. Connection status/testing is page-based and returns sanitized diagnostics. Existing explicit execution intent, distinct human review, immutable payload and single-use lease remain mandatory.

## Consequences

Update both PRD translations, feature support matrix and public roadmap/SVG: masking → registration/grants → integration. This extends M4/M6 planned scope; it does not ship masking, registration or MCP. Preserve existing SQL audit redaction as a separate verified capability.
