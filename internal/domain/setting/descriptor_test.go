package setting_test

import (
	"errors"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/setting"
)

// Every descriptor's compiled default must validate against its own rules —
// the default is the last fallback in the ADR-0017 precedence chain, so an
// invalid default would leave a key with no safe value at all.
func TestDefaultsAreValid(t *testing.T) {
	t.Parallel()
	for _, d := range setting.All() {
		if err := d.Validate(d.Default); err != nil {
			t.Errorf("descriptor %q: default %q does not validate: %v", d.Key, d.Default, err)
		}
	}
}

// The default pair must satisfy the cross-key backoff rule: Resolve's last
// repair lands on the defaults, so an inconsistent default pair would leave
// no consistent value at all.
func TestDefaultBackoffPairConsistent(t *testing.T) {
	t.Parallel()
	snap, invalid := setting.Resolve(nil, nil)
	if len(invalid) != 0 {
		t.Fatalf("invalid = %v, want none", invalid)
	}
	base, cap := snap.Duration(setting.KeyLoginBackoffBase), snap.Duration(setting.KeyLoginBackoffCap)
	if !setting.BackoffPairConsistent(base, cap) {
		t.Fatalf("default pair violates cap >= base: base=%v cap=%v", base, cap)
	}
}

func TestLookup(t *testing.T) {
	t.Parallel()
	if _, ok := setting.Lookup(setting.KeyConnectionTestTimeout); !ok {
		t.Fatalf("Lookup(%q) = not found, want descriptor", setting.KeyConnectionTestTimeout)
	}
	if _, ok := setting.Lookup(setting.Key("no_such_key")); ok {
		t.Fatal("Lookup of an unknown key succeeded, want not found")
	}
}

// Validate enforces the ADR-0010/0014/0006 bounds for each key; the same
// descriptor backs env bootstrap and the DB store (ADR-0017 no-drift).
func TestValidateBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key   setting.Key
		value string
		ok    bool
	}{
		{setting.KeyConnectionTestTimeout, "10s", true},
		{setting.KeyConnectionTestTimeout, "1s", true},
		{setting.KeyConnectionTestTimeout, "1m", true},
		{setting.KeyConnectionTestTimeout, "500ms", false}, // below ADR-0014 floor
		{setting.KeyConnectionTestTimeout, "61s", false},   // above ADR-0014 ceiling
		{setting.KeyConnectionTestTimeout, "-5s", false},
		{setting.KeyConnectionTestTimeout, "abc", false},
		{setting.KeyConnectionTestTimeout, "10", false}, // bare number is not a duration

		{setting.KeyLoginBackoffThreshold, "5", true},
		{setting.KeyLoginBackoffThreshold, "1", true},
		{setting.KeyLoginBackoffThreshold, "1000", true},
		{setting.KeyLoginBackoffThreshold, "0", false},
		{setting.KeyLoginBackoffThreshold, "1001", false},
		{setting.KeyLoginBackoffThreshold, "5.5", false},
		{setting.KeyLoginBackoffThreshold, "", false},

		{setting.KeyLoginBackoffBase, "1m", true},
		{setting.KeyLoginBackoffBase, "0s", false}, // must be positive
		{setting.KeyLoginBackoffBase, "25h", false},

		{setting.KeyLoginBackoffCap, "15m", true},
		{setting.KeyLoginBackoffCap, "24h", true},
		{setting.KeyLoginBackoffCap, "25h", false},

		{setting.KeyLogLevel, "debug", true},
		{setting.KeyLogLevel, "info", true},
		{setting.KeyLogLevel, "warn", true},
		{setting.KeyLogLevel, "error", true},
		{setting.KeyLogLevel, "verbose", false},
		{setting.KeyLogLevel, "INFO", false}, // vocabulary is lowercase, exact
	}
	for _, tt := range tests {
		d, ok := setting.Lookup(tt.key)
		if !ok {
			t.Fatalf("Lookup(%q): descriptor missing", tt.key)
		}
		err := d.Validate(tt.value)
		if tt.ok && err != nil {
			t.Errorf("Validate(%q, %q) = %v, want ok", tt.key, tt.value, err)
		}
		if !tt.ok {
			if err == nil {
				t.Errorf("Validate(%q, %q) = ok, want error", tt.key, tt.value)
			} else if !errors.Is(err, setting.ErrInvalidValue) {
				t.Errorf("Validate(%q, %q) error = %v, want ErrInvalidValue", tt.key, tt.value, err)
			}
		}
	}
}
