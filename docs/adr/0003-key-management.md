# ADR-0003: Key management

- **Status:** Accepted — key hierarchy, algorithms, provisioning, and rotation model fixed.
  (Amended 2026-07-04: on-disk envelope format, HKDF labels, canonical AAD layout, master-key
  file format, rotation execution, and the full Argon2 parameter set are pinned.)
- **Date:** 2026-06-27 (amended 2026-07-04)

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
- **Derived purpose keys** via HKDF-SHA-256 (salt = nil, sub-key length 32 bytes) from the KEK
  with distinct `info` labels — the exact label strings, versioned so a future derivation
  change coexists with old data:
  - `info="portcullis/payload-integrity/v1"` → the HMAC-SHA-256 key for `payload_digest`
    (also keys the CSRF token MAC, ADR-0006).
  - `info="portcullis/dek-wrap/v1"` → the key-wrapping key for data-encryption keys.
- **Data encryption keys (DEK)** — a fresh 256-bit key per encrypted unit (per result snapshot;
  per request payload). The DEK encrypts the data with AES-256-GCM; the DEK is wrapped by the
  `dek-wrap` key. GCM associated data binds record identity (below) to prevent swapping.

### Envelope format (v1 — as implemented in `internal/infra/crypto`)
Stored per record as `Blob`:
| Field | Content |
|---|---|
| `key_version` | KEK version whose `dek-wrap` key wrapped the DEK (int, ≥ 1; v1 keyring loads a single version `1`) |
| `wrapped_dek` | `wrapNonce (12B) ‖ AES-256-GCM(dek-wrap key, DEK)` — nonce prefixed, one field |
| `nonce` | 12-byte data nonce |
| `ciphertext` | `AES-256-GCM(DEK, plaintext)` |

- Every nonce is a fresh **12-byte CSPRNG value** (standard GCM nonce size; random nonces are
  safe far below NIST's 2^32-invocations-per-key bound because each DEK encrypts one unit).
- **Both layers** (DEK-wrap and data) authenticate the **same associated data**; a mismatch on
  either open fails closed (`ErrDecrypt`, no detail).

### Canonical associated data (AAD)
One layout for every envelope, UTF-8, `|`-separated (safe: every field is a fixed token,
UUID, or integer — no escaping needed):

```
portcullis/aad/v1|<record_type>|<organization_id>|<record_id>[|<chunk_index>]
```

- `record_type` — fixed lowercase token per table, e.g. `connection_credential`,
  `request_payload`, `saved_query_version`, `result_chunk`, `schema_artifact_file`,
  `oidc_pending` (the OIDC pending-auth cookie, ADR-0007).
- `organization_id` / `record_id` — canonical lowercase UUID text of the owning org and row.
- `chunk_index` — present only for chunked payloads (result chunks, artifact files), decimal.
- The KEK `key_version` is *not* in the AAD: it is bound implicitly — it selects the HKDF-derived
  wrap key, so a tampered version fails the unwrap authentication. (PRD §8.1's "key ID"
  binding is satisfied by this mechanism.)

### Provisioning
- Supplied as a 32-byte key, base64-encoded, via **either** `PORTCULLIS_MASTER_KEY` **or**
  `PORTCULLIS_MASTER_KEY_FILE` (a mounted file — preferred for production). Setting **both**
  is refused: a stale env var silently overriding a mounted file could re-encrypt with the
  wrong key and orphan data.
- **File format:** the file's entire contents are the **standard base64** (padded ok) of the
  32 raw bytes; surrounding whitespace/newline is trimmed. Nothing else — no PEM, no JSON.
- On boot: decode, validate length (exactly 32 bytes after decode), and load. Missing or
  malformed → refuse to start. The loaded key is **version 1** until rotation ships.
- **Multi-version provisioning (with the rotation CLI):** the active key stays in
  `PORTCULLIS_MASTER_KEY`/`_FILE`; retired-but-still-decrypting versions are supplied as
  `PORTCULLIS_MASTER_KEY_PREVIOUS` — comma-separated `<version>:<base64>` entries (or the
  `_PREVIOUS_FILE` variant with one `<version>:<base64>` per line). The active version number
  is `max(previous versions) + 1`. Duplicate or non-monotonic versions refuse to start.

### Rotation
- Multiple KEK versions coexist; the **highest version is active** for new writes, older
  versions remain available for decrypt/unwrap during transition.
- A `key rotate` CLI re-wraps DEKs and re-encrypts request payloads to the new version as an
  **eager batch** (decided 2026-07-04; no lazy-on-read path). Eager keeps completion
  observable — the CLI reports "0 rows on old versions", which is the precondition for
  retiring a version; lazy rotation can never prove completion and leaves ciphertext on old
  keys indefinitely. The batch is resumable (keyed by `key_version < active`) and throttled;
  reads during rotation work throughout because all versions stay loaded.
- `payload_digest` carries its `key_version`; verification uses the version recorded on the row.
- **Implementation status:** the rotation model (versioned columns, multi-version coexistence) is
  fixed here, but the multi-version keyring load and the `key rotate` CLI are implemented alongside
  the Core 1 features that use envelope encryption (connection credentials, result snapshots). Until
  then the keyring loads a single active version.
- **Operational note (CSRF):** the session CSRF token (ADR-0006) is HMAC'd with the active key and
  carries **no** `key_version`, so rotating the master key invalidates outstanding CSRF tokens —
  users must re-login. Tagging the CSRF token with a `key_version` for graceful rotation is a future
  option, taken with the rotation CLI.

### Password hashing (Argon2id)
- Store the Argon2id variant/version, salt, and cost parameters with each hash, PHC-encoded:
  `$argon2id$v=19$m=<KiB>,t=<time>,p=<threads>$<b64raw salt>$<b64raw hash>`
  (base64 raw/unpadded, std alphabet).
- **v1 profile (complete):** m = 64 MiB (`65536` KiB), t = 3, p = 2, **salt = 16 bytes
  (CSPRNG), key/tag = 32 bytes**. Revisit with a benchmark; a profile change triggers
  rehash-on-login.
- **Validation bounds** (enforced before hashing/verification — out-of-range params would
  panic Argon2 or exhaust memory): t ∈ [1, 10]; p ∈ [1, 16]; m ∈ [8×p KiB, 256 MiB];
  salt ∈ [8, 64] bytes; key ∈ [16, 64] bytes. Out-of-range stored hashes fail verification
  (`ErrBadHash`), never execute.
- **Concurrency cap:** at most `PORTCULLIS_ARGON2_MAX_CONCURRENT` hashes in flight
  (default **2**, valid range **[1, 256]** — 256 already permits ~16 GiB of hashing memory);
  a semaphore with deterministic cancellation (an already-cancelled request never starts a
  hash). Each hash costs ~m bytes, so the cap bounds hashing memory independently of cores.
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
- Rotation is operationally a CLI plus a batch re-encrypt pass; designing tables to carry
  `key_version` from day one keeps rotation non-breaking. (Eager-batch execution decided
  above, 2026-07-04.)
