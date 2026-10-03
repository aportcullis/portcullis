package crypto_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/query"
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
	h := crypto.NewArgon2Hasher(weakParams, 2)
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
	// An already-cancelled context must be rejected before any hashing, even with a free slot — deterministically, in a single call (no retry loop).
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

func TestArgon2HasherCancelledWaiterErrorsAndLeaksNoSlot(t *testing.T) {
	t.Parallel()
	// Cancellation must release a parked waiter without leaking a slot. The pre-select acquisition race cannot be scheduled through the public API.
	for range 100 {
		h := crypto.NewArgon2Hasher(weakParams, 1)
		h.TestFillSlot()
		ctx, cancel := context.WithCancel(context.Background())
		got := make(chan error, 1)
		go func() { got <- h.TestAcquire(ctx) }()
		time.Sleep(time.Millisecond)
		cancel()
		h.TestReleaseSlot()
		if err := <-got; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled waiter acquired the slot: err=%v", err)
		}
		if n := h.TestSlotsInUse(); n != 0 {
			t.Fatalf("slot leaked after a cancelled acquire: in use = %d", n)
		}
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
	for idx := range raw {
		raw[idx] = seed
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

func TestAccessRequestPayloadCodecRoundTrip(t *testing.T) {
	t.Parallel()
	codec := crypto.NewAccessRequestPayloadCodec(testKeyring(t))
	payload := access.Payload{
		Title: "Monthly maintenance",
		Body:  "Reason\n<script>literal</script>",
		SQL:   "update t set note = 'π secret' where id = :id",
		Params: []query.Parameter{
			{Name: "id", Value: query.TypedValue{Type: query.ParamInteger, Text: "42"}},
			{Name: "when", Value: query.TypedValue{Type: query.ParamNull, Text: ""}},
		},
	}

	sealed, err := codec.Seal("org-1", "req-1", payload)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if sealed.KeyVersion == 0 || len(sealed.WrappedDEK) == 0 || len(sealed.Nonce) == 0 || len(sealed.Ciphertext) == 0 {
		t.Fatalf("sealed envelope incomplete: %+v", sealed)
	}
	got, err := codec.Open("org-1", "req-1", sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got.Title != payload.Title || got.Body != payload.Body || got.SQL != payload.SQL || len(got.Params) != 2 || got.Params[0] != payload.Params[0] || got.Params[1] != payload.Params[1] {
		t.Errorf("round trip = %+v, want %+v", got, payload)
	}
}

func TestAccessRequestPayloadCodecAADBinding(t *testing.T) {
	t.Parallel()
	codec := crypto.NewAccessRequestPayloadCodec(testKeyring(t))
	sealed, err := codec.Seal("org-1", "req-1", access.Payload{SQL: "select 1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Open("org-1", "req-2", sealed); !errors.Is(err, crypto.ErrDecrypt) {
		t.Errorf("Open with wrong request id err = %v, want ErrDecrypt", err)
	}
	if _, err := codec.Open("org-2", "req-1", sealed); !errors.Is(err, crypto.ErrDecrypt) {
		t.Errorf("Open with wrong org err = %v, want ErrDecrypt", err)
	}
}

func TestAccessRequestPayloadCodecDigest(t *testing.T) {
	t.Parallel()
	codec := crypto.NewAccessRequestPayloadCodec(testKeyring(t))
	d1, kv1, err := codec.Digest([]byte("canonical-a"))
	if err != nil || len(d1) == 0 || kv1 == 0 {
		t.Fatalf("Digest = %x, %d, %v", d1, kv1, err)
	}
	d2, _, err := codec.Digest([]byte("canonical-a"))
	if err != nil || !bytes.Equal(d1, d2) {
		t.Errorf("digest not deterministic: %x vs %x (%v)", d1, d2, err)
	}
	d3, _, err := codec.Digest([]byte("canonical-b"))
	if err != nil || bytes.Equal(d1, d3) {
		t.Errorf("distinct inputs must digest differently (%v)", err)
	}
}
