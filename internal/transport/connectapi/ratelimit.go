package connectapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/time/rate"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/platform/reqmeta"
)

// rateLimiter is a keyed token-bucket store: one *rate.Limiter per key, created lazily and evicted once idle past its TTL so the map can't grow unbounded under a flood of distinct keys (e.g. spoofed emails). Safe for concurrent use.
type rateLimiter struct {
	mu         sync.Mutex
	buckets    map[string]*bucket
	limit      rate.Limit
	burst      int
	ttl        time.Duration
	maxBuckets int
	now        func() time.Time
	lastGC     time.Time
}

// newRateLimiter builds a store whose buckets refill one token per interval, up to burst, are evicted after ttl of inactivity, and are hard-capped at maxBuckets entries so a flood of distinct keys can't exhaust memory.
func newRateLimiter(interval time.Duration, burst int, ttl time.Duration, maxBuckets int) *rateLimiter {
	return &rateLimiter{
		buckets:    map[string]*bucket{},
		limit:      rate.Every(interval),
		burst:      burst,
		ttl:        ttl,
		maxBuckets: maxBuckets,
		now:        time.Now,
	}
}

// boundKey hashes oversized attacker-controlled keys to bound map memory; ordinary IP and email keys pass through without allocation.
func boundKey(key string) string {
	if len(key) <= maxRateLimitKeyBytes {
		return key
	}
	sum := sha256.Sum256([]byte(key))
	return "h:" + hex.EncodeToString(sum[:])
}

// allow reports whether the key has a token to spend, consuming one if so.
func (l *rateLimiter) allow(key string) bool {
	key = boundKey(key)
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		// Bound the map before inserting a new key: sweep idle entries first, then (if a flood of live keys still fills it) evict the least-recently-seen one.
		if len(l.buckets) >= l.maxBuckets {
			l.sweepLocked(now)
			if len(l.buckets) >= l.maxBuckets {
				l.evictOldestLocked()
			}
		}
		b = &bucket{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.buckets[key] = b
	}
	b.lastSeen = now
	l.gcLocked(now)
	return b.limiter.Allow()
}

// gcLocked sweeps idle buckets at most once per ttl. Caller holds the lock.
func (l *rateLimiter) gcLocked(now time.Time) {
	if now.Sub(l.lastGC) < l.ttl {
		return
	}
	l.lastGC = now
	l.sweepLocked(now)
}

// sweepLocked deletes every bucket idle longer than ttl. Caller holds the lock.
func (l *rateLimiter) sweepLocked(now time.Time) {
	for k, b := range l.buckets {
		if now.Sub(b.lastSeen) > l.ttl {
			delete(l.buckets, k)
		}
	}
}

// evictOldestLocked removes the single least-recently-seen bucket, so the map stays capped even under a flood of continuously-active distinct keys. O(n), but only runs when the cap is hit. Caller holds the lock.
func (l *rateLimiter) evictOldestLocked() {
	var oldestKey string
	var oldest time.Time
	for k, b := range l.buckets {
		if oldestKey == "" || b.lastSeen.Before(oldest) {
			oldestKey, oldest = k, b.lastSeen
		}
	}
	if oldestKey != "" {
		delete(l.buckets, oldestKey)
	}
}

// NewRateLimitInterceptor limits all procedures by client IP and credential procedures additionally by email, before hashing or session lookup (ADR-0010). NewClientIPInterceptor must run first.
func NewRateLimitInterceptor() connect.UnaryInterceptorFunc {
	byIP := newRateLimiter(loginRefill, loginBurst, rateLimiterTTL, maxLimiterBuckets)
	byEmail := newRateLimiter(loginRefill, loginBurst, rateLimiterTTL, maxLimiterBuckets)
	byIPAuth := newRateLimiter(authRefill, authBurst, rateLimiterTTL, maxLimiterBuckets)
	exhausted := connect.NewError(connect.CodeResourceExhausted, errors.New("too many attempts, retry later"))
	// A shed interactive request is not a failed attempt at anything, so it says so: the login message would tell a user who merely clicked quickly that their credentials are being rate limited.
	tooFast := connect.NewError(connect.CodeResourceExhausted, errors.New("too many requests, retry in a moment"))
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			// The context IP is already canonical (NewClientIPInterceptor normalizes it once for every consumer), so it keys the bucket as-is.
			ip := reqmeta.ClientIP(ctx)
			if !credentialProcedures[req.Spec().Procedure] {
				// Everything that costs no password hash — authenticated procedures (Me/Logout) AND the cheap public GetConfig the SPA calls on every page load: per-IP only, generous bucket.
				if ip != "" && !byIPAuth.allow("ip:"+ip) {
					return nil, tooFast
				}
				return next(ctx, req)
			}
			if ip != "" && !byIP.allow("ip:"+ip) {
				return nil, exhausted
			}
			if key := emailBucketKey(req.Any()); key != "" && !byEmail.allow(key) {
				return nil, exhausted
			}
			return next(ctx, req)
		}
	}
}

// emailBucketKey ignores missing or oversized emails so invalid input cannot evict legitimate per-email counters; the per-IP limit still applies.
func emailBucketKey(msg any) string {
	email := identity.NormalizeEmail(loginEmail(msg))
	if email == "" || identity.EmailTooLong(email) {
		return ""
	}
	return "email:" + email
}

// loginEmail extracts the email from a Login/Bootstrap message, or "" for any other message type (so the key space stays bounded to real login attempts).
func loginEmail(msg any) string {
	switch m := msg.(type) {
	case *portcullisv1.LoginRequest:
		return m.GetEmail()
	case *portcullisv1.BootstrapRequest:
		return m.GetEmail()
	default:
		return ""
	}
}
