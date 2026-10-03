package setting_test

import (
	"errors"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/setting"
)

func TestDefaultsAreValid(t *testing.T) {
	t.Parallel()
	for _, d := range setting.All() {
		if err := d.Validate(d.Default); err != nil {
			t.Errorf("descriptor %q: default %q does not validate: %v", d.Key, d.Default, err)
		}
	}
}

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
		{setting.KeyConnectionTestTimeout, "500ms", false},
		{setting.KeyConnectionTestTimeout, "61s", false},
		{setting.KeyConnectionTestTimeout, "-5s", false},
		{setting.KeyConnectionTestTimeout, "abc", false},
		{setting.KeyConnectionTestTimeout, "10", false},

		{setting.KeyExecutionLockTimeout, "5s", true},
		{setting.KeyExecutionLockTimeout, "1s", true},
		{setting.KeyExecutionLockTimeout, "30s", true},
		{setting.KeyExecutionLockTimeout, "1m", true},
		{setting.KeyExecutionLockTimeout, "999ms", false},
		{setting.KeyExecutionLockTimeout, "61s", false},
		{setting.KeyExecutionLockTimeout, "-1s", false},
		{setting.KeyExecutionLockTimeout, "five", false},

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
		{setting.KeyLogLevel, "INFO", false},
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
