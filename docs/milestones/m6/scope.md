# M6 · Reach

**Not started — operating and integrating Portcullis after Forge.**

Build on the M2 Kubernetes/CNPG deployment baseline and provide Terraform/OpenTofu integration after the API is stable. Keep metadata, keys, upgrades, and recovery manageable for self-hosted operators.

**Expanded integration track:** Initial external ChatGPT/Claude connectors, verified registration and explicit expiring human delegation belong to M2 after its database, deployment and SQL-review gates. After M5 and the broader M4 masking gate, expand compatible providers and clients, including local clients, Claude Code and Codex, and add a local stdio bridge over the governed Gateway use cases. Browser WebMCP remains an optional subsequent adapter with its own identity and browser qualification.
Extend the real client/version/protocol/auth compatibility matrix before each support claim. Reuse current human Binding authority, verified agent grants, protected output, revocation/expiry fencing, distinct human review, immutable payloads, single-use execution and dual-actor audit. Wider transports and clients do not receive a local or provider-specific authorization bypass.

**Infrastructure boundary:** Portcullis owns identity validation, masking, authorization, human approval, single-use execution and audit. Operators may supply ingress/TLS, a compatible authorization provider, Secret rotation, network restrictions and observability.
Provide Helm/Kustomize deployment examples for the expanded integration paths with discovery routing and proxy limits; do not build an agent runner or assume Kubernetes infrastructure replaces app policy enforcement. See the [initial connector sequence](../../adr/0090-early-delegated-ai-connectors.md), [Gateway direction](../../adr/0028-agent-neutral-mcp-gateway.md) and [PRD §4.13](../../product/prd.en.md#413-agent-neutral-mcp-gateway-and-deployment-m2--m6).

**To advance:** Establish API stability and the operational contracts for deployment, backup, upgrades, and accepted result-cache loss before promising provider compatibility. Deployment options do not by themselves imply high availability.
