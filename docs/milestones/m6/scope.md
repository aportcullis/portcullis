# M6 · Reach

**Not started — operating and integrating Portcullis after Forge.**

Build on the M2 Kubernetes/CNPG deployment baseline and provide Terraform/OpenTofu integration after the API is stable. Keep metadata, keys, upgrades, and recovery manageable for self-hosted operators.

**Deferred integration track:** After M5 and the M4 masking gate, provide organization-owned agent registration and expiring scoped grants, then a standard MCP Gateway for local clients, Claude Code, Codex and other compatible clients. Use Streamable HTTP plus a local stdio bridge over the same governed application use cases.
Publish a real client/version/protocol/auth compatibility matrix before support claims. Browser WebMCP is an optional follow-up, not the initial Gateway prerequisite. MCP remains outside MVP.

**Infrastructure boundary:** Portcullis owns identity validation, masking, authorization, human approval, single-use execution and audit. Operators may supply ingress/TLS, a compatible authorization provider, Secret rotation, network restrictions and observability.
Provide Helm/Kustomize deployment examples with discovery routing and proxy limits; do not build an agent runner or assume Kubernetes infrastructure replaces app policy enforcement. See [Gateway direction](../../adr/0028-agent-neutral-mcp-gateway.md) and [PRD §4.13](../../product/prd.en.md#413-agent-neutral-mcp-gateway-and-deployment-m6).

**To advance:** Establish API stability and the operational contracts for deployment, backup, upgrades, and accepted result-cache loss before promising provider compatibility. Deployment options do not by themselves imply high availability.
