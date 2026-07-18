package crypto_test

import (
	"context"
	"encoding/base64"
	"errors"
	"sync"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
)

var weakParams = crypto.Argon2Params{Memory: 8 * 1024, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 16}

func TestArgon2HasherMinConcurrency(t *testing.T) {
	t.Parallel()
	// 0 / negative must coerce to 1, not a zero-capacity semaphore that deadlocks.
	if _, err := crypto.NewArgon2Hasher(weakParams, 0).Hash(context.Background(), "pw"); err != nil {
		t.Errorf("maxConcurrent=0 should behave as 1: %v", err)
	}
}

func TestArgon2HasherConcurrentSafe(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := crypto.NewArgon2Hasher(weakParams, 2) // cap below the goroutine count
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			enc, err := h.Hash(ctx, "pw")
			if err != nil {
				t.Errorf("Hash: %v", err)
				return
			}
			if ok, _, err := h.Verify(ctx, "pw", enc); err != nil || !ok {
				t.Errorf("Verify = %v, %v", ok, err)
			}
		}()
	}
	wg.Wait()
}

func TestArgon2HasherCancelledContextIsRejected(t *testing.T) {
	t.Parallel()
	// An already-cancelled context must be rejected before any hashing, even with a
	// free slot — deterministically, in a single call (no retry loop).
	h := crypto.NewArgon2Hasher(weakParams, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := h.Hash(ctx, "pw"); !errors.Is(err, context.Canceled) {
		t.Errorf("Hash with a cancelled context = %v, want context.Canceled", err)
	}
	if _, _, err := h.Verify(ctx, "pw", "x"); !errors.Is(err, context.Canceled) {
		t.Errorf("Verify with a cancelled context = %v, want context.Canceled", err)
	}
}

func TestArgon2HasherRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := crypto.NewArgon2Hasher(weakParams, 4)

	enc, err := h.Hash(ctx, "hunter2")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if ok, _, err := h.Verify(ctx, "hunter2", enc); err != nil || !ok {
		t.Fatalf("Verify(correct) = %v, %v", ok, err)
	}
	if ok, _, err := h.Verify(ctx, "wrong", enc); err != nil || ok {
		t.Errorf("Verify(wrong) = %v, %v", ok, err)
	}
}

func TestArgon2HasherNeedsRehashUnderStrongerProfile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	enc, err := crypto.NewArgon2Hasher(weakParams, 4).Hash(ctx, "pw")
	if err != nil {
		t.Fatal(err)
	}
	stronger := crypto.NewArgon2Hasher(crypto.Argon2Params{Memory: 16 * 1024, Time: 2, Threads: 1, KeyLen: 32, SaltLen: 16}, 4)
	ok, needsRehash, err := stronger.Verify(ctx, "pw", enc)
	if err != nil || !ok {
		t.Fatalf("Verify = %v, %v", ok, err)
	}
	if !needsRehash {
		t.Error("a weaker stored hash should need rehashing under a stronger profile")
	}
}

func loadKeyring(t *testing.T, seed byte) *crypto.Keyring {
	t.Helper()
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = seed
	}
	kr, err := crypto.LoadKeyring(base64.StdEncoding.EncodeToString(raw), "")
	if err != nil {
		t.Fatalf("LoadKeyring: %v", err)
	}
	return kr
}

func TestCSRFProtectorIssueVerify(t *testing.T) {
	t.Parallel()
	c := crypto.NewCSRFProtector(loadKeyring(t, 0x01))

	t1, err := c.Issue("sess-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	t2, err := c.Issue("sess-1")
	if err != nil {
		t.Fatal(err)
	}
	// A fresh nonce each call → different tokens that both still verify.
	if t1 == t2 {
		t.Error("each Issue should produce a fresh token")
	}
	if !c.Verify("sess-1", t1) || !c.Verify("sess-1", t2) {
		t.Error("issued tokens must verify for their session")
	}
	// Wrong session, malformed, tampered, and a different key must all fail.
	if c.Verify("sess-2", t1) {
		t.Error("token must not verify for another session")
	}
	if c.Verify("sess-1", "garbage") || c.Verify("sess-1", t1+"x") {
		t.Error("malformed/tampered tokens must fail")
	}
	if other := crypto.NewCSRFProtector(loadKeyring(t, 0x02)); other.Verify("sess-1", t1) {
		t.Error("token must not verify under a different key")
	}
}

func TestConnectionCredentialCodecRoundTrip(t *testing.T) {
	t.Parallel()
	codec := crypto.NewConnectionCredentialCodec(testKeyring(t))
	cred := connection.Credential{User: "app_reader", Password: "s3cret-π"}

	sealed, err := codec.Seal("org-1", "conn-1", cred)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if sealed.KeyVersion == 0 || len(sealed.WrappedDEK) == 0 || len(sealed.Nonce) == 0 || len(sealed.Ciphertext) == 0 {
		t.Fatalf("sealed envelope incomplete: %+v", sealed)
	}
	got, err := codec.Open("org-1", "conn-1", sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got != cred {
		t.Errorf("round trip = %+v, want %+v", got, cred)
	}
}

// A sealed credential must not open under another connection's or another
// organization's identity — the AAD binds both (ADR-0003/0014), failing closed.
func TestConnectionCredentialCodecAADBinding(t *testing.T) {
	t.Parallel()
	codec := crypto.NewConnectionCredentialCodec(testKeyring(t))
	sealed, err := codec.Seal("org-1", "conn-1", connection.Credential{User: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Open("org-1", "conn-2", sealed); !errors.Is(err, crypto.ErrDecrypt) {
		t.Errorf("Open with wrong connection id err = %v, want ErrDecrypt", err)
	}
	if _, err := codec.Open("org-2", "conn-1", sealed); !errors.Is(err, crypto.ErrDecrypt) {
		t.Errorf("Open with wrong org err = %v, want ErrDecrypt", err)
	}
}
