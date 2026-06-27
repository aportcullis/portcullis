# ADR-0003: Key management

- **Status:** Accepted — key hierarchy, algorithms, provisioning, and rotation model fixed. Argon2id profile and rotation execution tuned during implementation.
- **Date:** 2026-06-27

## Context
Several secrets are stored at rest: connection credentials, request SQL + parameter values,
and query-result snapshots. Audit records additionally need a tamper-resistant integrity tag
(`payload_digest`) that does not leak low-entropy literals. This ADR fixes the key hierarchy,
algorithms, provisioning, rotation, and the loss-recovery posture. Password hashing is included
because it shares the "versioned crypto parameter" pattern.

Constraints:
- Single binary; no external KMS dependency required to boot (a KMS may be added later).
- Server must **refuse to start** if the master key is missing or malformed.
- The integrity tag must resist offline brute-force of guessable values (emails, ids, tokens)
  in long-retained audit rows — so it must be a **keyed** MAC, not a plain hash.

## Decision

### Algorithms
- **AEAD at rest:** AES-256-GCM.
- **Integrity tag (`payload_digest`):** HMAC-SHA-256.
- **Key derivation:** HKDF-SHA-256.
- **Password hashing:** Argon2id.

### Key hierarchy
- **Master key (KEK)** — 32 random bytes, carries a `key_version`. It never encrypts row data directly.
- **Derived purpose keys** via HKDF-SHA-256 from the KEK with distinct `info` labels:
  - `info="payload-integrity"` → the HMAC-SHA-256 key for `payload_digest`.
  - `info="dek-wrap"` → the key-wrapping key for data-encryption keys.
- **Data encryption keys (DEK)** — a fresh 256-bit key per encrypted unit (per result snapshot;
  per request payload). The DEK encrypts the data with AES-256-GCM; the DEK is wrapped by the
  `dek-wrap` key. Stored alongside each record: `key_version`, wrapped DEK, nonce. GCM associated
  data binds record identity (e.g. result id / request id + owner org id) to prevent swapping.

### Provisioning
- Supplied as a 32-byte key, base64-encoded, via **either** an environment variable **or** a
  mounted file (the file path is preferred for production; documented in the deployment examples).
- On boot: decode, validate length, and load all configured key versions. Missing or malformed
  → refuse to start.

### Rotation
- Multiple KEK versions coexist; the **highest version is active** for new writes, older
  versions remain available for decrypt/unwrap during transition.
- A `key rotate` CLI re-wraps DEKs and re-encrypts request payloads to the new version
  (batch or lazy-on-access); once all records are migrated the old version may be retired.
- `payload_digest` carries its `key_version`; verification uses the version recorded on the row.

### Password hashing (Argon2id)
- Store the Argon2id variant/version, salt, and cost parameters with each hash.
- Initial parameters: m = 64 MiB, t = 3, p = 2 (revisit with a benchmark; treat as the v1 profile).
- On successful login, if the stored parameters differ from the current profile, **rehash**.

### Loss-recovery posture
- If the KEK is lost and no backup exists, encrypted connection credentials, request payloads,
  and result snapshots are **unrecoverable** — this is accepted and must be documented.
- Audit **integrity tags and metadata survive** key loss by design (they are derived/stored
  separately and are not the only copy of operational history). Operators are told to back up
  the KEK in a secret manager and to keep metadata-DB backups.

## Consequences
- A small `crypto` package owns: KEK loading/validation, HKDF purpose-key derivation,
  `Seal/Open` (AES-256-GCM with associated data), DEK wrap/unwrap, `payload_digest` compute/verify,
  and Argon2id hash/verify/needs-rehash. It is the only place that touches key material.
- Rotation is operationally a CLI plus a background re-encrypt pass; designing tables to carry
  `key_version` from day one keeps rotation non-breaking.
- Open item: decide the exact rotation execution (eager batch vs lazy-on-read) under load — a
  performance question deferred to implementation, not a blocker for the schema/format here.
