# ADR-0010: Runtime & transport defaults

- **Status:** Accepted
- **Date:** 2026-07-04

## Context
A reimplementation of Portcullis from the PRD and ADRs alone would converge on the security *model* but diverge on the operational *numbers*: HTTP timeouts, request caps, rate-limit budgets, boot ordering, exit codes.
Those values were chosen deliberately during the M0 hardening reviews but lived only in code.
This ADR records every such default as the binding contract, so the documentation fully determines the implementation.
Values marked **(config)** are operator-tunable env vars; everything else is a compile-time constant by design (no config surface until a real need appears — see `docs/conventions/code.md`, minimize-hardcoding: these are fixed operational guards, not domain catalogs).

## Decision

### HTTP server (`internal/transport/server`)
| Setting | Value | Rationale |
|---|---|---|
| `ReadHeaderTimeout` | **10s** | slow-header (slowloris) bound |
| `ReadTimeout` | **30s** | whole-request read bound; auth/health bodies are tiny |
| `IdleTimeout` | **120s** | reap idle keep-alives |
| `WriteTimeout` | **unset — deliberately** | a blanket write deadline would kill future long-lived Connect server-streams; per-request deadlines via `http.ResponseController` when streams land |
| `MaxHeaderBytes` | **64 KiB** | vs net/http's 1 MiB default; cookies + metadata only |
| Request body cap | **64 KiB** | Connect defaults to *unlimited*; enforced twice — `connect.WithReadMaxBytes` (per message) and `http.MaxBytesHandler` (whole stream) |

- **The interactive bucket is sized from genuine traffic, and a throttle is not an outage** (amended 2026-07-26): the non-credential bucket (`Me`/`Logout`/`GetConfig` and every authenticated read or mutation) exists to shed a garbage-session flood before the per-request DB session lookup — **not** to pace legitimate use; that is the credential bucket's job.
  At 60 tokens refilling 1/s it did pace legitimate use: one SPA action fans out to several RPCs (opening a dialog lists targets; approving mutates and re-reads the page), so ordinary browsing drew `ResourceExhausted`, and a shared office IP multiplies it.
  OWASP's DoS guidance is to baseline real traffic before choosing a threshold, so the floor is now a test rather than a guess — `TestOrdinaryInteractionBurstIsNotThrottled` pins that ~20 actions' worth of fan-out passes, `TestAuthenticatedProcedureRateLimited` pins that a flood is still shed — and the numbers move to **240 burst, 5/s sustained** per IP.
  The credential bucket (10 burst, 1 per 3s, plus the per-account bucket, now keyed by email and client IP) is untouched: brute force is a different threat with a different rate.
  A shed request also reads differently now.
  It carries "too many requests, retry in a moment" instead of the login message (a user who merely clicked quickly was being told their credentials were throttled), and the SPA's boot-time reads retry `ResourceExhausted` with doubling backoff (`web/src/shared/api/retry.ts`) instead of rendering it as "the server could not be reached" — that card was a dead end with a manual Retry button for a condition that clears in a second.
  Everything else still surfaces immediately: an `Unauthenticated` is an answer, an `Unavailable` is an outage, and retrying either would only delay the truth.
- **The body cap is the ceiling every payload limit lives under** (amended 2026-07-26): a domain limit above it cannot be reached — the handler refuses before any validator runs — so the access-request payload budget (56 KiB over SQL + parameter names and values, ADR-0018) and PRD §4.2's saved-query rule both derive from this number rather than restating one.
  Note the two enforcement points differ in what they measure: `MaxBytesHandler` bounds the wire stream, which a compressible body can slip under, while `WithReadMaxBytes` bounds the decompressed message.
  Transport tests must mount **both**, or they exercise a server that accepts what production refuses.

- Routes: `GET /livez`, `GET /readyz`, Connect handlers at their generated paths, SPA fallback at `/` (unknown paths serve `index.html`; before the frontend is built, a placeholder page).
- Graceful shutdown: readiness flips to *draining* first, waits `drain_delay` (**default 0s**, capped at **5 min**, (config) `PORTCULLIS_DRAIN_DELAY`) so Kubernetes deregisters the pod, then `http.Server.Shutdown` bounded by `shutdown_timeout` (**default 15s**, (config) `PORTCULLIS_SHUTDOWN_TIMEOUT`, must be > 0).
  Execution interruption is carved out of that budget (amended 2026-10-04): `shutdown_interrupt_timeout` (**default 5s**, (config) `PORTCULLIS_SHUTDOWN_INTERRUPT_TIMEOUT`, positive and shorter than `shutdown_timeout`) is reserved for interrupting executions and recording their outcomes, and the HTTP drain gets the rest. The whole shutdown therefore takes at most `drain_delay + shutdown_timeout`; keep that below the orchestrator's grace period (Kubernetes `terminationGracePeriodSeconds` defaults to 30s), or interruption audits can be killed mid-write.
  The delay and the timeout are **sequential** budgets.
  Requests that outlive `shutdown_timeout` no longer keep running while the process exits (amended 2026-10-04): the server derives every request context from its own base context, runs registered shutdown-timeout hooks such as audited execution interruption, then cancels that base context. `http.Server.Shutdown` only waits for handlers and returns the context error on timeout; it never cancels them ([net/http Server](https://pkg.go.dev/net/http#Server.Shutdown)).
  `drain_delay` is capped (amended 2026-07-05) because it blocks shutdown before draining even begins — an unbounded typo ("2h") would hang past any orchestrator's grace period.
- **Second signal force-quits** (amended 2026-07-05): after the first SIGINT/SIGTERM begins the drain, signal handling is restored (`signal.NotifyContext`'s `stop` is called immediately, per the os/signal guidance) so a **second** signal terminates the process with the default behavior instead of being swallowed for the whole drain window.

### TLS termination & security headers (added 2026-07-05)
- **TLS is a deployment prerequisite, terminated upstream.** The server speaks plain HTTP (`ListenAndServe`, no in-process TLS) and is designed to sit behind a TLS-terminating reverse proxy / ingress.
  This is load-bearing, not incidental: session/CSRF cookies are `__Host-` + `Secure` (ADR-0006), which browsers accept **only over HTTPS** — served over cleartext to the browser, login silently fails because the cookie is never stored.
  Operators must terminate TLS in front of Portcullis.
- **HSTS is the proxy's responsibility** — it owns the TLS edge — so the app does not emit `Strict-Transport-Security`.
- Every response carries hardening headers as defense-in-depth (cheap, valid even behind the proxy): `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: strict-origin-when-cross-origin`, and CSP `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'`.
  Inline style compatibility does not require omitting script restrictions: `script-src` independently restricts the embedded SPA to same-origin JavaScript and excludes inline/eval scripts; `object-src` refuses plugin content. This is a source allowlist, not a nonce/hash-based strict CSP or complete XSS protection.
  `default-src 'self'` (amended 2026-10-04) makes every resource class without its own directive (fonts, frames, media, workers, manifests) same-origin, so injected markup cannot load or beacon to a foreign origin; `connect-src 'self'` covers the SPA's Connect fetches; `img-src 'self' data:` covers the bundled brand SVGs and bundler-inlined data images (profile identicons are inline SVG elements, ADR-0041); `style-src 'self' 'unsafe-inline'` is stated explicitly because `default-src` would otherwise refuse the inline style attributes the SPA and placeholder rely on. The UI uses system font stacks, so no font origin is needed. The CSV download is a same-document `blob:` link navigation, which fetch directives do not govern.
  Validate the production embedded bundle with browser scenarios that retain normal app behavior (same-origin and data images, inline styles, same-origin fetches), refuse a harmless injected inline script, and report foreign script, image, fetch and font violations under their own directives. References checked 2026-10-03: [MDN script-src](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Security-Policy/script-src) and [OWASP CSP](https://cheatsheetseries.owasp.org/cheatsheets/Content_Security_Policy_Cheat_Sheet.html); rechecked 2026-10-04: [MDN default-src](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Security-Policy/default-src).

### Request correlation (`internal/platform/logging`)
- Header `X-Request-Id` is accepted only when **non-empty, ≤ 128 bytes**, and matches the charset `[A-Za-z0-9._:/-]` exactly; anything else is **replaced** with a fresh 8-byte-hex id — never truncated, so two attacker-controlled values can't collide by prefix.
- The id is echoed in the response header, logged on the one-line-per-request log, and copied into audit rows.
  One structured line per request: request_id, method, path, status, bytes, duration, socket remote (forwarded headers deliberately ignored in logging — proxy trust is decided at the interceptor boundary only).
  Health-probe requests log at debug.

### Client IP resolution & interceptor chain (`internal/transport/connectapi`)
- Interceptor order is fixed: **ClientIP → RateLimit → Auth**.
  The IP is resolved once and shared via context (rate limiting and audit use the same value); rate limiting runs before auth so a login flood is shed before any Argon2 hashing.
- `clientIP`: with no trusted proxies (default), the direct peer is the client.
  When the peer is inside `PORTCULLIS_TRUSTED_PROXIES` (config, comma-separated CIDRs), walk `X-Forwarded-For` **right-to-left** (nearest hop first) and return the **first address that is not itself a trusted proxy**.
  Right-to-left is load-bearing: a client can prepend spoofed entries on the left, but everything from the real connection rightward was written by trusted proxies.
- **Malformed hops fall back to the peer** (amended 2026-07-05).
  A walked entry that does not `net.ParseIP` cannot be a real client address — a trusted proxy could forward a malformed/oversized value, or an attacker prepends one past the known hops.
  Returning it verbatim would let a spoofed `X-Forwarded-For` mint an arbitrary, unbounded rate-limit key (and a bogus audit `source_ip`); instead, stop trusting the chain at the malformed hop and attribute the request to the peer.
  (Supersedes the earlier "unparsable entries are returned as the client key".)
- Config rejects a `/0` trusted-proxy CIDR at startup: "trust everything" makes XFF fully spoofable and defeats per-IP limiting — always a misconfig.
- **Shared session pipeline** (amended 2026-10-04): the unary Auth interceptor and the stream security interceptor call one function for session lookup, cookie/header CSRF equality, HMAC verification and the post-CSRF idle slide, so both transports keep the ADR-0006 ordering and error mapping; the stream adds only its admission bucket, 30-minute lifetime and periodic revalidation.

### Server-fault logging (`internal/transport/connectapi`, amended 2026-10-04)
- The error log interceptor wraps unary **and streaming** handlers outermost and writes one `rpc failed` line per server fault (`Internal`, `Unavailable`, `Unknown`, `DataLoss`) with procedure and code; client faults and successes are not logged.
- Mapping an unexpected error to a generic wire message keeps the root cause reachable through `errors.Unwrap`, so the log line classifies it as the innermost error type, any SQLSTATE (through the driver's `SQLState()` method, without importing the driver into transport), and `canceled`/`deadline_exceeded`.
  Error messages are never logged or sent, because driver and storage messages can echo SQL, parameters, DSNs or credentials (security conventions); this is the same rule as the PostgreSQL adapter's `ErrorLogFields`.
- Stream handler panics propagate to the shared recover option, which logs procedure and panic type exactly as for unary handlers and returns a generic `Internal`.

### Rate limiting (`internal/transport/connectapi`)
For the **public** procedures (Bootstrap, Login) two **independent** token-bucket stores — keyed by client IP and by **(normalized email, client IP)** — must both admit the request, **before any hashing**; over limit → Connect `ResourceExhausted`.
The account bucket is keyed per client (amended 2026-10-04): a bucket keyed by email alone let anyone keep a named account, including the sole admin, throttled by sending one login every refill interval from anywhere.
Cross-IP guessing against one account is bounded by the database-backed account backoff instead, as OWASP recommends associating the failure counter with the account rather than the source IP; its residual lockout risk is recorded in ADR-0006.
The IP key is canonicalized via `net.ParseIP(...).String()` (IPv6 spellings of one address share a bucket).

**Authenticated procedures are throttled too** (amended 2026-07-05): Me/Logout carry no email, so a **third, more generous per-IP store** guards them, so a flood of requests carrying garbage session cookies cannot drive unbounded DB session lookups through the Auth interceptor.
It is more generous than login because one SPA interaction fires several RPCs.

**Key-byte bound** (amended 2026-07-05): every bucket key, regardless of dimension, is capped — a key over `maxRateLimitKeyBytes` (320) is SHA-256-collapsed before use, so no attacker-influenced key (a spoofed/malformed `X-Forwarded-For`, a future per-token key) can hold large strings.
The bucket-count cap bounds the map size; this bounds each entry's key size.

| Parameter | Login/Bootstrap (per IP; per email + IP) | Authenticated (per IP) |
|---|---|---|
| burst | **10** tokens per IP; **5** per email + IP (the default backoff threshold) | **240** tokens |
| refill | **1 token / 3s** | **5 tokens / 1s** |
| idle-bucket TTL | **10 min** | **10 min** |
| max buckets per store | **50,000** — sweep idle first, then evict least-recently-seen | **50,000** |
| max key bytes | **320** (over → SHA-256) | **320** |

Process-local by design (single-instance MVP, ADR-0009); shared per-account counters arrive with the progressive-backoff work (parameters pinned in ADR-0006, Parameters).
The authenticated-procedure numbers are *provisional* — re-checked against real SPA traffic after MVP.

### Startup sequence (`cmd/portcullis`)
Ordered, each step fail-fast; any failure exits **1** (the only non-zero exit code — cleanup runs via deferred calls inside `run()`):
1. Load config (validated: enumerated log level/format **reject unknown values** — no silent fallback; bounded numerics reject out-of-range; addr must be non-empty, bare port `8080` normalized to `:8080`, default **`:8080`**).
2. Build logger.
3. Load keyring — **refuse to start** without a valid master key (ADR-0003).
4. Require `PORTCULLIS_DATABASE_URL`.
5. Migrate before the startup budget (amended 2026-10-04): migration runs on its own context because `Migrate` bounds its migration-lock wait, lock acquisitions and statements itself (ADR-0009), so a peer holding the migration lock can no longer consume the startup budget.
   The **30s startup context** is opened after this step and covers everything from the runtime pool up to serving.
6. Migrate — *conditionally* (amended 2026-07-18, external review): the recommended production shape runs migrations as the **one-shot `portcullis migrate` command** in a separate process/container that is the only holder of the owner DSN, so the serving process never carries owner credentials in env or memory (OWASP Database Security: the application account must not own the schema).
   `serve` migrates at startup only when the explicit tri-state `PORTCULLIS_STARTUP_MIGRATE` says so, or — when it is unset — when `PORTCULLIS_MIGRATE_DATABASE_URL` is set on this process (compatibility with the previous deployment shape).
   Deliberately DECOUPLED from `PORTCULLIS_ALLOW_PRIVILEGED_RUNTIME` (self-review 2026-07-18): a security-debug flag must not silently change who migrates the schema; single-role dev/e2e opts in explicitly with `STARTUP_MIGRATE=true`.
   When it does migrate, it uses a short-lived pool: ping first (so connect errors get a generic, DSN-safe message), apply, close.
   Migration/DB errors are logged with classified fields only; raw errors can echo the DSN (password).
   Otherwise startup migration is skipped and an unmigrated database fails step 7 with a `portcullis migrate` hint.
7. Open the runtime pool, ping, then `VerifyRuntimeConnection` (ADR-0009); `PORTCULLIS_ALLOW_PRIVILEGED_RUNTIME=true` downgrades *only* over-privilege violations.
8. Wire adapters (composition root), mount handlers, register the metadata-DB readiness check, serve.
   A bind/serve failure is delivered on a channel and selected against the signal context — it terminates with exit 1, never the graceful path.

### Metadata migrations & advisory locks (`internal/infra/postgres`)
- Migrations: embedded `migrations/*.sql`, applied in **lexical filename order**, each file in its own transaction, recorded in `public.schema_migrations(version text primary key, applied_at timestamptz not null default now())` (`version` = filename minus `.sql`).
  The whole run is serialized by a **session-level advisory lock held on one dedicated connection**; that session first `DISCARD TEMP`, then pins `search_path = public` (`pg_catalog` remains implicitly first) rather than inheriting a role/DSN setting; role preflight/postflight per ADR-0009.
  Amended 2026-10-04: the lock is polled for a bounded wait (default **2m**), each file runs with `SET LOCAL lock_timeout` **5s** (retried up to five times) and `statement_timeout` **15m**, and `schema_migrations.checksum` (0019) lets the runner refuse edited released files and unknown versions (ADR-0009).
- Advisory-lock keyspace (first arg of the two-arg `pg_advisory_*` family; second arg = object id, 0 when global): class **1** = bootstrap (global), **2** = per-user session rotation, **3** = migration (global).
  New classes are appended here, never reused.

### Metadata connection pool (added 2026-10-04)
`postgres.Open` parses the DSN with `pgxpool.ParseConfig` and bounds the pool instead of taking pgx defaults (no session timeouts, CPU-derived size, unbounded acquisition waits).
Boot-only configuration, validated at load and not a settings-store tunable (ADR-0017):

| Setting | Default | Range | Applied as |
|---|---|---|---|
| `PORTCULLIS_DATABASE_MAX_CONNS` | **16** | [2, 200] | `pgxpool.Config.MaxConns` |
| `PORTCULLIS_DATABASE_ACQUIRE_TIMEOUT` | **10s** | [100ms, 1m] | deadline on each pool acquisition (pgxpool acquire tracer) and the connect timeout cap |
| `PORTCULLIS_DATABASE_STATEMENT_TIMEOUT` | **30s** | [1s, 10m] | session `statement_timeout` startup parameter |
| `PORTCULLIS_DATABASE_LOCK_TIMEOUT` | **10s** | [100ms, 5m], ≤ statement timeout | session `lock_timeout` startup parameter |
| `PORTCULLIS_DATABASE_IDLE_IN_TRANSACTION_TIMEOUT` | **1m** | [1s, 1h] | session `idle_in_transaction_session_timeout` startup parameter |

Migration transactions override the session bounds with their own `SET LOCAL` values; governed target executions use their own connections and ADR-0021 bounds.

### Circuit breaker — target-DB execution path (M1)
Wraps each connection's execute path so a dead/hung target DB sheds load fast instead of stacking goroutines.
Adopt `sony/gobreaker/v2` with its defaults, per target connection:

Execution reports target availability separately from SQL completion. Confirmed SQL errors (such as syntax errors or constraint violations) prove a responding target and do not count as connection failures. Connection failures and unconfirmed transport outcomes count as unhealthy. User cancellation, context deadlines and local response-allocation limits remain `outcome_unknown` for execution, but report inconclusive target health and are excluded with `IsExcluded`: they neither trip the circuit nor reset a real failure streak. Unattempted and inconclusive callbacks are always completed so half-open reservations are released. Admission still precedes lease acquisition so an open circuit cannot consume an approved request; a failed lease acquisition reports an explicit unattempted outcome through gobreaker's `IsExcluded` hook, releasing its reservation without recording a success or resetting the failure streak. Do not leave the two-step callback uncompleted. Result persistence failure does not turn a healthy target into a target fault. This 2026-10-03 review correction preserves single-use execution, audit and the existing target-scoped breaker thresholds. Amended 2026-10-04 with ADR-0021's interruption outcomes: a cancellation or deadline before COMMIT now records `cancelled` or `failed` instead of `outcome_unknown`, and the server's statement timeout (SQLSTATE 57014) records `failed`; all of them still report inconclusive target health, because a timed-out or cancelled statement says nothing about target availability. References: [gobreaker Settings](https://pkg.go.dev/github.com/sony/gobreaker/v2#Settings), [Circuit Breaker exception handling](https://learn.microsoft.com/en-us/azure/architecture/patterns/circuit-breaker).

- trip after **> 5 consecutive failures**; open state lasts **60s**; half-open admits **1 probe** request.
- A rejected call surfaces as `Unavailable` (retryable by the user, no auto-retry — PRD §8.2), leaves the request `approved` (the lease is only taken when the breaker admits), and is recorded in audit metadata.
  Breaker state is process-local (same stance as rate limiting).
- *Provisional:* thresholds are the library defaults; revisit with load-test data at M1 exit.

### Connect server-streams (when streaming lands — PRD §7.4, §8.3)
- Max stream lifetime **30 min** (client silently re-subscribes; unary re-fetch on reconnect per PRD §5.1); server re-validates the session every **60s** and force-closes on revoke/disable; notification fallback polling every **30s**.

## Consequences
Runtime and test harness configuration share an isolated `config.NewEnvironmentLoader` factory (2026-10-03). It retains Viper experimental struct binding, dot-to-underscore environment replacement and TextUnmarshaller/duration/comma-slice hooks. Each consumer uses its own typed struct, prefix, defaults and validation, rather than importing application defaults into a test harness or reading individual settings with `os.Getenv`. This is a configuration-boundary correction; product requirements and runtime defaults are unchanged. [Viper options and environment decoding](https://pkg.go.dev/github.com/spf13/viper).

- Every operational default above is normative: a change requires editing this ADR in the same PR.
  `.env.example` mirrors the (config) values.
- Compile-time constants stay constants until an operator need is demonstrated — promoting one to config is a small change but must update this table.
- **Amended 2026-07-20 (ADR-0017):** for the Tier-C operational tunables listed there (`log_level`, `login_backoff_*`, `connection_test_timeout`, and the M1 query-execution tunables), **(config)** now means *env seed + live DB override* — effective value is the settings-store row when present and valid, else the env value, else the default here.
  Bounds in this ADR remain normative and are enforced on both paths.
  Everything else in this ADR keeps its original meaning (env-only or compile-time).
- The breaker and streaming values ship with M1+ features; they are pinned now so the implementation doesn't improvise them later.

## Sources (checked 2026-07-04)
- pgxpool `Config` (MaxConns, ConnConfig.RuntimeParams, acquire tracer called with the acquisition context; checked 2026-10-04 against pgx v5.10.0): https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool
- PostgreSQL client settings (`statement_timeout`, `lock_timeout`, `idle_in_transaction_session_timeout`): https://www.postgresql.org/docs/current/runtime-config-client.html
- sony/gobreaker defaults (trip > 5 consecutive failures, 60s open, 1 half-open request): https://github.com/sony/gobreaker
- net/http Server timeouts (ReadHeaderTimeout/ReadTimeout/IdleTimeout semantics): https://pkg.go.dev/net/http#Server
- connect-go read limits (`WithReadMaxBytes`, unlimited default): https://connectrpc.com/docs/go/common-errors/
