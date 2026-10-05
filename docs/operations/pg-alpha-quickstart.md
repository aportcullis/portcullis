# PostgreSQL alpha quickstart

The M1 implementation provides local/password and Google authentication, connection policy, immutable requests and approvals, single-use PostgreSQL execution, encrypted result snapshots, and audit history.
The PostgreSQL governance correctness gate has historical passing evidence with Docker-hosted Chromium; the [validation record](../milestones/m1/validation.md) describes the environment and separate capacity qualification. M1 administration is still in progress; MySQL and saved queries are later milestones, and SQLite is excluded.

For a disposable local installation, run `docker compose up --build` from the project root and open `http://127.0.0.1:8080`. The HTTP port is published on IPv4 loopback; remote access requires the private HTTPS ingress described in [recommended architecture](recommended-architecture.md).
Compose initializes PostgreSQL, creates a persistent master key, runs owner migrations separately, and starts the restricted runtime server. The sample credentials and disabled database TLS are for local development. The key initializer preserves existing valid key bytes and sets runtime ownership (UID/GID 65532), file mode `0400` and directory mode `0700`.
It refuses invalid or empty existing keys rather than silently replacing them.

1. Copy the `setup_token` value from the "first-run setup token issued" line in `docker compose logs portcullis`, then bootstrap the first administrator with that token, a display name, email, and password.
   The token works once, expires after 24 hours, and is replaced on every restart until an administrator exists; set `PORTCULLIS_SETUP_TOKEN_FILE` to receive it in an owner-only file instead of the log (ADR-0052).
2. On Connections, register a PostgreSQL target using a dedicated least-privilege account. For the Compose database itself, the host is `postgres`, port `5432`, database `portcullis`, user/password `portcullis`; this owner account is suitable only for a disposable demonstration. Choose TLS `disable` for that local target and create the connection; the server tests it before persisting credentials.
3. Open Policy, keep read enabled, and set Read approvals to `0` for a single-user demonstration. Save the policy; real teams should retain the default distinct-reviewer quorum.
4. On Requests, create `SELECT 9007199254740993::bigint AS exact_value` against this connection and Submit. Verify Approved, then Execute once.
5. Verify Succeeded and open Result. Sort columns, filter values, change page size, open full cells, and Export CSV, then use Download CSV to save the prepared file. Large integers and decimals remain exact strings. CSV always represents the whole cached snapshot, independent of the current page/filter.

Results expire 15 minutes after admission and may be evicted earlier under quota pressure. Limits are the submitted connection-policy snapshot, capped at 10,000 rows/25 MiB; the existing default policy limits results to 16 MiB. The interface marks truncated snapshots; returning writes still commit the whole approved statement.
Only the original requester can execute or read its result, regardless of reviewer access to request details.

The request list refreshes every 30 seconds and when reconnecting or returning to the foreground. Approvers see a pending badge. The Administration → Users UI is not yet available.
M1 will add administrator-created users and one-time password setup links shown once with 24-hour expiry (ADR-0053).
Until that journey ships, this quickstart uses zero required read approvals for its disposable single-user demonstration; the synthetic walkthrough provisions its reviewer in the fixture.

An `outcome_unknown` means completion could not be confirmed. Check the target database and audit evidence before creating a new request: Portcullis never automatically reruns it. Stop execution sends a best-effort cancellation and is not proof of rollback. Expired owner recovery runs on startup and every 30 seconds.
Cache loss after PostgreSQL crash/failover does not change durable execution history.

Back up metadata and master keys together; follow [key rotation](key-rotation.md) for key changes. The alpha runs as one serving process; multi-instance cancellation routing is not provided. Capacity and hardware recommendations remain subject to the [performance qualification plan](../performance/README.md).

To verify key initialization separately, run `make keygen-check`. This uses a disposable container/tmpfs and never mounts the persistent demo secrets volume.
