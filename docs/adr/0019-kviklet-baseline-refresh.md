# ADR-0019: Refresh the kviklet comparison baseline to 0.9.2

- **Status:** Accepted
- **Date:** 2026-09-30

## Context
The PRD's competitor assumptions were recorded before kviklet's 0.8/0.9 releases.
GitHub's official releases API now identifies **0.9.2**, published **2026-09-29**, as the latest release.
Portcullis does not depend on kviklet as a runtime library; this update concerns product assumptions and the implementation roadmap.

## Verified changes

| Release | Officially documented change | Implication for Portcullis |
|---|---|---|
| 0.7.0 | Optional execution-result storage and transactional dry-run; role review requirements and IdP role sync are Enterprise features | Result storage alone is not a differentiator. Keep the snapshot, quota, encryption and requester-only access contracts explicit. |
| 0.8.0 | Review sidebar, permission-aware UI, connection/author/date request filters; ALLOW-only RBAC | Treat these as workflow baselines; withdraw blanket claims that kviklet lacks usable navigation or review UX. |
| 0.8.0 | Execute-right holders can execute approved single-execution requests from other authors | This differs deliberately from Portcullis's requester-only execution and result visibility. Temporary access and dumps remain author-only in the 0.9.2 source. |
| 0.9.0 | PostgreSQL proxy rework and MySQL/MariaDB proxy support; the proxy feature becomes Enterprise-only and remains beta | Compare web sessions separately from native-client proxy access. Preserve the web-session-first scope and deferred proxy decision. |
| 0.9.0 | Request-type filter, searchable connection/author filters, full-value result-cell dialog, user deactivation and authenticated credential encryption | These are baseline usability/security expectations, not reasons to expand the alpha before its execution path works. |
| 0.9.0 | Usage telemetry is enabled by default and can be disabled using `KVIKLET_TELEMETRY_ENABLED=false` | Include outbound behavior in deployment comparisons. The tagged README states that telemetry includes instance URL and version; do not equate anonymous usage with zero external communication. |
| 0.9.1 | IAM authentication for proxy sessions; stock MySQL CLI/PyMySQL compatibility | Native-client compatibility is now broader than PostgreSQL alone. It still incurs separate wire-protocol and audit work for Portcullis. |
| 0.9.2 | Single-execution Kubernetes requests use the approved command rather than a replacement from the execute body; Live Session responses stop being logged verbatim | Preserve immutable approval binding and payload/result-free application logs. Apply these lessons to the forthcoming SQL executor without adding Kubernetes support. |

The tagged review model still uses per-connection `ReviewConfig.numTotalRequired`, distinct reviewers and approval reset after EDIT events (also execution errors for non-temporary requests).
Enterprise role requirements add constraints beyond the total-review floor.
This supports ADR-0015/0018's precedent without making Portcullis a copy of that model.

## Options
1. Keep old competitor claims: inexpensive but contradicted by current releases.
2. Copy every new feature into the alpha: broader parity but delays a functioning approved execution loop and changes authorization semantics.
3. Refresh evidence and acceptance scenarios while preserving the existing staged scope.

## Decision
Choose option 3.
Amend PRD §§1, 3, 4.3, 4.6, 7.1, 10 and 13 to cite the new baseline.
UX superiority remains a hypothesis requiring the same-task comparison; pagination or result storage alone does not establish it.
Saved-query versioning, parameter definitions, sharing/favorites and asset-to-request reuse remain Portcullis's intended workflow, not a verified claim that kviklet lacks each feature.

Keep requester-only execution and result access, immutable submitted payloads, pre-execution digest/authorization checks, one-time leases and no automatic replay.
Transactional dry-run is not automatically adopted: rollback cannot undo every database effect.
Keep the existing web-session-first strategy and milestone order.
Do not add telemetry as a consequence of competitor parity.

## Consequences
- Alpha work remains lease/reconciler → pre-execution authorization/digest/function checks → result caps/store → Execute RPC and results UI.
- Add scenario gates to the executor work: altered execute-body SQL/parameters must never reach the target; a concurrent rejected execution must not release another execution's lease; application/error logs must not expose payloads or result rows.
- Before asserting better UX, compare request discovery, SQL review, approve/reject, full-value cell viewing and history-to-asset reuse with kviklet 0.9.2.
  Request filters and review layout changes remain follow-up work rather than silently expanded alpha requirements.
- This is a documentation and planning update.
  The executor and result UI are still pending; this ADR does not claim those scenario gates have passed.

## Sources (checked 2026-09-30)
- [Official latest-release API](https://api.github.com/repos/kviklet/kviklet/releases/latest) and [0.9.2 release](https://github.com/kviklet/kviklet/releases/tag/0.9.2).
- [0.9.2 security diff](https://github.com/kviklet/kviklet/compare/0.9.1...0.9.2).
- [0.9.1 release](https://github.com/kviklet/kviklet/releases/tag/0.9.1), [0.9.0 release](https://github.com/kviklet/kviklet/releases/tag/0.9.0), [0.8.0 release](https://github.com/kviklet/kviklet/releases/tag/0.8.0), [0.7.0 release](https://github.com/kviklet/kviklet/releases/tag/0.7.0).
- [Tagged README: editions, proxy, review gates, telemetry](https://github.com/kviklet/kviklet/blob/0.9.2/Readme.md).
- [Tagged request DTO: ReviewConfig, quorum/reset and executor authorization](https://github.com/kviklet/kviklet/blob/0.9.2/backend/src/main/kotlin/dev/kviklet/kviklet/service/dto/ExecutionRequest.kt).
- [Tagged connection DTO: connection-level review configuration](https://github.com/kviklet/kviklet/blob/0.9.2/backend/src/main/kotlin/dev/kviklet/kviklet/service/dto/Connection.kt).
