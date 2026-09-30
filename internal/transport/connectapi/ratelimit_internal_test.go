package connectapi

// White-box tests inspect bucket count and key size, which cannot be observed through the shared public throttle response.

import (
	"strconv"
	"strings"
	"testing"
	"time"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
)

func TestEmailBucketKeyBoundsKeyBytes(t *testing.T) {
	t.Parallel()
	oversized := strings.Repeat("a", 60_000) + "@example.com"
	if key := emailBucketKey(&portcullisv1.LoginRequest{Email: oversized}); key != "" {
		t.Errorf("oversized email opened a bucket key of %d bytes", len(key))
	}

	if key := emailBucketKey(&portcullisv1.LoginRequest{Email: " User@Example.com "}); key != "email:user@example.com" {
		t.Errorf("emailBucketKey = %q, want %q", key, "email:user@example.com")
	}

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
