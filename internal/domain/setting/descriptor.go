package setting

import (
	"fmt"
	"slices"
	"strconv"
	"time"
)

// Kind is the value shape a descriptor validates and a Snapshot getter reads.
type Kind uint8

const (
	KindDuration Kind = iota + 1
	KindInt
	KindEnum
)

// Descriptor pins one tunable's shape, bounds, and compiled default. The SAME descriptor validates the env seed at boot and every DB write/read (ADR-0017: one registry, no drift). Bounds mirror their source ADRs and remain normative there (ADR-0010 amendment).
type Descriptor struct {
	Key     Key
	Kind    Kind
	Default string // canonical text form; always passes Validate

	// KindDuration bounds (inclusive).
	MinDuration, MaxDuration time.Duration
	// KindInt bounds (inclusive).
	MinInt, MaxInt int64
	// KindEnum vocabulary, exact match.
	Enum []string
}

// Validate reports whether value is a well-formed, in-bounds text form for this descriptor. All failures wrap ErrInvalidValue.
func (d Descriptor) Validate(value string) error {
	switch d.Kind {
	case KindDuration:
		v, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("%w: %q is not a duration for %s", ErrInvalidValue, value, d.Key)
		}
		if v < d.MinDuration || v > d.MaxDuration {
			return fmt.Errorf("%w: %s %s out of range [%s, %s]", ErrInvalidValue, d.Key, v, d.MinDuration, d.MaxDuration)
		}
	case KindInt:
		v, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("%w: %q is not an integer for %s", ErrInvalidValue, value, d.Key)
		}
		if v < d.MinInt || v > d.MaxInt {
			return fmt.Errorf("%w: %s %d out of range [%d, %d]", ErrInvalidValue, d.Key, v, d.MinInt, d.MaxInt)
		}
	case KindEnum:
		if !slices.Contains(d.Enum, value) {
			return fmt.Errorf("%w: %s %q not one of %v", ErrInvalidValue, d.Key, value, d.Enum)
		}
	default:
		return fmt.Errorf("%w: descriptor %s has no kind", ErrInvalidValue, d.Key)
	}
	return nil
}

// Bounds authoritative here and referenced by platform/config (ADR-0017): connection-test window per ADR-0014; backoff guardrails per ADR-0006 (the cap ceiling rules out a fat-fingered duration, the threshold ceiling only catches typos).
const (
	MinConnectionTestTimeout = time.Second
	MaxConnectionTestTimeout = time.Minute
	MaxLoginBackoffCap       = 24 * time.Hour
	MaxLoginBackoffThreshold = 1000
	// Approval validity window (PRD §4.3: default 24h, org-configurable 15 minutes to 7 days; ADR-0018).
	MinApprovalValidity = 15 * time.Minute
	MaxApprovalValidity = 7 * 24 * time.Hour
	// Governed execution lock wait (ADR-0021): short enough that a queued ACCESS EXCLUSIVE request cannot stall a target table for the whole statement timeout.
	MinExecutionLockTimeout = time.Second
	MaxExecutionLockTimeout = time.Minute
)

// LogLevels is the log_level vocabulary. platform/logging owns the runtime mapping; a config test pins the two lists together (the repo's established cross-package-literal pattern — domain imports nothing outward).
var LogLevels = []string{"debug", "info", "warn", "error"}

// registry is the Tier-C catalog, keyed for Lookup; ordered for All.
var registry = []Descriptor{
	{Key: KeyLogLevel, Kind: KindEnum, Default: "info", Enum: LogLevels},
	{Key: KeyLoginBackoffThreshold, Kind: KindInt, Default: "5", MinInt: 1, MaxInt: MaxLoginBackoffThreshold},
	{Key: KeyLoginBackoffBase, Kind: KindDuration, Default: "1m0s", MinDuration: time.Nanosecond, MaxDuration: MaxLoginBackoffCap},
	{Key: KeyLoginBackoffCap, Kind: KindDuration, Default: "15m0s", MinDuration: time.Nanosecond, MaxDuration: MaxLoginBackoffCap},
	{Key: KeyConnectionTestTimeout, Kind: KindDuration, Default: "10s", MinDuration: MinConnectionTestTimeout, MaxDuration: MaxConnectionTestTimeout},
	{Key: KeyApprovalValidity, Kind: KindDuration, Default: "24h0m0s", MinDuration: MinApprovalValidity, MaxDuration: MaxApprovalValidity},
	{Key: KeyExecutionLockTimeout, Kind: KindDuration, Default: "5s", MinDuration: MinExecutionLockTimeout, MaxDuration: MaxExecutionLockTimeout},
}

var byKey = func() map[Key]Descriptor {
	m := make(map[Key]Descriptor, len(registry))
	for _, d := range registry {
		m[d.Key] = d
	}
	return m
}()

// DefaultDuration returns a KindDuration descriptor's compiled default in its typed form. Defaults always validate (pinned by test), so parsing is total.
func (d Descriptor) DefaultDuration() time.Duration {
	v, _ := time.ParseDuration(d.Default)
	return v
}

// DefaultInt64 returns a KindInt descriptor's compiled default in its typed form.
func (d Descriptor) DefaultInt64() int64 {
	v, _ := strconv.ParseInt(d.Default, 10, 64)
	return v
}

// Lookup returns the descriptor for k, reporting whether k is in the catalog.
func Lookup(k Key) (Descriptor, bool) {
	d, ok := byKey[k]
	return d, ok
}

// All returns the catalog in its declaration order.
func All() []Descriptor {
	return slices.Clone(registry)
}
