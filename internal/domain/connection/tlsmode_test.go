package connection_test

import (
	"errors"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/connection"
)

func TestParseTLSMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		want    connection.TLSMode
		wantErr error
	}{
		{"verify-full", "verify-full", connection.TLSModeVerifyFull, nil},
		{"verify-ca", "verify-ca", connection.TLSModeVerifyCA, nil},
		{"require", "require", connection.TLSModeRequire, nil},
		{"disable", "disable", connection.TLSModeDisable, nil},
		{"empty means the certificate-verifying default", "", connection.TLSModeVerifyFull, nil},
		// prefer/allow silently downgrade to plaintext — rejected (ADR-0014).
		{"prefer rejected", "prefer", "", connection.ErrInvalidTLSMode},
		{"allow rejected", "allow", "", connection.ErrInvalidTLSMode},
		{"garbage rejected", "verifyfull", "", connection.ErrInvalidTLSMode},
		{"case-sensitive", "Verify-Full", "", connection.ErrInvalidTLSMode},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := connection.ParseTLSMode(tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ParseTLSMode(%q) err = %v, want %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseTLSMode(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDefaultTLSModeIsCertificateVerifying(t *testing.T) {
	t.Parallel()
	if got := connection.DefaultTLSMode(); got != connection.TLSModeVerifyFull {
		t.Errorf("DefaultTLSMode() = %q, want verify-full (PRD §8.1)", got)
	}
}

func TestTLSModeRelaxed(t *testing.T) {
	t.Parallel()
	// Relaxed = no certificate-chain validation at all; choosing one requires an
	// explicit admin choice plus a CONNECTION_TLS_RELAXED audit event (PRD §8.1).
	relaxed := map[connection.TLSMode]bool{
		connection.TLSModeVerifyFull: false,
		connection.TLSModeVerifyCA:   false,
		connection.TLSModeRequire:    true,
		connection.TLSModeDisable:    true,
	}
	for mode, want := range relaxed {
		if got := mode.Relaxed(); got != want {
			t.Errorf("%q.Relaxed() = %v, want %v", mode, got, want)
		}
	}
}
