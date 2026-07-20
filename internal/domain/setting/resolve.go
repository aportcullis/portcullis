package setting

import (
	"fmt"
	"strconv"
	"time"
)

// Invalid reports one entry Resolve rejected and the layer it came from; the
// caller logs it (a warning, ADR-0017) — resolution itself never fails.
type Invalid struct {
	Key    Key
	Source string // "seed" | "override"
	Err    error
}

// Snapshot is one immutable, fully-resolved view of every catalog key. Every
// value is guaranteed valid and cross-key-consistent, so the typed getters
// are total for catalog keys; an unknown key returns the zero value
// (programmer error, caught by tests).
type Snapshot struct {
	values map[Key]string
}

// backoffPair is the one cross-key constraint (cap ≥ base, ADR-0006). A
// second paired constraint is the trigger to generalize this into a
// descriptor-level mechanism; one pair does not justify it yet.
var backoffPair = [2]Key{KeyLoginBackoffBase, KeyLoginBackoffCap}

// BackoffPairConsistent reports whether a base/cap pair satisfies the
// ADR-0006 rule cap ≥ base. Shared by Resolve and the config bootstrap
// (ADR-0017: one registry, one rule).
func BackoffPairConsistent(base, cap time.Duration) bool {
	return cap >= base
}

// Resolve applies the single ADR-0017 precedence rule per key — override when
// valid, else seed when valid, else the compiled default — and guarantees the
// backoff pair rule on the RETURNED snapshot regardless of which layer
// supplied the values: a violating pair first drops the pair's overrides, and
// if the remaining seed/default mix still violates, both keys drop to their
// (mutually consistent) defaults. Unknown and invalid entries are reported in
// the Invalid slice and skipped.
func Resolve(seeds, overrides map[Key]string) (Snapshot, []Invalid) {
	var invalid []Invalid
	for k := range seeds {
		if _, ok := byKey[k]; !ok {
			invalid = append(invalid, Invalid{Key: k, Source: "seed", Err: ErrUnknownKey})
		}
	}
	for k := range overrides {
		if _, ok := byKey[k]; !ok {
			invalid = append(invalid, Invalid{Key: k, Source: "override", Err: ErrUnknownKey})
		}
	}

	values := make(map[Key]string, len(registry))
	fromOverride := make(map[Key]bool, len(overrides))
	for _, d := range registry {
		if s, ok := seeds[d.Key]; ok {
			if err := d.Validate(s); err != nil {
				invalid = append(invalid, Invalid{Key: d.Key, Source: "seed", Err: err})
			}
		}
		if o, ok := overrides[d.Key]; ok {
			if err := d.Validate(o); err != nil {
				invalid = append(invalid, Invalid{Key: d.Key, Source: "override", Err: err})
			} else {
				values[d.Key] = o
				fromOverride[d.Key] = true
				continue
			}
		}
		values[d.Key] = seedOrDefault(d, seeds)
	}

	snap := Snapshot{values: values}
	if !snap.backoffPairOK() {
		// First repair: the pair's overrides are the mutable layer — drop them
		// and fall back to seed/default, blaming exactly the dropped entries.
		for _, k := range backoffPair {
			if fromOverride[k] {
				invalid = append(invalid, Invalid{Key: k, Source: "override", Err: errBackoffPair})
				values[k] = seedOrDefault(byKey[k], seeds)
			}
		}
		// Second repair: a seed/default mix can still violate (config.Load
		// guarantees consistency only when BOTH seeds are present and valid) —
		// land on the defaults, which are consistent by construction.
		if !snap.backoffPairOK() {
			for _, k := range backoffPair {
				d := byKey[k]
				if values[k] == d.Default {
					continue
				}
				invalid = append(invalid, Invalid{Key: k, Source: "seed", Err: errBackoffPair})
				values[k] = d.Default
			}
		}
	}
	return snap, invalid
}

// seedOrDefault resolves one key from the seed layer down: a valid seed wins,
// else the compiled default. Seed invalidity is reported once by the caller's
// main pass, never here.
func seedOrDefault(d Descriptor, seeds map[Key]string) string {
	if s, ok := seeds[d.Key]; ok && d.Validate(s) == nil {
		return s
	}
	return d.Default
}

func (s Snapshot) backoffPairOK() bool {
	return BackoffPairConsistent(s.Duration(KeyLoginBackoffBase), s.Duration(KeyLoginBackoffCap))
}

var errBackoffPair = fmt.Errorf("%w: login_backoff_cap below login_backoff_base; value dropped to the next layer", ErrInvalidValue)

// Duration returns the resolved value of a KindDuration key.
func (s Snapshot) Duration(k Key) time.Duration {
	v, err := time.ParseDuration(s.values[k])
	if err != nil {
		return 0
	}
	return v
}

// Int returns the resolved value of a KindInt key.
func (s Snapshot) Int(k Key) int64 {
	v, err := strconv.ParseInt(s.values[k], 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// String returns the resolved text value of k ("" for an unknown key).
func (s Snapshot) String(k Key) string {
	return s.values[k]
}
