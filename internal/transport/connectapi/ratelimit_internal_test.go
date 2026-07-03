package connectapi

// White-box test: the bucket-cap invariant is about the limiter's unexported map
// size, which a black-box test can't observe. Everything else is tested black-box.

import (
	"strconv"
	"testing"
	"time"
)

func TestRateLimiterBoundsBucketCount(t *testing.T) {
	t.Parallel()
	const cap = 8
	// Long TTL so idle-sweep never fires: the cap must hold purely via eviction.
	l := newRateLimiter(time.Second, 1, time.Hour, cap)

	// Far more distinct keys than the cap; the map must never exceed it.
	for i := range 1000 {
		if !l.allow("k:" + strconv.Itoa(i)) {
			t.Fatalf("first use of a fresh key should be allowed (i=%d)", i)
		}
		if got := len(l.buckets); got > cap {
			t.Fatalf("bucket count %d exceeded cap %d at i=%d", got, cap, i)
		}
	}
	if got := len(l.buckets); got != cap {
		t.Errorf("final bucket count = %d, want %d", got, cap)
	}
}
