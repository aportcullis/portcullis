# M4 · Watch

**Not started — access governance beyond a single request.**

Extend the initial M2 connector's minimum default-deny protected-output gate into versioned application-wide disclosure policies and server-side masking/withholding across result APIs, full cells, CSV and SQL/catalog/plan metadata. Existing SQL audit redaction is a separate capability. Validate leak-free outputs, policy changes/cache reuse and fail-closed handling before exposing the broader surfaces to agents. The initial ChatGPT/Claude connectors satisfy their own protected-output, registration and expiring human-delegation acceptance in M2.
Then validate temporary web-console access, multistage approval and broader identity integration beyond M3 Keycloak sign-in sequentially. A session must retain per-statement governance and immediate revocation; it must not become a way around the request policy.

**To advance:** Revalidate demand and the console threat model before implementing each capability. The [temporary-access contract](../../product/prd.en.md#49-temporary-web-sql-console-threat-model-m4) defines expiry, concurrency, revocation, transaction boundaries, and audit. Native database proxy access remains a Later candidate.
See the [connector sequencing decision](../../adr/0090-early-delegated-ai-connectors.md) and [Gateway scope](../../product/prd.en.md#413-agent-neutral-mcp-gateway-and-deployment-m2--m6) for the initial M2 and expanded M6 boundaries.
