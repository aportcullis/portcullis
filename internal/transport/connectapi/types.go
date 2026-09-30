package connectapi

import (
	"time"

	"golang.org/x/time/rate"
)

// ctxKey types the context keys the auth interceptor injects, so they can't collide with keys from other packages.
type ctxKey int

// bucket pairs a token-bucket limiter with when it was last used, so idle entries can be swept (see rateLimiter).
type bucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}
