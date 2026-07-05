# ADR-0010: Runtime & transport defaults

- **Status:** Accepted
- **Date:** 2026-07-04

## Context
A reimplementation of Portcullis from the PRD and ADRs alone would converge on the security
*model* but diverge on the operational *numbers*: HTTP timeouts, request caps, rate-limit
budgets, boot ordering, exit codes. Those values were chosen deliberately during the M0
hardening reviews but lived only in code. This ADR records every such default as the binding
contract, so the documentation fully determines the implementation. Values marked
**(config)** are operator-tunable env vars; everything else is a compile-time constant by
design (no config surface until a real need appears — see `docs/conventions/code.md`,
minimize-hardcoding: these are fixed operational guards, not domain catalogs).

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

- Routes: `GET /livez`, `GET /readyz`, Connect handlers at their generated paths, SPA fallback
  at `/` (unknown paths serve `index.html`; before the frontend is built, a placeholder page).
- Graceful shutdown: readiness flips to *draining* first, waits `drain_delay`
  (**default 0s**, capped at **5 min**, (config) `PORTCULLIS_DRAIN_DELAY`) so Kubernetes
  deregisters the pod, then `http.Server.Shutdown` bounded by `shutdown_timeout`
  (**default 15s**, (config) `PORTCULLIS_SHUTDOWN_TIMEOUT`, must be > 0). The delay and the
  timeout are **sequential** budgets. `drain_delay` is capped (amended 2026-07-05) because it
  blocks shutdown before draining even begins — an unbounded typo ("2h") would hang past any
  orchestrator's grace period.
- **Second signal force-quits** (amended 2026-07-05): after the first SIGINT/SIGTERM begins the
  drain, signal handling is restored (`signal.NotifyContext`'s `stop` is called immediately, per
  the os/signal guidance) so a **second** signal terminates the process with the default
  behavior instead of being swallowed for the whole drain window.

### TLS termination & security headers (added 2026-07-05)
- **TLS is a deployment prerequisite, terminated upstream.** The server speaks plain HTTP
  (`ListenAndServe`, no in-process TLS) and is designed to sit behind a TLS-terminating reverse
  proxy / ingress. This is load-bearing, not incidental: session/CSRF cookies are `__Host-` +
  `Secure` (ADR-0006), which browsers accept **only over HTTPS** — served over cleartext to the
  browser, login silently fails because the cookie is never stored. Operators must terminate TLS
  in front of Portcullis.
- **HSTS is the proxy's responsibility** — it owns the TLS edge — so the app does not emit
  `Strict-Transport-Security`.
- Every response carries hardening headers as defense-in-depth (cheap, valid even behind the
  proxy): `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`,
  `Referrer-Policy: strict-origin-when-cross-origin`, and a **minimal** CSP
  (`frame-ancestors 'none'; base-uri 'self'; form-action 'self'`). The CSP omits
  `script-src`/`style-src` on purpose: the embedded SPA and the not-built placeholder use inline
  `style` attributes that a strict `style-src` would break. Tightening to a script/style CSP is a
  follow-up once the built bundle is verified against it.

### Request correlation (`internal/platform/logging`)
- Header `X-Request-Id` is accepted only when **non-empty, ≤ 128 bytes**, and matches the
  charset `[A-Za-z0-9._:/-]` exactly; anything else is **replaced** with a fresh
  8-byte-hex id — never truncated, so two attacker-controlled values can't collide by prefix.
- The id is echoed in the response header, logged on the one-line-per-request log, and copied
  into audit rows. One structured line per request: request_id, method, path, status, bytes,
  duration, socket remote (forwarded headers deliberately ignored in logging — proxy trust is
  decided at the interceptor boundary only). Health-probe requests log at debug.

### Client IP resolution & interceptor chain (`internal/transport/connectapi`)
- Interceptor order is fixed: **ClientIP → RateLimit → Auth**. The IP is resolved once and
  shared via context (rate limiting and audit use the same value); rate limiting runs before
  auth so a login flood is shed before any Argon2 hashing.
- `clientIP`: with no trusted proxies (default), the direct peer is the client. When the peer
  is inside `PORTCULLIS_TRUSTED_PROXIES` (config, comma-separated CIDRs), walk
  `X-Forwarded-For` **right-to-left** (nearest hop first) and return the **first address that
  is not itself a trusted proxy**. Right-to-left is load-bearing: a client can prepend spoofed
  entries on the left, but everything from the real connection rightward was written by
  trusted proxies.
- **Malformed hops fall back to the peer** (amended 2026-07-05). A walked entry that does not
  `net.ParseIP` cannot be a real client address — a trusted proxy could forward a
  malformed/oversized value, or an attacker prepends one past the known hops. Returning it
  verbatim would let a spoofed `X-Forwarded-For` mint an arbitrary, unbounded rate-limit key
  (and a bogus audit `source_ip`); instead, stop trusting the chain at the malformed hop and
  attribute the request to the peer. (Supersedes the earlier "unparsable entries are returned
  as the client key".)
- Config rejects a `/0` trusted-proxy CIDR at startup: "trust everything" makes XFF fully
  spoofable and defeats per-IP limiting — always a misconfig.

### Rate limiting (`internal/transport/connectapi`)
For the **public** procedures (Bootstrap, Login) two **independent** token-bucket stores —
keyed by client IP and by normalized email — must both admit the request, **before any
hashing**; over limit → Connect `ResourceExhausted`. The IP key is canonicalized via
`net.ParseIP(...).String()` (IPv6 spellings of one address share a bucket).

**Authenticated procedures are throttled too** (amended 2026-07-05): Me/Logout carry no email,
so a **third, more generous per-IP store** guards them, so a flood of requests carrying garbage
session cookies cannot drive unbounded DB session lookups through the Auth interceptor. It is
more generous than login because one SPA interaction fires several RPCs.

**Key-byte bound** (amended 2026-07-05): every bucket key, regardless of dimension, is capped —
a key over `maxRateLimitKeyBytes` (320) is SHA-256-collapsed before use, so no
attacker-influenced key (a spoofed/malformed `X-Forwarded-For`, a future per-token key) can hold
large strings. The bucket-count cap bounds the map size; this bounds each entry's key size.

| Parameter | Login/Bootstrap (per IP, per email) | Authenticated (per IP) |
|---|---|---|
| burst | **10** tokens | **60** tokens |
| refill | **1 token / 3s** | **1 token / 1s** |
| idle-bucket TTL | **10 min** | **10 min** |
| max buckets per store | **50,000** — sweep idle first, then evict least-recently-seen | **50,000** |
| max key bytes | **320** (over → SHA-256) | **320** |

Process-local by design (single-instance MVP, ADR-0009); shared per-account counters arrive
with the progressive-backoff work (parameters pinned in ADR-0006, Parameters). The
authenticated-procedure numbers are *provisional* — re-checked against real SPA traffic after MVP.

### Startup sequence (`cmd/portcullis`)
Ordered, each step fail-fast; any failure exits **1** (the only non-zero exit code — cleanup
runs via deferred calls inside `run()`):
1. Load config (validated: enumerated log level/format **reject unknown values** — no silent
   fallback; bounded numerics reject out-of-range; addr must be non-empty, bare port `8080`
   normalized to `:8080`, default **`:8080`**).
2. Build logger.
3. Load keyring — **refuse to start** without a valid master key (ADR-0003).
4. Require `PORTCULLIS_DATABASE_URL`.
5. Open a **30s startup context** covering everything up to serving.
6. Migrate on the owner DSN (`PORTCULLIS_MIGRATE_DATABASE_URL`, empty ⇒ runtime DSN) on a
   short-lived pool: ping first (so connect errors get a generic, DSN-safe message), apply,
   close — the owner credential lives only for this window. Migration/DB errors are logged
   with classified fields only; raw errors can echo the DSN (password).
7. Open the runtime pool, ping, then `VerifyRuntimeConnection` (ADR-0009);
   `PORTCULLIS_ALLOW_PRIVILEGED_RUNTIME=true` downgrades *only* over-privilege violations.
8. Wire adapters (composition root), mount handlers, register the metadata-DB readiness check,
   serve. A bind/serve failure is delivered on a channel and selected against the signal
   context — it terminates with exit 1, never the graceful path.

### Metadata migrations & advisory locks (`internal/infra/postgres`)
- Migrations: embedded `migrations/*.sql`, applied in **lexical filename order**, each file in
  its own transaction, recorded in `public.schema_migrations(version text primary key,
  applied_at timestamptz not null default now())` (`version` = filename minus `.sql`). The
  whole run is serialized by a **session-level advisory lock held on one dedicated
  connection**; that session first `DISCARD TEMP`, then pins `search_path = public`
  (`pg_catalog` remains implicitly first) rather than inheriting a role/DSN setting; role
  preflight/postflight per ADR-0009.
- Advisory-lock keyspace (first arg of the two-arg `pg_advisory_*` family; second arg = object
  id, 0 when global): class **1** = bootstrap (global), **2** = per-user session rotation,
  **3** = migration (global). New classes are appended here, never reused.

### Circuit breaker — target-DB execution path (M1)
Wraps each connection's execute path so a dead/hung target DB sheds load fast instead of
stacking goroutines. Adopt `sony/gobreaker/v2` with its defaults, per target connection:
- trip after **> 5 consecutive failures**; open state lasts **60s**; half-open admits
  **1 probe** request.
- A rejected call surfaces as `Unavailable` (retryable by the user, no auto-retry — PRD §8.2),
  leaves the request `approved` (the lease is only taken when the breaker admits), and is
  recorded in audit metadata. Breaker state is process-local (same stance as rate limiting).
- *Provisional:* thresholds are the library defaults; revisit with load-test data at M1 exit.

### Connect server-streams (when streaming lands — PRD §7.4, §8.3)
- Max stream lifetime **30 min** (client silently re-subscribes; unary re-fetch on reconnect
  per PRD §5.1); server re-validates the session every **60s** and force-closes on
  revoke/disable; notification fallback polling every **30s**.

## Consequences
- Every operational default above is normative: a change requires editing this ADR in the
  same PR. `.env.example` mirrors the (config) values.
- Compile-time constants stay constants until an operator need is demonstrated — promoting
  one to config is a small change but must update this table.
- The breaker and streaming values ship with M1+ features; they are pinned now so the
  implementation doesn't improvise them later.

## Sources (checked 2026-07-04)
- sony/gobreaker defaults (trip > 5 consecutive failures, 60s open, 1 half-open request):
  https://github.com/sony/gobreaker
- net/http Server timeouts (ReadHeaderTimeout/ReadTimeout/IdleTimeout semantics):
  https://pkg.go.dev/net/http#Server
- connect-go read limits (`WithReadMaxBytes`, unlimited default):
  https://connectrpc.com/docs/go/common-errors/
