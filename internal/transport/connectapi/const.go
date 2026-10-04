package connectapi

import "time"

// Process-local IP and per-client account buckets limit credential attempts before hashing; the cross-IP account counter is the database backoff (ADR-0006/0010).
const (
	loginBurst  = 10              // tokens available before throttling kicks in
	loginRefill = 3 * time.Second // one token replenished per this interval
	// The (email, client IP) bucket has a smaller burst than the IP bucket, matching the default backoff threshold, so one client cannot spend its whole IP budget on one account while other clients keep their own budget for it.
	loginAccountBurst  = 5
	loginAccountRefill = loginRefill
	rateLimiterTTL     = 10 * time.Minute // idle buckets are evicted after this long
	// maxLimiterBuckets hard-caps each limiter's map so a flood of distinct IPs or emails within the TTL window can't exhaust memory (least-recently-seen evicted).
	maxLimiterBuckets = 50_000
	// Authenticated procedures use a per-IP bucket before session lookup to shed floods while allowing normal RPC bursts and shared office IPs (ADR-0010).
	authBurst  = 240                    // several fan-outs of headroom
	authRefill = 200 * time.Millisecond // 5/s sustained per IP
	// maxRateLimitKeyBytes bounds any bucket key regardless of dimension: keys over this are SHA-256-collapsed before use, so no attacker-influenced key (a spoofed or malformed X-Forwarded-For, a future per-token key) can hold large strings. Comfortably fits a normalized "email:" + 254-char address plus "|ip:" and an IPv6 key.
	maxRateLimitKeyBytes = 320
)

// Cookie and header names for the session + CSRF double-submit scheme (ADR-0006).
const (
	sessionCookie = "__Host-portcullis_session"
	csrfCookie    = "__Host-portcullis_csrf"
	csrfHeader    = "X-CSRF-Token"
)

// Google OIDC redirect flow (ADR-0007). The route patterns are exported for the composition root's mounts; the handler itself lives in oidc.go.
const (
	// OIDCStartPattern begins the flow: mint pending state, redirect to Google.
	OIDCStartPattern = "GET /auth/google/start"
	// OIDCCallbackPattern is Google's redirect target.
	OIDCCallbackPattern = "GET /auth/google/callback"
	// oidcPendingCookie carries the sealed state/nonce/verifier across the provider redirect.
	oidcPendingCookie = "__Host-portcullis_oidc"
	// oidcSuccessRedirect is fixed (never a client-supplied return URL — no open redirect); the SPA routes from its own state after calling Me.
	oidcSuccessRedirect = "/"
	// oidcFailureRedirect carries no failure detail; specifics go to the server log only (types, never values).
	oidcFailureRedirect = "/login?error=oidc"
)

// Context keys for the values the auth interceptor injects.
const (
	ctxUser ctxKey = iota
	ctxSessionToken
)
