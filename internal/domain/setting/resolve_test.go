package setting_test

import (
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/setting"
)

// Resolve applies the single ADR-0017 precedence rule: DB override when
// present and valid → env seed → compiled default. Invalid entries are
// reported and skipped — the effective value is always safe.
func TestResolvePrecedence(t *testing.T) {
	t.Parallel()

	t.Run("default when nothing is set", func(t *testing.T) {
		t.Parallel()
		snap, invalid := setting.Resolve(nil, nil)
		if len(invalid) != 0 {
			t.Fatalf("invalid = %v, want none", invalid)
		}
		if got := snap.Duration(setting.KeyConnectionTestTimeout); got != 10*time.Second {
			t.Fatalf("connection_test_timeout = %v, want the 10s default", got)
		}
		if got := snap.Int(setting.KeyLoginBackoffThreshold); got != 5 {
			t.Fatalf("login_backoff_threshold = %d, want the default 5", got)
		}
		if got := snap.String(setting.KeyLogLevel); got != "info" {
			t.Fatalf("log_level = %q, want the default info", got)
		}
	})

	t.Run("seed beats default, override beats seed", func(t *testing.T) {
		t.Parallel()
		seeds := map[setting.Key]string{
			setting.KeyConnectionTestTimeout: "20s",
			setting.KeyLogLevel:              "warn",
		}
		overrides := map[setting.Key]string{
			setting.KeyLogLevel: "debug",
		}
		snap, invalid := setting.Resolve(seeds, overrides)
		if len(invalid) != 0 {
			t.Fatalf("invalid = %v, want none", invalid)
		}
		if got := snap.Duration(setting.KeyConnectionTestTimeout); got != 20*time.Second {
			t.Fatalf("connection_test_timeout = %v, want the 20s seed", got)
		}
		if got := snap.String(setting.KeyLogLevel); got != "debug" {
			t.Fatalf("log_level = %q, want the debug override", got)
		}
	})

	t.Run("invalid override falls back to seed and is reported", func(t *testing.T) {
		t.Parallel()
		seeds := map[setting.Key]string{setting.KeyConnectionTestTimeout: "20s"}
		overrides := map[setting.Key]string{setting.KeyConnectionTestTimeout: "2h"}
		snap, invalid := setting.Resolve(seeds, overrides)
		if got := snap.Duration(setting.KeyConnectionTestTimeout); got != 20*time.Second {
			t.Fatalf("connection_test_timeout = %v, want the 20s seed after the invalid override", got)
		}
		if len(invalid) != 1 || invalid[0].Key != setting.KeyConnectionTestTimeout {
			t.Fatalf("invalid = %v, want the one rejected override reported", invalid)
		}
	})

	t.Run("invalid seed falls back to default and is reported", func(t *testing.T) {
		t.Parallel()
		seeds := map[setting.Key]string{setting.KeyLogLevel: "loud"}
		snap, invalid := setting.Resolve(seeds, nil)
		if got := snap.String(setting.KeyLogLevel); got != "info" {
			t.Fatalf("log_level = %q, want the info default after the invalid seed", got)
		}
		if len(invalid) != 1 || invalid[0].Key != setting.KeyLogLevel {
			t.Fatalf("invalid = %v, want the one rejected seed reported", invalid)
		}
	})

	t.Run("unknown key is reported and ignored", func(t *testing.T) {
		t.Parallel()
		overrides := map[setting.Key]string{setting.Key("no_such_key"): "1"}
		_, invalid := setting.Resolve(nil, overrides)
		if len(invalid) != 1 || invalid[0].Key != setting.Key("no_such_key") {
			t.Fatalf("invalid = %v, want the unknown key reported", invalid)
		}
	})
}

// The backoff pair rule (cap ≥ base, ADR-0006 via config) spans two keys and
// must hold on EVERY returned snapshot, whichever layer produced the values:
// a violating pair first drops both keys' overrides, and if the seed/default
// mix still violates, both keys drop to their (consistent) defaults.
func TestResolveBackoffPairRule(t *testing.T) {
	t.Parallel()

	t.Run("cap override below the default base is dropped", func(t *testing.T) {
		t.Parallel()
		overrides := map[setting.Key]string{setting.KeyLoginBackoffCap: "30s"} // default base is 1m
		snap, invalid := setting.Resolve(nil, overrides)
		if got := snap.Duration(setting.KeyLoginBackoffCap); got != 15*time.Minute {
			t.Fatalf("login_backoff_cap = %v, want the 15m default after the pair violation", got)
		}
		if len(invalid) == 0 {
			t.Fatal("pair violation was not reported")
		}
	})

	t.Run("consistent override pair is accepted", func(t *testing.T) {
		t.Parallel()
		overrides := map[setting.Key]string{
			setting.KeyLoginBackoffBase: "30s",
			setting.KeyLoginBackoffCap:  "5m",
		}
		snap, invalid := setting.Resolve(nil, overrides)
		if len(invalid) != 0 {
			t.Fatalf("invalid = %v, want none", invalid)
		}
		if got := snap.Duration(setting.KeyLoginBackoffBase); got != 30*time.Second {
			t.Fatalf("login_backoff_base = %v, want 30s", got)
		}
		if got := snap.Duration(setting.KeyLoginBackoffCap); got != 5*time.Minute {
			t.Fatalf("login_backoff_cap = %v, want 5m", got)
		}
	})

	t.Run("invalid cap seed cannot leave cap below a valid base seed", func(t *testing.T) {
		t.Parallel()
		// The invalid cap seed falls to the 15m default; base's 20m seed would
		// then violate cap ≥ base with no override involved — both keys must
		// drop to their defaults instead of returning an inconsistent pair.
		seeds := map[setting.Key]string{
			setting.KeyLoginBackoffBase: "20m",
			setting.KeyLoginBackoffCap:  "bogus",
		}
		snap, invalid := setting.Resolve(seeds, nil)
		base, cap := snap.Duration(setting.KeyLoginBackoffBase), snap.Duration(setting.KeyLoginBackoffCap)
		if cap < base {
			t.Fatalf("snapshot violates cap >= base: base=%v cap=%v", base, cap)
		}
		if base != time.Minute || cap != 15*time.Minute {
			t.Fatalf("want the default pair (1m, 15m) after the seed-mix violation, got (%v, %v)", base, cap)
		}
		if len(invalid) == 0 {
			t.Fatal("no Invalid reported for the dropped seed pair")
		}
	})

	t.Run("override drop falls back to a still-violating seed mix and lands on defaults", func(t *testing.T) {
		t.Parallel()
		// Overrides violate (5m > 2m) → dropped; the caller passed only a base
		// seed (30m) which still violates against the 15m default cap → both
		// keys must land on defaults, never on the inconsistent mix.
		seeds := map[setting.Key]string{setting.KeyLoginBackoffBase: "30m"}
		overrides := map[setting.Key]string{
			setting.KeyLoginBackoffBase: "5m",
			setting.KeyLoginBackoffCap:  "2m",
		}
		snap, _ := setting.Resolve(seeds, overrides)
		base, cap := snap.Duration(setting.KeyLoginBackoffBase), snap.Duration(setting.KeyLoginBackoffCap)
		if cap < base {
			t.Fatalf("snapshot violates cap >= base: base=%v cap=%v", base, cap)
		}
	})

	t.Run("pair violation blames the keys that actually contributed", func(t *testing.T) {
		t.Parallel()
		overrides := map[setting.Key]string{setting.KeyLoginBackoffBase: "20m"} // cap stays at the 15m default
		_, invalid := setting.Resolve(nil, overrides)
		if len(invalid) != 1 {
			t.Fatalf("invalid = %v, want exactly the dropped base override", invalid)
		}
		if invalid[0].Key != setting.KeyLoginBackoffBase || invalid[0].Source != "override" {
			t.Fatalf("invalid = %+v, want key=login_backoff_base source=override", invalid[0])
		}
	})
}
