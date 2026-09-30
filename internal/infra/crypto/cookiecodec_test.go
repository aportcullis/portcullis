package crypto_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/infra/crypto"
)

const testOrgID = "0b0d8f7e-3c1a-4d2b-9e5f-6a7b8c9d0e1f"

func TestAADCanonicalLayout(t *testing.T) {
	t.Parallel()

	got := crypto.AAD("oidc_pending", testOrgID, "11111111-2222-3333-4444-555555555555")
	want := "portcullis/aad/v1|oidc_pending|" + testOrgID + "|11111111-2222-3333-4444-555555555555"
	if string(got) != want {
		t.Errorf("AAD = %q, want %q", got, want)
	}
}

func TestCookieCodecRoundTrips(t *testing.T) {
	t.Parallel()
	codec := crypto.NewOIDCPendingCodec(testKeyring(t), testOrgID)

	plaintext := []byte(`{"state":"s","nonce":"n","verifier":"v","exp":123}`)
	value, err := codec.Seal(plaintext)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	// The value must be cookie-safe: one dot separator, no characters a Set-Cookie value can't carry.
	if strings.Count(value, ".") != 1 || strings.ContainsAny(value, " ;,\"\\") {
		t.Errorf("sealed value is not cookie-safe: %q", value)
	}
	got, err := codec.Open(value)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("round trip = %q, want %q", got, plaintext)
	}

	// Each Seal mints a fresh flow id + DEK — two seals of the same plaintext must not produce the same value.
	again, err := codec.Seal(plaintext)
	if err != nil {
		t.Fatalf("second Seal: %v", err)
	}
	if again == value {
		t.Error("two seals produced identical values")
	}
}

func TestCookieCodecFailsClosed(t *testing.T) {
	t.Parallel()
	kr := testKeyring(t)
	codec := crypto.NewOIDCPendingCodec(kr, testOrgID)
	value, err := codec.Seal([]byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	flowID, body, _ := strings.Cut(value, ".")

	other, err := codec.Seal([]byte("other payload"))
	if err != nil {
		t.Fatal(err)
	}
	otherFlowID, otherBody, _ := strings.Cut(other, ".")

	cases := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"no separator", flowID + body},
		{"non-uuid flow id", "not-a-uuid." + body},
		{"garbage body", flowID + ".!!!not-base64!!!"},
		{"body not json", flowID + ".aGVsbG8"},
		{"tampered ciphertext", flowID + "." + tamperLastByte(t, body)},
		// Swapping the flow id re-points the AAD: authentication must fail even though both halves are individually valid.
		{"flow id swap", otherFlowID + "." + body},
		{"body swap", flowID + "." + otherBody},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := codec.Open(tc.value); !errors.Is(err, crypto.ErrDecrypt) {
				t.Errorf("Open(%s) = %v, want ErrDecrypt (fail closed, no detail)", tc.name, err)
			}
		})
	}

	// A codec bound to a different org must not open the value (the org id is in the AAD), and neither may a different keyring.
	t.Run("wrong org", func(t *testing.T) {
		t.Parallel()
		wrongOrg := crypto.NewOIDCPendingCodec(kr, "ffffffff-ffff-4fff-8fff-ffffffffffff")
		if _, err := wrongOrg.Open(value); !errors.Is(err, crypto.ErrDecrypt) {
			t.Errorf("Open with wrong org = %v, want ErrDecrypt", err)
		}
	})
	t.Run("wrong keyring", func(t *testing.T) {
		t.Parallel()
		wrongKey := crypto.NewOIDCPendingCodec(testKeyring(t), testOrgID)
		if _, err := wrongKey.Open(value); !errors.Is(err, crypto.ErrDecrypt) {
			t.Errorf("Open with wrong keyring = %v, want ErrDecrypt", err)
		}
	})
}

func tamperLastByte(t *testing.T, body string) string {
	t.Helper()
	b := []byte(body)
	idx := len(b) - 2
	if b[idx] == 'A' {
		b[idx] = 'B'
	} else {
		b[idx] = 'A'
	}
	return string(b)
}
