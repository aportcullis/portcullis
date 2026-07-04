# ADR-0012: Schema governance operations — Atlas pin, artifact store, apply safety

- **Status:** Accepted — model and formats fixed; the exact Atlas patch version and the
  timeout numbers are **provisional until the Schema-milestone (M5) integration work**, which
  re-confirms them against the compatibility suite.
- **Date:** 2026-07-04

## Context
The PRD's Schema milestone (§4.5, §8.1, §12.2) requires four operational decisions before
implementation: the Atlas Community pin/distribution policy, where migration artifacts live
(with size caps and retention), the `schema_apply_timeout` default, and how the per-connection
migration lock recovers from a dead server. Without them, two implementations of §4.5 would
diverge on every one of these.

## Decision

### Atlas Community pin & distribution
- **Edition:** Atlas **Community Edition** only (Apache-2.0; built from the public repo). No
  Pro/EULA features — the license map in PRD §5.5 is binding.
- **Pin policy:** the schema-enabled image bundles one exact Atlas Community version
  (`arigaio/atlas:<X.Y.Z>-community` digest-pinned, or the checksummed release binary).
  Baseline at decision time: the **v1.2 line** (current as of 2026-07-04); the exact patch is
  pinned when M5 integration starts and recorded here by amendment.
- **Upgrade rule:** Renovate proposes bumps; a bump merges only when the DB-object
  **compatibility matrix suite** (per-engine contract tests, PRD §4.5) passes against the new
  binary. The image build records the binary's SHA-256; Apache-2.0 NOTICE ships in the image.
- Invocation is always through the `SchemaEngine` interface (PRD §5.4) — subprocess only,
  `migrate status` / `migrate apply --dry-run` / `migrate apply`.

### Migration artifact store
- **Backend: the metadata PostgreSQL** — no new infrastructure. Tables (Schema milestone
  migration): `schema_artifacts` (id, organization_id, remote id, commit SHA, path, Atlas
  version/options, `atlas.sum`, file count, total bytes, retention timestamps) and
  `schema_artifact_files` (artifact_id, ordinal, filename, checksum, AEAD-encrypted content).
  File content uses the ADR-0003 envelope; AAD record type `schema_artifact_file` with the
  artifact id + ordinal as the record identity (ADR-0003, AAD).
- **Caps (fetch-time, fail the request — never truncate):** per file **1 MiB**, per artifact
  **10 MiB** and **500 files**; git operation timeout **60s** *(provisional)*. Submodules,
  LFS, and symlinks are rejected (PRD §8.1).
- **Retention:** the artifact (ciphertext) lives while its `schema_change_request` is
  non-terminal, **plus 90 days** after the terminal transition — the same window as request
  payload retention (PRD §8.4) so review evidence outlives the decision. After that the
  ciphertext is deleted; checksums, `atlas.sum`, metadata, and audit events are kept
  indefinitely (they prove *what* was applied without retaining the SQL).

### Apply timeout
- `schema_apply_timeout` **default 10 min, ceiling 60 min** *(provisional)*, configurable per
  connection alongside the query limits in `connection_policy_versions`. Applies to the whole
  `migrate apply` subprocess; on expiry the subprocess is killed and the step records
  `outcome_unknown` (PRD §4.4 rules — no auto-retry, operator reconciles).

### Migration lock & recovery
- **Lease row, not a session advisory lock.** Reuse the execution-lease pattern (PRD §4.4):
  a per-connection lock row carrying `owner` (server instance id), `deadline`, `heartbeat`,
  acquired by conditional update; heartbeat every **15s**, deadline slides to
  `now() + 60s` on each beat *(provisional — same numbers as the query-execution lease,
  PRD §4.4)*.
- Why not `pg_advisory_lock`: an apply can run for minutes; a session lock would pin a pooled
  connection for the duration and silently vanish if the process dies mid-apply, leaving no
  audited owner. The lease row survives, names the owner, and recovers deterministically.
- **Recovery:** the same reconciler that expires query-execution leases expires stale schema
  locks (deadline passed → lock released, in-flight step → `outcome_unknown`, audit event,
  no auto-retry). Late completion is fenced by the identical
  `owner + attempt_id` conditional-update rule (`LATE_COMPLETION_OBSERVED` on mismatch).

## Consequences
- M5 needs no further ops ADR: artifact schema, caps, retention, timeout, and lock recovery
  are all pinned; integration only fills in the Atlas patch pin (amendment here).
- Reusing the lease/reconciler machinery keeps one failure model across query execution and
  schema apply — one set of invariants to test.
- Storing artifacts in the metadata DB keeps the single-binary/no-extra-infra promise; the
  caps bound its growth and retention bounds its lifetime.

## Sources (checked 2026-07-04)
- Atlas Community Edition (Apache-2.0 scope, `--community` install, image tags):
  https://atlasgo.io/community-edition
- Atlas `migrate apply` / `migrate status` CLI:
  https://atlasgo.io/cli-reference
