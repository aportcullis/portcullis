# ADR-0003: Key management

- **Status:** Accepted — key hierarchy, algorithms, provisioning, and rotation model fixed.
  (Amended 2026-07-04: on-disk envelope format, HKDF labels, canonical AAD layout, master-key file format, rotation execution, and the full Argon2 parameter set are pinned.)
- **Date:** 2026-06-27 (amended 2026-07-04)

## Context
Several secrets are stored at rest: connection credentials, request SQL + parameter values, and query-result snapshots.
Audit records additionally need a tamper-resistant integrity tag (`payload_digest`) that does not leak low-entropy literals.
This ADR fixes the key hierarchy, algorithms, provisioning, rotation, and the loss-recovery posture.
Password hashing is included because it shares the "versioned crypto parameter" pattern.

Constraints:
- Single binary; no external KMS dependency required to boot (a KMS may be added later).
- Server must **refuse to start** if the master key is missing or malformed.
- The integrity tag must resist offline brute-force of guessable values (emails, ids, tokens) in long-retained audit rows — so it must be a **keyed** MAC, not a plain hash.

## Decision

### Algorithms
- **AEAD at rest:** AES-256-GCM.
- **Integrity tag (`payload_digest`):** HMAC-SHA-256.
- **Key derivation:** HKDF-SHA-256.
- **Password hashing:** Argon2id.

### Key hierarchy
- **Master key (KEK)** — 32 random bytes, carries a `key_version`.
  It never encrypts row data directly.
- **Derived purpose keys** via HKDF-SHA-256 (salt = nil, sub-key length 32 bytes) from the KEK with distinct `info` labels — the exact label strings, versioned so a future derivation change coexists with old data:
  - `info="portcullis/payload-integrity/v1"` → the HMAC-SHA-256 key for `payload_digest` (also keys the CSRF token MAC, ADR-0006).
  - `info="portcullis/dek-wrap/v1"` → the key-wrapping key for data-encryption keys.
- **Data encryption keys (DEK)** — a fresh 256-bit key per encrypted unit (per result snapshot; per request payload).
  The DEK encrypts the data with AES-256-GCM; the DEK is wrapped by the `dek-wrap` key.
  GCM associated data binds record identity (below) to prevent swapping.

### Envelope format (v1 — as implemented in `internal/infra/crypto`)
Stored per record as `Blob`:
| Field | Content |
|---|---|
| `key_version` | KEK version whose `dek-wrap` key wrapped the DEK (int, ≥ 1; M1 keyring loads historical versions and an active version) |
| `wrapped_dek` | `wrapNonce (12B) ‖ AES-256-GCM(dek-wrap key, DEK)` — nonce prefixed, one field |
| `nonce` | 12-byte data nonce |
| `ciphertext` | `AES-256-GCM(DEK, plaintext)` |

- Every nonce is a fresh **12-byte CSPRNG value** (standard GCM nonce size; random nonces are safe far below NIST's 2^32-invocations-per-key bound because each DEK encrypts one unit).
- **Both layers** (DEK-wrap and data) authenticate the **same associated data**; a mismatch on either open fails closed (`ErrDecrypt`, no detail).

### Canonical associated data (AAD)
One layout for every envelope, UTF-8, `|`-separated (safe: every field is a fixed token, UUID, or integer — no escaping needed):

```
portcullis/aad/v1|<record_type>|<organization_id>|<record_id>[|<chunk_index>]
```

- `record_type` — fixed lowercase token per table, e.g. `connection_credential`, `request_payload`, `saved_query_version`, `result_chunk`, `schema_artifact_file`, `oidc_pending` (the OIDC pending-auth cookie, ADR-0007).
- `organization_id` / `record_id` — canonical lowercase UUID text of the owning org and row.
- `chunk_index` — present only for chunked payloads (result chunks, artifact files), decimal.
- **Result chunk manifest** (amended 2026-10-04): a `result_chunk` AAD continues `|<chunk_index>|m1|<row_count>|<truncated 0/1>|<chunk_count>`, where `chunk_count` = 1 schema chunk + ⌈row_count / 100⌉ row chunks.
  The plaintext `result_sets` metadata a reader trusts for paging and CSV export is therefore authenticated by every chunk: changing the row count, truncation flag or implied chunk count makes every open fail and the read reports `result unavailable`.
  The `result_set` DEK-wrap AAD is unchanged, so `key rotate` still rewraps result keys from identity alone.
  Snapshots sealed before the manifest no longer open; they are refused as unavailable and expire within the 15-minute result TTL, so no migration is needed.
- The KEK `key_version` is *not* in the AAD: it is bound implicitly — it selects the HKDF-derived wrap key, so a tampered version fails the unwrap authentication.
  (PRD §8.1's "key ID" binding is satisfied by this mechanism.)

### Provisioning
- Supplied as a 32-byte key, base64-encoded, via **either** `PORTCULLIS_MASTER_KEY` **or** `PORTCULLIS_MASTER_KEY_FILE` (a mounted file — preferred for production).
  Setting **both** is refused: a stale env var silently overriding a mounted file could re-encrypt with the wrong key and orphan data.
- **File format:** the file's entire contents are the **standard base64** (padded ok) of the 32 raw bytes; surrounding whitespace/newline is trimmed.
  Nothing else — no PEM, no JSON.
- On boot: decode, validate length (exactly 32 bytes after decode), and load.
  Missing or malformed → refuse to start.
  With no historical versions the active key is **version 1**.
- **Multi-version provisioning (with the rotation CLI):** the active key stays in `PORTCULLIS_MASTER_KEY`/`_FILE`; retired-but-still-decrypting versions are supplied as `PORTCULLIS_MASTER_KEY_PREVIOUS` — comma-separated `<version>:<base64>` entries (or the `_PREVIOUS_FILE` variant with one `<version>:<base64>` per line).
  The active version number is `max(previous versions) + 1`.
  Historical versions must be consecutive ascending entries starting at 1; duplicate versions or key material, missing versions, and reuse of the active key refuse to start.

### Rotation
- Multiple KEK versions coexist; the **highest version is active** for new writes, older versions remain available for decrypt/unwrap during transition.
- A `key rotate` CLI re-wraps DEKs and re-encrypts request payloads to the new version as an **eager batch** (decided 2026-07-04; no lazy-on-read path).
  Eager keeps completion observable — the CLI reports "0 rows on old encryption versions" only after a database-wide count verifies every credential, request payload and result wrapper uses the active encryption version. The completion check includes all organizations and versions newer than the loaded active key; unresolved rows or a failed count refuse completion (2026-10-03 review correction). Historical KEKs remain necessary for immutable approval/audit HMAC verification, so M1 does not support destroying them solely on that count.
  The batch selects one organization with eligible old envelopes, then locks at most 100 records of each kind with explicit organization predicates. Repeat organization discovery until none remains; credentials precede request payloads and result wrappers, and updates plus the selected organization's audit evidence commit atomically. The administrative identity-selection exception is documented in ADR-0004; there is no customer cross-org read API. A corrupt batch still rolls back and refuses progress; bounded recovery beyond corrupt records requires separate work. The batch is resumable (keyed by `key_version < active`) and throttled; reads during rotation work throughout because all versions stay loaded.
- `payload_digest` carries its `key_version`; verification uses the version recorded on the row.
- **Implementation status:** M1 implements the multi-version keyring and resumable `key rotate` CLI. Credential/request envelopes are re-encrypted and result DEKs rewrapped in locked, audited batches. Stop serving writers during the operational key switch; see [rotation runbook](../operations/key-rotation.md).
- **Operational note (CSRF, amended 2026-10-04):** the session CSRF token (ADR-0006) leads with the `key_version` whose payload-integrity key produced its HMAC and is verified under that version, so rotating the master key keeps outstanding sessions usable while historical versions stay loaded.
  A token naming a version the process has not loaded (a rollback to an older keyring) or a pre-versioning token answers `Unauthenticated` and the user signs in again.

### Password hashing (Argon2id)
- Store the Argon2id variant/version, salt, and cost parameters with each hash, PHC-encoded: `$argon2id$v=19$m=<KiB>,t=<time>,p=<threads>$<b64raw salt>$<b64raw hash>` (base64 raw/unpadded, std alphabet).
- **v1 profile (complete):** m = 64 MiB (`65536` KiB), t = 3, p = 2, **salt = 16 bytes (CSPRNG), key/tag = 32 bytes**.
  Revisit with a benchmark; a profile change triggers rehash-on-login.
- **Validation bounds** (enforced before hashing/verification — out-of-range params would panic Argon2 or exhaust memory): t ∈ [1, 10]; p ∈ [1, 16]; m ∈ [8×p KiB, 256 MiB]; salt ∈ [8, 64] bytes; key ∈ [16, 64] bytes.
  Out-of-range stored hashes fail verification (`ErrBadHash`), never execute.
- **Concurrency cap:** at most `PORTCULLIS_ARGON2_MAX_CONCURRENT` hashes in flight (default **2**, valid range **[1, 256]** — 256 already permits ~16 GiB of hashing memory); a semaphore with deterministic cancellation (an already-cancelled request never starts a hash).
  Each hash costs ~m bytes, so the cap bounds hashing memory independently of cores.
- On successful login, if the stored parameters differ from the current profile, **rehash**.

### Loss-recovery posture
- If the KEK is lost and no backup exists, encrypted connection credentials, request payloads, and result snapshots are **unrecoverable** — this is accepted and must be documented.
- Audit **integrity tags and metadata survive** key loss by design (they are derived/stored separately and are not the only copy of operational history).
  Operators are told to back up the KEK in a secret manager and to keep metadata-DB backups.

### Compose demo key provisioning (2026-10-03)

The one-shot initializer uses a private temporary file and atomic no-replace hard-link publication. Concurrent initializers must preserve the winning key. An existing empty, malformed or symlink key is refused, never regenerated. Set the key to mode `0400` and its directory to `0700`, owned by UID/GID `65532` to match the distroless nonroot runtime; do not log key material. Apply ownership/permission repair to valid existing demo keys without changing their bytes. Mount the persistent volume read-only in the serving process. Production remains responsible for protected secret provisioning and backups.

`make keygen-check` exercises concurrent initialization, nonroot readability, unrelated-user denial, repeated byte preservation, corrupt/empty key refusal and symlink refusal in a disposable tmpfs using the exact Compose keygen image. Never execute this test on an operator secrets volume. See [Docker secret injection](https://docs.docker.com/compose/how-tos/use-secrets/) (checked 2026-10-03).

Organization traversal retains the existing transaction and row-lock ordering; it does not use `SKIP LOCKED` to infer completion. Standard references rechecked 2026-10-03: [OWASP key management lifecycle](https://cheatsheetseries.owasp.org/cheatsheets/Key_Management_Cheat_Sheet.html) and [PostgreSQL row-level locking](https://www.postgresql.org/docs/current/explicit-locking.html).

## Consequences
- A small `crypto` package owns: KEK loading/validation, HKDF purpose-key derivation, `Seal/Open` (AES-256-GCM with associated data), DEK wrap/unwrap, `payload_digest` compute/verify, and Argon2id hash/verify/needs-rehash.
  It is the only place that touches key material.
- Rotation is operationally a CLI plus a batch re-encrypt pass; designing tables to carry `key_version` from day one keeps rotation non-breaking.
  (Eager-batch execution decided above, 2026-07-04.)
