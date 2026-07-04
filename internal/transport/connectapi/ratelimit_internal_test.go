package connectapi

// White-box tests: the limiter's memory-bound invariants — the bucket-count cap
// and the key-byte bound — live in the unexported map and key derivation, which
// a black-box test can't observe (both limiter dimensions share the throttle
// parameters, so an e2e request can't tell WHICH bucket rejected it).
// Everything else is tested black-box.

import (
	"strconv"
	"strings"
	"testing"
	"time"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
)

// An oversized "email" (up to the 64 KiB body cap) must not open a bucket: the
// 50k bucket cap bounds the COUNT, not the key bytes, so attacker-sized keys
// would otherwise hold gigabytes. It can never match a stored account anyway.
func TestEmailBucketKeyBoundsKeyBytes(t *testing.T) {
	t.Parallel()
	oversized := strings.Repeat("a", 60_000) + "@example.com"
	if key := emailBucketKey(&portcullisv1.LoginRequest{Email: oversized}); key != "" {
		t.Errorf("oversized email opened a bucket key of %d bytes", len(key))
	}
	// Normal addresses key their bucket in normalized form (one identity, one bucket).
	if key := emailBucketKey(&portcullisv1.LoginRequest{Email: " User@Example.com "}); key != "email:user@example.com" {
		t.Errorf("emailBucketKey = %q, want %q", key, "email:user@example.com")
	}
	// Non-login messages never open email buckets.
	if key := emailBucketKey(&portcullisv1.MeRequest{}); key != "" {
		t.Errorf("non-login message opened bucket key %q", key)
	}
}

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
