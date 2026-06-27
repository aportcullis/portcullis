package crypto_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/aportcullis/portcullis/internal/infra/crypto"
)

func testKeyring(t *testing.T) *crypto.Keyring {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	kr, err := crypto.LoadKeyring(base64.StdEncoding.EncodeToString(raw), "")
	if err != nil {
		t.Fatalf("LoadKeyring: %v", err)
	}
	return kr
}

func TestLoadKeyringErrors(t *testing.T) {
	t.Parallel()
	if _, err := crypto.LoadKeyring("", ""); err == nil {
		t.Error("want error when no key is configured")
	}
	if _, err := crypto.LoadKeyring("not base64 @@@", ""); err == nil {
		t.Error("want error for non-base64 key")
	}
	short := base64.StdEncoding.EncodeToString(make([]byte, 16))
	if _, err := crypto.LoadKeyring(short, ""); err == nil {
		t.Error("want error for a 16-byte key")
	}
}

func TestLoadKeyringRejectsBothSources(t *testing.T) {
	t.Parallel()
	valid := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if _, err := crypto.LoadKeyring(valid, "/some/key/file"); err == nil {
		t.Error("want error when both master key and key file are set")
	}
}

func TestHashPasswordRejectsInvalidParams(t *testing.T) {
	t.Parallel()
	// t=0 would panic Argon2; HashPassword must reject it up front.
	bad := crypto.Argon2Params{Memory: 65536, Time: 0, Threads: 1, KeyLen: 32, SaltLen: 16}
	if _, err := crypto.HashPassword("pw", bad); err == nil {
		t.Error("HashPassword should reject out-of-range parameters")
	}
}

func TestVerifyPasswordRejectsMalformedParams(t *testing.T) {
	t.Parallel()
	// t=0 would panic Argon2; a crafted hash must be rejected, not executed.
	bad := "$argon2id$v=19$m=65536,t=0,p=1$YWJjZGVmZ2hpamtsbW5vcA$YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY"
	if _, _, err := crypto.VerifyPassword("pw", bad, crypto.DefaultArgon2Params); err == nil {
		t.Error("want error for a hash with out-of-range parameters")
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	t.Parallel()
	kr := testKeyring(t)
	pt := []byte("UPDATE users SET token='secret'")
	ad := []byte("req:123|org:1")

	blob, err := kr.Seal(pt, ad)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if bytes.Contains(blob.Ciphertext, []byte("secret")) {
		t.Error("ciphertext leaks plaintext")
	}

	got, err := kr.Open(blob, ad)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, pt) {
		t.Errorf("round trip mismatch: got %q", got)
	}
}

func TestOpenWrongAssociatedData(t *testing.T) {
	t.Parallel()
	kr := testKeyring(t)
	blob, _ := kr.Seal([]byte("data"), []byte("ad-1"))
	if _, err := kr.Open(blob, []byte("ad-2")); err == nil {
		t.Error("Open with mismatched associated data must fail")
	}
}

func TestOpenTampered(t *testing.T) {
	t.Parallel()
	kr := testKeyring(t)
	blob, _ := kr.Seal([]byte("data"), nil)
	blob.Ciphertext[0] ^= 0xff
	if _, err := kr.Open(blob, nil); err == nil {
		t.Error("Open of tampered ciphertext must fail")
	}
}

func TestDigestVerify(t *testing.T) {
	t.Parallel()
	kr := testKeyring(t)
	data := []byte("canonical-payload")

	d, err := kr.Digest(data)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	ok, err := kr.VerifyDigest(data, d)
	if err != nil || !ok {
		t.Errorf("VerifyDigest = %v, %v; want true, nil", ok, err)
	}
	if bad, _ := kr.VerifyDigest([]byte("other"), d); bad {
		t.Error("VerifyDigest must fail for different data")
	}
}

func TestPasswordHashVerify(t *testing.T) {
	t.Parallel()
	enc, err := crypto.HashPassword("hunter2", crypto.DefaultArgon2Params)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	ok, needs, err := crypto.VerifyPassword("hunter2", enc, crypto.DefaultArgon2Params)
	if err != nil || !ok {
		t.Fatalf("VerifyPassword = %v, %v", ok, err)
	}
	if needs {
		t.Error("should not need rehash with identical params")
	}
	if bad, _, _ := crypto.VerifyPassword("wrong", enc, crypto.DefaultArgon2Params); bad {
		t.Error("wrong password must not verify")
	}
}

func TestPasswordNeedsRehash(t *testing.T) {
	t.Parallel()
	weak := crypto.Argon2Params{Memory: 8 * 1024, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 16}
	enc, _ := crypto.HashPassword("pw", weak)

	ok, needs, err := crypto.VerifyPassword("pw", enc, crypto.DefaultArgon2Params)
	if err != nil || !ok {
		t.Fatalf("VerifyPassword = %v, %v", ok, err)
	}
	if !needs {
		t.Error("should need rehash when stored params are weaker than the default profile")
	}
}
