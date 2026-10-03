# Security conventions

- **Cryptography**
  - Use [`internal/infra/crypto`](../../internal/infra/crypto) for AES-256-GCM envelopes, HKDF sub-keys, keyed-HMAC digests, and Argon2id.
  - Follow ADR-0003 for canonical AAD and storage format.
  - Refuse startup without a valid master key.
- Encrypt at rest: connection credentials, request SQL + parameters (including inline literals), and result snapshots.
- **Never log** SQL, parameters, credentials, or result rows.
- Treat correlation headers as untrusted input: accept an `X-Request-Id` only when non-empty, **≤ 128 bytes**, and matching `[A-Za-z0-9._:/-]` exactly; anything else is **replaced** with a fresh generated id (never truncated) before echoing, logging, or persisting it in audit metadata (ADR-0010).
- Pin PostgreSQL execution string interpretation to `standard_conforming_strings=on` and verify the server report before issuing SQL (ADR-0044); never rely on inherited database/role defaults.
- **Audit evidence**
  - Store redacted SQL with comments removed and literals replaced by typed placeholders.
  - On parse failure, retain only digest and statement type.
  - Block UPDATE, DELETE, and TRUNCATE on `audit_events` at the database boundary.
- **Mutation timestamps**
  - Lock, call `ObserveWallClock`, then use that instant for rows and audit events (`internal/infra/postgres/instant.go`).
  - `now()` is transaction start time and can predate lock waits.
  - `TestWriteQueriesDoNotStampWithNow` rejects it in write queries unless allowlisted with a reason (ADR-0009).
- **Sessions / CSRF** per ADR-0006: `__Host-` cookies (`HttpOnly; Secure; SameSite=Lax`), opaque 32-byte tokens stored as `sha256`, rotation on login, HMAC session-bound double-submit CSRF.
- **Google OIDC** per ADR-0007: server-side callback (no client SDK), Authorization Code + PKCE, `state` + `nonce`, verify `email_verified`, link to an existing user (no auto-provisioning).
