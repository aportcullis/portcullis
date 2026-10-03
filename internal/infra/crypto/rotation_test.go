package crypto_test

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/aportcullis/portcullis/internal/infra/crypto"
)

func TestRotationRetainsHistoricalPayloadsAndDigests(t *testing.T) {
	oldKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	newKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	old, _ := crypto.LoadKeyring(oldKey, "")
	aad := crypto.AAD("request_payload", "org", "request")
	blob, _ := old.Seal([]byte("secret"), aad)
	digest, _ := old.Digest([]byte("canonical"))
	ring, err := crypto.LoadVersionedKeyring(newKey, "", "1:"+oldKey, "")
	if err != nil {
		t.Fatal(err)
	}
	if ring.Active() != 2 {
		t.Fatalf("active=%d, want 2", ring.Active())
	}
	plain, err := ring.Open(blob, aad)
	if err != nil || string(plain) != "secret" {
		t.Fatalf("historical payload unavailable: %v", err)
	}
	if valid, err := ring.VerifyDigest([]byte("canonical"), digest); err != nil || !valid {
		t.Fatal("historic approval digest lost")
	}
	for _, previous := range []string{"1:" + oldKey + ",1:" + oldKey, "2:" + oldKey, "1:bad", "1:" + newKey} {
		if _, err := crypto.LoadVersionedKeyring(newKey, "", previous, ""); err == nil {
			t.Fatal("invalid historical keys accepted")
		}
	}
}
