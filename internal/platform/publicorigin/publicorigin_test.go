package publicorigin_test

import (
	"slices"
	"testing"

	"github.com/aportcullis/portcullis/internal/platform/publicorigin"
)

func TestParsePolicyNormalizesConfiguredOrigins(t *testing.T) {
	t.Parallel()
	policy, err := publicorigin.ParsePolicy([]string{" HTTPS://Portcullis.Example.com/ ", "http://10.0.0.7:8080", ""})
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	if policy.IsLoopbackDefault() {
		t.Error("configured origins must replace the loopback default")
	}
	want := []string{"https://portcullis.example.com", "http://10.0.0.7:8080"}
	if got := policy.TrustedOrigins(); !slices.Equal(got, want) {
		t.Errorf("TrustedOrigins = %q, want %q", got, want)
	}
}

func TestConfiguredPolicyAllowsOnlyPublishedHosts(t *testing.T) {
	t.Parallel()
	policy, err := publicorigin.ParsePolicy([]string{"https://portcullis.example.com", "http://10.0.0.7:8080"})
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	cases := []struct {
		host string
		want bool
	}{
		{"portcullis.example.com", true},
		{"PORTCULLIS.example.com", true},
		{"portcullis.example.com:443", true},
		{"10.0.0.7:8080", true},
		{"attacker.example", false},
		{"portcullis.example.com:8443", false},
		{"10.0.0.7", false},
		{"localhost:8080", false},
		{"portcullis.example.com.attacker.example", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := policy.AllowsHost(tc.host); got != tc.want {
			t.Errorf("AllowsHost(%q) = %t, want %t", tc.host, got, tc.want)
		}
	}
}

func TestLoopbackDefaultAllowsOnlyLoopbackHosts(t *testing.T) {
	t.Parallel()
	policy, err := publicorigin.ParsePolicy(nil)
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	if !policy.IsLoopbackDefault() || len(policy.TrustedOrigins()) != 0 {
		t.Fatalf("empty configuration = default %t, origins %q; want loopback default without trusted origins", policy.IsLoopbackDefault(), policy.TrustedOrigins())
	}
	cases := []struct {
		host string
		want bool
	}{
		{"localhost:8080", true},
		{"LOCALHOST", true},
		{"127.0.0.1:18080", true},
		{"[::1]:8080", true},
		{"attacker.example:8080", false},
		{"192.168.1.20:8080", false},
		{"localhost.attacker.example", false},
		{"0.0.0.0:8080", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := policy.AllowsHost(tc.host); got != tc.want {
			t.Errorf("AllowsHost(%q) = %t, want %t", tc.host, got, tc.want)
		}
	}
}

func TestParsePolicyRejectsNonOrigins(t *testing.T) {
	t.Parallel()
	for _, origin := range []string{
		"*",
		"portcullis.example.com",
		"ftp://portcullis.example.com",
		"https://portcullis.example.com/app",
		"https://user:secret@portcullis.example.com",
		"https://portcullis.example.com?next=1",
		"https://portcullis.example.com#top",
		"https://*.example.com",
		"https://",
		"https://portcullis.example.com:99999",
	} {
		if _, err := publicorigin.ParsePolicy([]string{origin}); err == nil {
			t.Errorf("ParsePolicy(%q) succeeded, want rejection", origin)
		}
	}
}
