# ADR-0014: Connection registration, credential envelope & TLS validation (PostgreSQL)

- **Status:** Accepted
- **Date:** 2026-07-11

## Context
M1's first slice registers PostgreSQL connections (PRD §4.1, §7.2): per-type form, mandatory connection test **before save**, application-level credential encryption (§8.1), archive instead of delete (§4.3), admin-only management, and "credential and raw DSN are never returned again via UI/API after creation" (§7.2).
The PRD leaves open: the exact TLS mode set, what "encrypted config" covers, the archive snapshot's target fingerprint, uniqueness, update semantics, the test timeout, and how target-DB errors are redacted (§8.1).
This ADR closes those for PostgreSQL; MySQL/SQLite (M2) amend rather than redesign.

## Decision

### Storage split: plaintext descriptor + one sealed credential
A connection row stores a **plaintext descriptor** — `db_type`, `display_name`, `host`, `port`, `database_name`, `tls_mode`, `target_fingerprint` — and **one AEAD envelope** holding a versioned JSON credential `{"v":1,"user":…,"password":…}`.
Rationale: the list UI and the archive snapshot need the descriptor without a decrypt round-trip; the credential (user + password) is exactly what §7.2 forbids returning and §4.3 requires discarding on archive.
The username lives inside the blob, so no RPC response can ever carry it.

The envelope is persisted as four columns mirroring `crypto.Blob` — `credential_key_version int`, `credential_wrapped_dek bytea`, `credential_nonce bytea`, `credential_ciphertext bytea` — with an all-or-none CHECK.
`key_version` stays a queryable column so the ADR-0003 eager rotation batch ("rows where key_version < active") needs no schema change.
The multi-version keyring load and `key rotate` CLI remain a separate Core-1 slice (ADR-0003 Implementation status).

- **AAD** (ADR-0003 canonical layout): `portcullis/aad/v1|connection_credential|<org_id>|<record_id>`.
  The record type token `connection_credential` is the one ADR-0003 already names.
- **The connection UUID is app-generated** (`github.com/google/uuid`, v4): the AAD binds the record id *before* `Seal`, so a DB-side default is impossible.
  The column has no `gen_random_uuid()` default on purpose. google/uuid is slow-moving but the de-facto standard and already in the module graph (v1.6.0).
- **Request ids are canonicalized at the transport boundary** (amended 2026-07-18, external review): `uuid.Parse` accepts non-standard encodings (uppercase, no hyphens, `urn:`, braces — its docs warn against using it for validation), and PostgreSQL matches any of them to the same row while the AAD binds the *string*.
  The transport therefore re-renders every parsed id to the canonical lowercase form, and the use cases bind Seal/Open and audit target ids to the **stored** row id, never the caller's spelling — otherwise an update through a non-canonical spelling would seal a credential no later canonical request could decrypt.
- **A raw DSN string is never constructed.** The tester parses a password-free parameter set and injects the password directly into the `pgconn.Config` field, so no parse error, log line, or driver message can ever embed it.

### TLS modes
Accepted values: **`verify-full` (default)**, `verify-ca`, `require`, `disable`.
`prefer`/`allow` are rejected: they silently downgrade to plaintext, which contradicts §8.1's "certificate-verifying TLS mode is the default" posture.

| mode | encryption | chain validated | hostname checked | classification |
|---|---|---|---|---|
| verify-full | yes | yes | yes | default |
| verify-ca | yes | yes | **no** | allowed, UI shows the hostname caveat |
| require | yes | **no** | no | **relaxed** |
| disable | no | no | no | **relaxed** |

Choosing a relaxed mode (`require`, `disable`) needs the admin's explicit selection and emits `CONNECTION_TLS_RELAXED` in the same transaction as the create/update (§8.1).
`verify-ca` validates the chain, so it is not "relaxed", but the form and docs state that it does not check the hostname.

**CA source (verified in pgx v5.10.0 source, `pgconn/config.go` `configTLS`):** with `sslrootcert` unset, pgx's `verify-full` sets only `tls.Config.ServerName`, leaving `RootCAs` nil — Go's `crypto/tls` then verifies against the **operating-system trust store**.
So `verify-full` works out of the box for publicly-trusted server certificates; targets using a private CA (typical for managed PGs) need a custom root CA. **Custom CA upload (`sslrootcert` PEM) is deferred** — the credential JSON and the form are versioned so adding it is compatible.
`require` with no root cert is `InsecureSkipVerify`; with one it upgrades to verify-ca behavior (libpq-compatible) — irrelevant here until CA upload lands.

*Amended 2026-07-13/-15 (external review):* the tester is **environment-independent**.
The connection string wins pgx's settings merge over both `PG*` environment variables and a `PGSERVICE` file, so every result-affecting key is pinned there to an explicit safe default — TLS material (`sslrootcert`/`sslcert`/`sslkey`/`sslpassword` empty, `sslsni`, `sslnegotiation`) **and** the authentication-negotiation parameters (`channel_binding`, `require_auth`, `target_session_attrs`, `min`/`max_protocol_version`); the parsed struct then clears runtime parameters, fallbacks, and validators.
Without this, a lone `PGCHANNELBINDING=require` or a dangling `PGSSLROOTCERT` path flips a healthy target to a failure (both reproduced).
This also keeps the deferred-custom-CA decision honest: no CA can sneak in via the environment.

*Amended 2026-07-18 (external review):* the parse step itself is isolated too.
`connect_timeout` is pinned in the connection string (an invalid `PGCONNECT_TIMEOUT` would otherwise fail `ParseConfig`).
`PGSERVICE` cannot be pinned by value — pgx triggers the service-file lookup whenever the merged settings *contain* the key (presence check, v5 source), and hand-building the config is impossible (`ConnectConfig` accepts only a `ParseConfig` product) — so when `PGSERVICE` is set the tester generates a throwaway service file holding just an empty section for that name and pins `servicefile` to it (the connection string wins over `PGSERVICEFILE`): the lookup resolves and contributes nothing.
Documented limitation: a service name an INI section cannot express (`[`/`]`/newline) is not neutralizable and fails closed.
`passfile` is pinned to `os.DevNull`: ParseConfig OPENS the passfile unconditionally (the password checks only gate use of the result), config assembly runs before any timeout context, and the credential is injected directly — so neither a blocking `PGPASSFILE` (a FIFO reproduces an unbounded hang) nor the `~/.pgpass` default may ever be touched.

### Mandatory server-side test-before-save
`Create` and any config-changing `Update` run the connection test *inside* the use case and refuse to persist when it fails.
The `Test` RPC (by config pre-save, or by id for a saved connection) is UX convenience, not the enforcement point.
Timeout: `PORTCULLIS_CONNECTION_TEST_TIMEOUT`, default **10s** (pgAdmin's default connection timeout; libpq recommends no less than 2s), bounds **[1s, 60s]**, startup-rejected outside the range (ADR-0010-style bounded config).
The eventual PG `QueryDialect.ValidateConnection` (§5.3) absorbs the tester; the app-layer `Tester` port is that seam.

*Amended 2026-07-18 (external review):* a test-by-id reads the descriptor and the sealed credential in **one statement** (`TestMaterial`), so a config replace committing between two separate reads can never pair an old target with a freshly re-entered credential (under READ COMMITTED).
The dialer also sets `pgconn.Config.Host/Port/Database` from the **validated Target**, not from the parsed URI — pgx would otherwise split a comma-bearing host into multiple hosts and strip leading slashes from the database path, dialing a target different from the stored (and audited) descriptor; the domain additionally rejects URI-structural characters in the host.

### Target fingerprint
`hex(sha256("portcullis/fingerprint/v1|postgresql|" + lower(trim(host)) + "|" + port + "|" + database_name))`.
Versioned prefix, canonicalized inputs, stored plaintext, survives archive — it is the "target-identifying fingerprint" §4.3 requires in historical snapshots without retaining credentials or full paths.

*Amended 2026-07-18 (external review):* the v1 input is unambiguous because the host additionally rejects `|` (never valid in a DNS name or IP literal) and the port is digits-only: the first separator after the db-type token ends the host uniquely, and the port↔database boundary cannot shift into a digit-leading database name.
The database name may still contain `|` — no two distinct targets can collide, so the v1 layout stands without a version bump.

### Uniqueness & update
- `unique (organization_id, lower(display_name)) where archived_at is null` — one active name per org; the name is reusable after archive.
  Targets are deliberately **not** unique (two connections to one database with different accounts is legitimate).
- **Rename-only updates skip the test.** Any config change requires the **full credential re-entry** plus a fresh successful test (there is no partial credential edit — consistent with never returning stored values).
- Every row has a positive `bigint version`, initialized to 1 and incremented by SQL on every rename, config replacement, and archive.
  A config replacement captures that version before its external test and writes only `where version = expected_version`, while SQL performs `version = version + 1`.
  A concurrent rename/config change therefore returns a conflict for reload-and-retry instead of being silently overwritten by the slower test request.
  `updated_at` remains display/audit metadata, never a lock token: timestamp precision cannot guarantee a distinct value for consecutive writes. **A second token, `config_version`, answers "is this still the database that was approved?"** (added 2026-07-27, external review round 13; migration 0014).
  `version` moves on every rename, config replacement and archive, so it cannot serve: pinning it would expire live approvals for a label edit.
  `config_version` moves **only** in `ReplaceConnectionConfig` — the one statement that changes host, port, database, TLS mode or credential — and never on a rename or an archive.
  Access requests pin it in their approval unit, and a config replacement expires the ones approved against the old value (ADR-0018).
  The two tokens are deliberately separate concerns: `version` guards concurrent EDITS, `config_version` describes the TARGET's identity over time. **The condition covers the descriptor-only update too, and `Update` requires the token from the caller** (amended 2026-07-26, external review round 12).
  Both flows write name, environment and description as **full replacement values** — the SPA sends all three from the form it opened with — so a form built before someone else's change reverts the fields it is not even editing, and the `fields` audit metadata (computed from the editor's own pre-read) does not name what was reverted.
  `expected_version` is therefore a required field of `UpdateConnectionRequest`: a missing or non-positive value is `INVALID_ARGUMENT` rather than a silently unguarded write, and a mismatch is `ABORTED`.
  The service compares the token to the row it read and hands the store **the version it read** (the rule the request vertical follows in ADR-0018), while the store keeps its own `where version = …` for the window a slow external test opens.
  The detail read (`Connection`) now carries `version` alongside the summary's, so `Get → Update` is possible without also holding `connections.list`.
  Rename-only updates of an **archived** row stay allowed: that statement has no `archived_at is null` predicate, so zero rows means missing or stale, never archived.
- `ConnectionSummary` carries the same version.
  The SPA applies a mutation response only when its version is newer than the cached row, so reordered update/archive responses cannot resurrect an archived connection.
  The list response remains descriptor-safe; a row's Details dialog calls `connections.get` on demand to show host, port, database, TLS mode, and fingerprint to callers holding that distinct permission.

### Archive & restore
Archive (gated by `connections.delete` — the catalog's delete verb *is* archive; no hard delete exists) is one transaction: set `archived_at`, **null all four credential columns**, write `CONNECTION_ARCHIVED`.
A CHECK (`archived_at is null or credential_key_version is null`) makes "archived row still holds a credential" unrepresentable.
Restore is a **deferred follow-up**: it will be gated by `connections.update` (it is update-shaped — full credential re-entry + re-test), and nothing in this slice blocks it (descriptor columns survive).
The §4.3 "refuse archive while an execution is in flight" guard is a documented seam: the archive UPDATE is written so the executions slice adds a `not exists (…)` predicate, nothing more.

*Amended 2026-07-18 (external review):* archive vs an **in-flight connection test** is decided as *admitted tests complete*: archive immediately blocks NEW admissions (`TestMaterial` returns archived-refusal, matching PRD §4.3's "immediately blocks new … connection tests"), while a test that already read its credential snapshot finishes its dial, bounded by the test timeout (≤60s, default 10s).
No lease or cancel-and-wait: the credential is already in memory and the dial may already be on the wire, so cancellation cannot un-access the target — it would only trade a bounded, audited completion (CONNECTION_TEST is recorded either way) for false assurance.
Long-running query **executions** remain the real concern and keep their own archive guard (above).

### Error redaction (PRD §8.1: "대상 DB 오류도 credential을 redaction한 뒤 반환")
Target-DB errors are classified into coarse buckets — `unreachable`, `auth-failed`, `tls-failed`, `unknown-database`, `timeout`, `failed` (fallback) — and **only the bucket's fixed message** crosses the API boundary.
Raw driver text is never returned and never logged above debug-free fields (OWASP Logging Cheat Sheet: authentication passwords and database connection strings must never reach logs).
Tests assert the password substring appears in no returned or logged message.

### Audit vocabulary
`CONNECTION_CREATED` / `CONNECTION_UPDATED` / `CONNECTION_ARCHIVED` — transactional, same commit as the mutation (ADR-0009); `CONNECTION_TLS_RELAXED` — transactional, alongside the create/update that chose the relaxed mode; `CONNECTION_TEST` — best-effort with outcome `SUCCEEDED|FAILED` (detached write, like failed logins).
`target_type="connection"`, `target_id` = connection UUID.
The `audit_events.connection_id` snapshot column stays reserved for execution-path events.

`CONNECTION_ARCHIVED` metadata is assembled from the archive statement's `RETURNING` row inside that transaction: `display_name`, `db_type`, and `fingerprint`.
This both satisfies the PRD's self-contained historical snapshot and prevents a concurrent config update from making the audit record describe an older descriptor.

*Amended 2026-07-13 (external review):* inside Create/Update a successful dial is normally implied by the transactional `CONNECTION_CREATED`/`UPDATED` — but when the mutation itself fails (name conflict, seal failure, metadata-DB error) that implication rolls back with it, so the service records a best-effort `CONNECTION_TEST SUCCEEDED` with `persisted: false`: the target was accessed even though nothing was saved.

*Amended 2026-07-18 (external review):* every `CONNECTION_TEST` event carries `tls_mode` in its metadata.
`CONNECTION_TLS_RELAXED` stays bound to the create/update transaction, so explicit tests and failed-save dials would otherwise leave no audit-visible record that the target was reached with `require`/`disable` — §8.1 requires relaxed-TLS use to be auditable on every path that actually connects.

### Runtime-role grants
`connections` is **not** append-only: the runtime must SELECT/INSERT/UPDATE (archive is an UPDATE).
DELETE is revoked anyway in the same migration — hard delete is forbidden by the PRD (§4.3, §6: FK RESTRICT, no hard-delete API), so the runtime role should be structurally unable to do it.
Boot verification enforces this: `privcheck.go` runs a per-table policy matrix over **every** table in `public` (not just `audit_events`/`schema_migrations`), with `connections` requiring SELECT/INSERT/UPDATE and forbidding DELETE (ADR-0009 amendment).

### Deferred (recorded so they are decisions, not omissions)
| deferral | lands with |
|---|---|
| `connection_policy_versions` + `connections.current_policy_version` (+ default policy backfill: read=true/write=false/ddl=false, required_approvals=1) | **landed 2026-07-18** — migration 0011, ADR-0015 |
| multi-version keyring + `key rotate` CLI | own Core-1 slice (ADR-0003) |
| restore/unarchive flow | follow-up (rule fixed above) |
| in-flight-execution archive guard predicate | executions slice |
| list pagination (`include_archived` flag instead) | when connections outgrow one page |
| custom root CA upload (`sslrootcert` PEM) | when a private-CA target needs it |
| `Me.permissions` for nav-level hiding | **landed 2026-07-18** — Login/Me carry the caller's permission keys; the SPA's can() hides affordances (server authorization unchanged) |

## Consequences
- The credential is unrecoverable after archive (by design, §4.3) and after master-key loss (accepted, ADR-0003) — restore always means re-entry + re-test.
- Every layer can be tested without real TLS material except the tester's verify-full negative case, which uses the non-TLS test container (expected `tls-failed`).
- MySQL (M2) reuses the descriptor/envelope/fingerprint shape with its own TLS-mode vocabulary; SQLite replaces host/port with a validated server-local path (§7.2) — both are amendments here.

## Sources (checked 2026-07-11)
- libpq SSL mode semantics table & default root.crt location: https://www.postgresql.org/docs/current/libpq-ssl.html
- pgx v5.10.0 TLS mapping (verify-full → `ServerName` only, nil `RootCAs` → Go system pool; `require` → `InsecureSkipVerify`; `sslrootcert=system`): `pgconn/config.go` `configTLS` (module source, v5.10.0)
- Go `crypto/tls` nil `RootCAs` = host's root CA set: https://pkg.go.dev/crypto/tls#Config
- OWASP Logging Cheat Sheet (never log passwords / DB connection strings): https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html
- OWASP Secrets Management Cheat Sheet: https://cheatsheetseries.owasp.org/cheatsheets/Secrets_Management_Cheat_Sheet.html
- pgAdmin connection timeout default 10s, libpq "not recommended less than 2s": https://www.pgadmin.org/docs/pgadmin4/latest/server_dialog.html
- google/uuid v1.6.0 status: https://github.com/google/uuid/releases
