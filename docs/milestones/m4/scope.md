# M4 · Watch

**Not started — access governance beyond a single request.**

First implement versioned sensitive-data disclosure policies and server-side masking/withholding across result APIs, full cells, CSV and SQL/catalog/plan metadata. Existing SQL audit redaction is a separate capability. Validate leak-free outputs, policy changes/cache reuse and fail-closed handling before any agent integration. Then validate temporary web-console access, multistage approval and broader identity integration sequentially. A session must retain per-statement governance and immediate revocation; it must not become a way around the request policy.

**To advance:** Revalidate demand and the console threat model before implementing each capability. The [temporary-access contract](../../product/prd.en.md#49-temporary-web-sql-console-threat-model-m4-gate-decided-2026-07-04) defines expiry, concurrency, revocation, transaction boundaries, and audit. Native database proxy access remains a Later candidate.
