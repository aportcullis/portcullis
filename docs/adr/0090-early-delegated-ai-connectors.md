# ADR-0090: Initial delegated AI connectors in M2

- **Status:** Accepted
- **Date:** 2026-10-25
- **Partly supersedes:** ADR-0024/0026/0027/0028/0035 for initial connector timing and the associated protection/registration prerequisites.

## Context

External ChatGPT and Claude clients should participate in governed Portcullis workflows during M2. Deferring all agent connectivity until M6 delays this use without improving the existing human workflow. Advancing connectivity must advance its security prerequisites and make the accountable person visible.

## Decision

Keep M2's MySQL parity → Kubernetes/CNPG → SQL review/EXPLAIN sequence, then qualify the initial ChatGPT and Claude connectors. Within that connector slice, establish default-deny protected output, authenticated registration and explicit expiring human delegation before exposing tools. M4 retains broader cross-surface masking; M6 retains provider, additional-client and transport expansion, including local stdio and optional browser WebMCP. Initial connectors are M2 acceptance, not M1 functionality.

Use the existing governed application ports. Effective permissions intersect the delegating person's current authority, verified registration/grant, organization/connection/tool scope and disclosure policy. Preserve explicit execution intent, distinct human reviewers, no self-approval, quorum, immutable payloads and single-use execution. Revoke or expire delegation without admitting later calls or stale output; never replay SQL to recover an interrupted response.

Validate the selected HTTP authorization path, token issuer/audience/expiry/scope and active delegation for every protected request. Do not forward browser sessions or incoming client tokens to downstream services, trust client names as identity, disclose credentials/raw sensitive output or grant authority merely by registering a client. Keep unknown or unprotected fields withheld. Exact protocol, provider and client-registration choices need an implementation ADR and real compatibility evidence.

Display the AI/client avatar with the delegating person's smaller avatar over its lower-right corner. Hover, keyboard focus and touch reveal both identities and connection state. Authorized audit views attribute each mediated action to the verified client/registration and stable human identity; images, names and claimed model labels are presentation rather than authentication evidence. Different clients acting for one person never become distinct quorum participants.

## Consequences

Amend both PRDs, M2/M4/M6 scopes, the support matrix and roadmap together. M2 must qualify real ChatGPT and Claude client versions, authorization, protected output, spoofed/cross-org/revoked/expired access, pending-response fencing, attribution and unchanged governance before claiming support. This decision changes planned scope; it implements no connector and makes no current compatibility claim. The earlier ADRs' remaining security and deployment decisions stay in force.

## Sources (verified 2026-10-25)

- [MCP authorization](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization).
- [Authorization security considerations](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization/security-considerations).
- [MCP security best practices](https://modelcontextprotocol.io/docs/2026-07-28/tutorials/security/security_best_practices).

MCP guidance informs the authorization boundary; client compatibility and the dual-identity interface remain Portcullis acceptance requirements.
