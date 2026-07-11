package connectapi

import "time"

// Login/Bootstrap throttling: token buckets keyed by client IP and by email cap
// brute-force and enumeration attempts on the unauthenticated procedures (OWASP).
// A burst absorbs legitimate retries; the refill rate bounds sustained attempts.
// The buckets are PROCESS-LOCAL by design (single-instance MVP, ADR-0009);
// shared per-account failure counters ship with the progressive-backoff work.
const (
	loginBurst     = 10               // tokens available before throttling kicks in
	loginRefill    = 3 * time.Second  // one token replenished per this interval
	rateLimiterTTL = 10 * time.Minute // idle buckets are evicted after this long
	// maxLimiterBuckets hard-caps each limiter's map so a flood of distinct IPs or
	// emails within the TTL window can't exhaust memory (least-recently-seen evicted).
	maxLimiterBuckets = 50_000
	// Authenticated procedures (Me/Logout) carry no email, so only a per-IP bucket
	// guards them — more generous than login because a single SPA interaction fires
	// several RPCs, but still enough to shed a garbage-session flood before the auth
	// interceptor's per-request DB session lookup (ADR-0010).
	authBurst  = 60          // interactive burst headroom
	authRefill = time.Second // one token per second sustained
	// maxRateLimitKeyBytes bounds any bucket key regardless of dimension: keys over
	// this are SHA-256-collapsed before use, so no attacker-influenced key (a spoofed
	// or malformed X-Forwarded-For, a future per-token key) can hold large strings.
	// Comfortably fits a normalized "email:" + 254-char address and any IP key.
	maxRateLimitKeyBytes = 320
)

// Cookie and header names for the session + CSRF double-submit scheme (ADR-0006).
const (
	sessionCookie = "__Host-portcullis_session"
	csrfCookie    = "__Host-portcullis_csrf"
	csrfHeader    = "X-CSRF-Token"
)

// Google OIDC redirect flow (ADR-0007). The route patterns are exported for the
// composition root's mounts; the handler itself lives in oidc.go.
const (
	// OIDCStartPattern begins the flow: mint pending state, redirect to Google.
	OIDCStartPattern = "GET /auth/google/start"
	// OIDCCallbackPattern is Google's redirect target.
	OIDCCallbackPattern = "GET /auth/google/callback"
	// oidcPendingCookie carries the sealed state/nonce/verifier across the
	// provider redirect.
	oidcPendingCookie = "__Host-portcullis_oidc"
	// oidcSuccessRedirect is fixed (never a client-supplied return URL — no open
	// redirect); the SPA routes from its own state after calling Me.
	oidcSuccessRedirect = "/"
	// oidcFailureRedirect carries no failure detail; specifics go to the server
	// log only (types, never values).
	oidcFailureRedirect = "/login?error=oidc"
)

// Context keys for the values the auth interceptor injects.
const (
	ctxUser ctxKey = iota
	ctxSessionToken
)
