# PostgreSQL alpha quickstart

The M1 implementation provides local/password and Google authentication, connection policy, immutable requests and approvals, single-use PostgreSQL execution, encrypted result snapshots, and audit history. The complete M1 correctness gate has passed with Docker-hosted Chromium; the [validation record](m1-validation.md) describes the environment and separate capacity qualification. MySQL/SQLite and saved queries remain later milestones.

For a disposable local installation, run `docker compose up --build` from the project root and open `http://localhost:8080`. Compose initializes PostgreSQL, creates a persistent master key, runs owner migrations separately, and starts the restricted runtime server. The sample credentials and disabled database TLS are for local development.

1. Bootstrap the first administrator with a display name, email, and password.
2. On Connections, register a PostgreSQL target using a dedicated least-privilege account. For the Compose database itself, the host is `postgres`, port `5432`, database `portcullis`, user/password `portcullis`; this owner account is suitable only for a disposable demonstration. Choose TLS `disable` for that local target and create the connection; the server tests it before persisting credentials.
3. Open Policy, keep read enabled, and set Read approvals to `0` for a single-user demonstration. Save the policy; real teams should retain the default distinct-reviewer quorum.
4. On Requests, create `SELECT 9007199254740993::bigint AS exact_value` against this connection and Submit. Verify Approved, then Execute once.
5. Verify Succeeded and open Result. Sort columns, filter values, change page size, open full cells, and Export CSV, then use Download CSV to save the prepared file. Large integers and decimals remain exact strings. CSV always represents the whole cached snapshot, independent of the current page/filter.

Results expire 15 minutes after admission and may be evicted earlier under quota pressure. Limits are the submitted connection-policy snapshot, capped at 10,000 rows/25 MiB; the existing default policy limits results to 16 MiB. The interface marks truncated snapshots; returning writes still commit the whole approved statement. Only the original requester can execute or read its result, regardless of reviewer access to request details.

The request list refreshes every 30 seconds and when reconnecting or returning to the foreground. Approvers see a pending badge. For distinct-reviewer scenarios use provisioned users and roles; user-management UI arrives in M4.

An `outcome_unknown` means completion could not be confirmed. Check the target database and audit evidence before creating a new request: Portcullis never automatically reruns it. Stop execution sends a best-effort cancellation and is not proof of rollback. Expired owner recovery runs on startup and every 30 seconds. Cache loss after PostgreSQL crash/failover does not change durable execution history.

Back up metadata and master keys together; follow [key rotation](key-rotation.md) for key changes. The alpha runs as one serving process; multi-instance cancellation routing is not provided. Capacity and hardware recommendations remain subject to the [performance qualification plan](../performance/README.md).
