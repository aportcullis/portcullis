# Security conventions

- Use [`internal/infra/crypto`](../../internal/infra/crypto) for all crypto: AES-256-GCM **envelope**
  encryption (canonical AAD layout and on-disk format per ADR-0003), HKDF-derived sub-keys,
  keyed-HMAC `payload_digest`, Argon2id. The server **refuses to start** without a valid
  master key (ADR-0003).
- Encrypt at rest: connection credentials, request SQL + parameters (including inline literals), and
  result snapshots.
- **Never log** SQL, parameters, credentials, or result rows.
- Treat correlation headers as untrusted input: accept an `X-Request-Id` only when non-empty,
  **≤ 128 bytes**, and matching `[A-Za-z0-9._:/-]` exactly; anything else is **replaced** with a
  fresh generated id (never truncated) before echoing, logging, or persisting it in audit
  metadata (ADR-0010).
- Audit stores **redacted SQL** (comments stripped, literals → typed placeholders) and is
  **fail-closed** (digest + statement type only when parsing fails). `audit_events` is append-only —
  UPDATE/DELETE/TRUNCATE are blocked at the database.
- **Sessions / CSRF** per ADR-0006: `__Host-` cookies (`HttpOnly; Secure; SameSite=Lax`), opaque
  32-byte tokens stored as `sha256`, rotation on login, HMAC session-bound double-submit CSRF.
- **Google OIDC** per ADR-0007: server-side callback (no client SDK), Authorization Code + PKCE,
  `state` + `nonce`, verify `email_verified`, link to an existing user (no auto-provisioning).
