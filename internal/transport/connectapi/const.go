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
)

// Cookie and header names for the session + CSRF double-submit scheme (ADR-0006).
const (
	sessionCookie = "__Host-portcullis_session"
	csrfCookie    = "__Host-portcullis_csrf"
	csrfHeader    = "X-CSRF-Token"
)

// Context keys for the values the auth interceptor injects.
const (
	ctxUser ctxKey = iota
	ctxSessionToken
)
