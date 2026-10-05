# ADR-0028: Agent-neutral MCP Gateway and infrastructure boundary

- **Status:** Accepted
- **Date:** 2026-10-03

## Context

The product owner wants local agents, Claude Code, Codex and other clients to connect freely through a gateway, with infrastructure integration and Kubernetes deployment. Browser-only WebMCP would unnecessarily limit this direction. Preserve ADR-0026's M6 timing and ADR-0027's masking → registration/grants → integration prerequisites.

Verified primary sources on 2026-10-03:
- [MCP transports, 2026-07-28](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports)
- [HTTP authorization](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization)
- [Kubernetes NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
- [Secrets](https://kubernetes.io/docs/concepts/configuration/secret/).
MCP defines stdio and Streamable HTTP; protocol versions and authorization discovery require implementation-time verification. NetworkPolicy needs an enforcing network plugin; a Secret object alone is not an encrypted secret-management guarantee.

## Decision

Make **M6's primary agent integration a self-hosted, agent-neutral MCP Gateway**. Promote the previously Later remote/headless integration into this gated track; browser WebMCP becomes an optional subsequent adapter, with no requirement for shipping the initial gateway. Keep MCP outside MVP. Do not build an agent runner, model host, arbitrary MCP proxy or vendor-specific tool API.

Provide two client paths over the same application use cases:

| Path | Intended use | Boundary |
| --- | --- | --- |
| Streamable HTTP | Local clients with HTTP support, remote/team agents, Kubernetes | Opt-in MCP endpoint; validated authorization and current registration/delegation on every call |
| Local stdio bridge | Local clients requiring subprocess/stdin/stdout integration | Narrow subcommand/bridge to the same authenticated Gateway; no direct target DB or metadata access, no approval/policy bypass |

Preserve the single-Go-binary default: the MCP endpoint is an optional inbound adapter; the local bridge can be a subcommand in the same binary. Deployment may use the same artifact in a dedicated gateway process without duplicating business rules. Wire consumer-owned ports in `cmd/portcullis`. Do not embed vendor SDKs to implement shared governance.

Gateway-owned responsibilities: authenticated principal/registered-client binding, expiring delegation, org/connection/tool scopes, masking and bounded output, explicit user intent, distinct human approval, immutable payload, single-use lease, cancellation/outcome handling, rate admission and secret-free audit.
Registration is not protocol dynamic client registration and grants no authority by itself. Existing-user permissions and agent grants intersect; no unauthenticated local exception, no user cookie/token passthrough, no target credentials in tools. Sensitive raw user-input SQL/parameters may enter an authorized draft use case but must never be echoed into agent outputs or logs.

HTTP authorization must validate issuer, audience/resource, expiry and scope and bind the authenticated client/delegation to an active organization registration. Use standards-based protected-resource/authorization-server discovery and a compatible OAuth provider; do not replace this with trusted client names or unrestricted proxy headers. The provider may be separately deployed.
No gateway activation until the selected provider/client authorization scenarios pass; clients without the selected secure authorization path remain unsupported. The stdio bridge keeps stdout protocol-only, stderr sanitized, stores no credentials in repo/client example text, and forwards only its scoped credential through the authenticated HTTP path.
Local HTTP binds loopback by default; deployed HTTP needs configured TLS/origin controls. Reverify and test supported protocol revisions instead of assuming every client supports the newest revision.

Infrastructure owns TLS/certificates, ingress or Gateway API routing, identity-provider deployment, Secret delivery/rotation, network policy, observability plumbing and backup topology. Portcullis still validates identity and business authorization: an ingress check cannot replace app permission/masking enforcement.
Publish Helm and Kustomize examples for the opt-in endpoint, discovery routing, Secret references, probes, resource limits and enforcing ingress/egress policy. Account for auth discovery outside the MCP path, request timeouts, proxy streaming/buffering and graceful shutdown.
Do not claim HA until shared state, revocation, output bounds and one-time leases pass multi-replica tests; registration and leases must not depend on one pod's memory.

Compatibility targets are local generic MCP clients, Claude Code and Codex, without vendor exclusivity. Publish a versioned matrix of client version, negotiated MCP revision, HTTP/stdio path, auth, tool journey and revoke/cancel behavior. These are intended targets, not current support claims.
Exercise the real request → human review → single-use execution → protected results workflow, stale/revoked grants, cross-org access, spoofed identity, leak canaries, disconnect/cancel behavior and repeat-execution refusal. Kubernetes acceptance includes routing/discovery, ingress body/stream limits, secret rotation, restart/shutdown and absence of direct policy-bypass paths.

## Consequences

Amend both PRD translations and support/roadmap docs: masking first; registration second; standard Gateway third; browser WebMCP optional afterward. This supersedes the remote/headless Later placement in ADR-0024/0026/0027 while retaining their security boundaries. Helm/Kustomize are planned deployment deliverables, not generated or validated charts yet.
Detailed auth-provider selection and protocol implementation receive their own ADRs before code.
