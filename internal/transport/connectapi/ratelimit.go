package connectapi

import (
	"context"
	"errors"
	"sync"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/time/rate"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/platform/reqmeta"
)

// rateLimiter is a keyed token-bucket store: one *rate.Limiter per key, created
// lazily and evicted once idle past its TTL so the map can't grow unbounded under
// a flood of distinct keys (e.g. spoofed emails). Safe for concurrent use.
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

// newRateLimiter builds a store whose buckets refill one token per interval, up
// to burst, are evicted after ttl of inactivity, and are hard-capped at
// maxBuckets entries so a flood of distinct keys can't exhaust memory.
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

// allow reports whether the key has a token to spend, consuming one if so.
func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		// Bound the map before inserting a new key: sweep idle entries first, then
		// (if a flood of live keys still fills it) evict the least-recently-seen one.
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

// evictOldestLocked removes the single least-recently-seen bucket, so the map
// stays capped even under a flood of continuously-active distinct keys. O(n),
// but only runs when the cap is hit. Caller holds the lock.
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

// NewRateLimitInterceptor throttles the unauthenticated Login/Bootstrap
// procedures per client IP and per email, so neither a single host nor a single
// targeted account can be hammered. Over-limit requests are rejected with
// ResourceExhausted before any password hashing runs. The client IP is read from
// the context (resolved once by NewClientIPInterceptor, which must run first).
func NewRateLimitInterceptor() connect.UnaryInterceptorFunc {
	byIP := newRateLimiter(loginRefill, loginBurst, rateLimiterTTL, maxLimiterBuckets)
	byEmail := newRateLimiter(loginRefill, loginBurst, rateLimiterTTL, maxLimiterBuckets)
	exhausted := connect.NewError(connect.CodeResourceExhausted, errors.New("too many attempts, retry later"))
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if !publicProcedures[req.Spec().Procedure] {
				return next(ctx, req)
			}
			if ip := reqmeta.ClientIP(ctx); ip != "" && !byIP.allow("ip:"+ip) {
				return nil, exhausted
			}
			if email := loginEmail(req.Any()); email != "" && !byEmail.allow("email:"+identity.NormalizeEmail(email)) {
				return nil, exhausted
			}
			return next(ctx, req)
		}
	}
}

// loginEmail extracts the email from a Login/Bootstrap message, or "" for any
// other message type (so the key space stays bounded to real login attempts).
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
