# ADR-0017: Runtime settings store (operator-tunable operational config)

- **Status:** Accepted
- **Date:** 2026-07-20

## Context
ADR-0010 pinned every operational number (timeouts, backoff windows, log verbosity) as an env var **(config)** or a compile-time constant, with one explicit escape hatch: "promoting one to config is a small change but must update this table … no config surface until a real need appears."
That need is now here — operators want to tune operational values **without a redeploy or restart**.

PRD §8.4 already states "모든 관리자 설정 변경도 audit 대상" (every admin settings change is audited), so the PRD assumes a runtime admin-settings surface; it never defines one.
This ADR fills that gap.
No PRD amendment is needed — the hook already exists.

The move is deliberately narrow.
Config splits into three tiers, fixed here:

- **Tier A — safety invariants.** Classification allow-list (ADR-0002), redaction contract and placeholder format (ADR-0016).
  These are **never** settings: a runtime-mutable safety policy is a governance tool that can disarm itself from its own admin screen, and a DB/admin compromise would then weaken the very gate that protects the target databases.
  They stay compile-time (code.md minimize-hardcoding: fixed domain enums stay in code; ADR-0002/0016 fixture-pinned).
- **Tier B — bootstrap & secrets.** `DATABASE_URL`, `MASTER_KEY(_FILE)`, `RUNTIME_ROLE`, `MIGRATE_DATABASE_URL`, `ADDR`.
  Chicken-and-egg (you need them to reach and decrypt the DB) or secret material.
  They stay env (12-factor deploy config; PRD §11 image-as-single-source-of-truth).
- **Tier C — operational tunables consumed per-operation.** This ADR moves these into a DB-backed store, live-mutable, with env as the seed/fallback.

## Decision

### The tunable set (Tier C) — pinned
Moves into the settings store (live-mutable):
- `log_level`
- `login_backoff_threshold`, `login_backoff_base`, `login_backoff_cap` (ADR-0006 Parameters)
- `connection_test_timeout` (ADR-0014)
- `execution_lock_timeout` (ADR-0021, default 5s, range [1s, 60s])
- the M1 query-execution tunables as they land — query timeout (PRD §8.2), max result rows/bytes, and the pgdialect execution timeouts currently hardcoded.

Stays env, by design (lifecycle-read-once or rebuild-required — live mutation has no value or no effect):
- `addr` (bind happens once), `argon2_max_concurrent` (sizes a startup semaphore), `drain_delay` / `shutdown_timeout` (read only on the shutdown path), the `database_*` metadata pool bounds (amended 2026-10-04: they size the pool that reads this store, so they are bounded in `platform/config` per ADR-0010), and all of Tier A/B.

A new tunable declares its tier when introduced; Tier C entries are added to the descriptor registry below.

### Storage — `settings` table + one code-defined descriptor registry
- Table `settings(organization_id, key, value, updated_at, updated_by, version)`, org-scoped (ADR-0004 RLS; the single-org MVP resolves to the default org).
  `key` is the exact dotted config key; `value` is text; `version` gives optimistic concurrency (as connections do, ADR-0014).
- Each Tier-C key has a **code-defined descriptor**: logical type, bounds, default, and validator — reusing the **same** validators and bounds as `internal/platform/config` (e.g. `connection_test_timeout ∈ [1s, 60s]`, `login_backoff_cap ≥ login_backoff_base`).
  One registry, shared by env bootstrap and the DB store, so the two paths cannot drift (the same reason ADR-0010 keeps one copy of the log-level vocabulary).
- **Which keys exist is code** (a fixed operational vocabulary, like RBAC verbs); **their values live in the DB**.
  This matches minimize-hardcoding: the catalog is code, the mutable data is DB.
- An **absent row means "use the default"**, so the table only ever stores explicit overrides.

### Precedence & fallback — one rule
Effective value = **DB override, if the row is present and valid → else the env seed → else the compiled default.** DB wins so the admin UI actually changes behavior.

Validation is applied twice and always fails to a *safe* value, never a bad one:
- **On write:** the RPC validates against the descriptor; an out-of-range value is rejected and never persists.
- **On read/load:** a row that fails validation (e.g. after a later bounds tightening) is ignored — the setting falls back to env/default and a warning is logged.
  The server never adopts an out-of-range value.

If the settings store is unreadable at boot, the server starts on env/defaults and logs it (availability; Tier B already gates real safety), and recovery from a bad persisted value is `portcullis settings reset <key>` or fixing the row — there is deliberately **no** second env-overrides-DB mode, keeping precedence a single rule.

### Change propagation — LISTEN/NOTIFY + reconcile
- Readers see an **atomic in-memory snapshot** behind a consumer-defined port (`settings.Reader`), O(1) per access; no per-request DB hit.
- A write commits the row and `NOTIFY settings_changed` (payload = the key, a **signal only**).
  A dedicated long-lived listener connection `LISTEN`s the channel and, on notify, re-reads and atomically swaps the snapshot.
  The table is the source of truth; the notification is only the nudge (web-verified pattern).
- **Notifications are not durable** (missed while a listener is offline), so a periodic **reconcile** (60s) re-reads the table and a full read runs on every listener (re)connect — a missed `NOTIFY` self-heals.
  Fan-out to multiple instances is native, so this scales past the single-instance MVP without redesign.
- `log_level` specifically requires backing the slog handler with `slog.LevelVar` (an atomic level settable at runtime); today `HandlerOptions{Level: lvl}` fixes it at construction.

### Authorization & audit
- Changing a setting requires a new catalog permission `settings.update` (ADR-0008 `resource.verb`, seeded in the SQL `permissions` catalog; reading uses `settings.get`/`settings.list`).
  Roles stay data; nothing checks a role name.
- Every change writes an append-only `SETTING_UPDATED` audit event (a new `audit.Action`) in the **same transaction** as the row (ADR-0009), recording key, old→new value, and actor — satisfying PRD §8.4.
  These values are operational numbers, not secrets, so they are recorded verbatim (unlike SQL/params, which are redacted/encrypted).

### Out of scope (explicit)
Tier A safety invariants and Tier B bootstrap/secrets; per-connection policy (ADR-0015 `connection_policies` is its own store); general feature flags.
This store is for **global/org operational numbers only**.

## Consequences
- ADR-0010's **(config)** marker for the Tier-C keys now means "env seed + DB override"; ADR-0010's table gains a footnote pointing here, and `.env.example` keeps those keys as seeds.
- New surface to build: a `settings` migration, a `settings` domain port + descriptor registry, a Postgres store + listener/reconcile adapter, the RPCs and an admin SPA screen, the `settings.update` permission row, and the `SETTING_UPDATED` audit action.
- Validators live once and are shared by config bootstrap and the settings store — no drift, which is the property ADR-0010 valued.
- Accepted risk: a brief propagation lag (NOTIFY latency, or up to the reconcile interval on a miss) between a change and all readers observing it.
  Fine for operational tunables; this store is **never** consulted for a safety decision (those are Tier A, in code).

## Sources (checked 2026-07-20)
- PostgreSQL LISTEN/NOTIFY — transactional, non-durable, dedicated long-lived connection, "signal not payload" pattern: https://www.postgresql.org/docs/current/sql-notify.html · https://www.cybertec-postgresql.com/en/listen-notify-automatic-client-notification-in-postgresql/
- Twelve-Factor "Store config in the environment" — env is deploy config; internal runtime settings are a separate category (DB + audit/versioning is a recognized pattern): https://12factor.net/config
- Go `slog.LevelVar` for a runtime-settable log level: https://pkg.go.dev/log/slog#LevelVar
