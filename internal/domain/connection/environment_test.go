package connection_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/connection"
)

func TestParseEnvironment(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		want    connection.Environment
		wantErr bool
	}{
		{"", connection.EnvironmentDevelopment, false},
		{"development", connection.EnvironmentDevelopment, false},
		{"production", connection.EnvironmentProduction, false},
		{"staging", "", true},
		{"prod", "", true},
		{"PRODUCTION", "", true},
	}
	for _, tt := range cases {
		got, err := connection.ParseEnvironment(tt.in)
		if tt.wantErr {
			if !errors.Is(err, connection.ErrInvalidEnvironment) {
				t.Errorf("ParseEnvironment(%q) err = %v, want ErrInvalidEnvironment", tt.in, err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("ParseEnvironment(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
}

func TestNewValidatesEnvironmentAndDescription(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	target := validTarget(t)

	if _, err := connection.New("c1", "org1", connection.DBTypePostgreSQL, "n", "staging", "", target, connection.TLSModeVerifyFull, "u1", now); !errors.Is(err, connection.ErrInvalidEnvironment) {
		t.Errorf("unknown environment: err = %v, want ErrInvalidEnvironment", err)
	}
	if _, err := connection.New("c1", "org1", connection.DBTypePostgreSQL, "n", "", "", target, connection.TLSModeVerifyFull, "u1", now); !errors.Is(err, connection.ErrInvalidEnvironment) {
		t.Errorf("empty environment: err = %v, want ErrInvalidEnvironment (parse at the boundary)", err)
	}
	if _, err := connection.New("c1", "org1", connection.DBTypePostgreSQL, "n", connection.EnvironmentDevelopment, strings.Repeat("a", 501), target, connection.TLSModeVerifyFull, "u1", now); !errors.Is(err, connection.ErrInvalidDescription) {
		t.Errorf("oversized description: err = %v, want ErrInvalidDescription", err)
	}
}

func TestValidateDescription(t *testing.T) {
	t.Parallel()
	ok := []string{
		"",
		"read replica for analytics",
		"line one\nline two",
		strings.Repeat("한", 500),
	}
	for _, d := range ok {
		if err := connection.ValidateDescription(d); err != nil {
			t.Errorf("ValidateDescription(%q) = %v, want nil", d, err)
		}
	}

	bad := []string{
		strings.Repeat("a", 501),
		"nul\x00byte",
		"esc\x1b[31mape",
		"zero\u200bwidth",
	}
	for _, d := range bad {
		if err := connection.ValidateDescription(d); !errors.Is(err, connection.ErrInvalidDescription) {
			t.Errorf("ValidateDescription(%q) = %v, want ErrInvalidDescription", d, err)
		}
	}
}
