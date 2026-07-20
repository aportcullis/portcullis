// Package setting is the engine-agnostic vocabulary for the runtime settings
// store (ADR-0017): the fixed catalog of operator-tunable keys (Tier C), the
// per-key descriptor that validates values for BOTH env bootstrap and the DB
// store, and the single precedence rule (DB override → env seed → compiled
// default). Which keys exist is code; their values live in the database.
package setting

// Key names one operator-tunable setting. Keys are the exact config keys
// (PORTCULLIS_<KEY> in the environment, the `key` column in the settings
// table), so one name identifies a setting across every layer.
type Key string

// The Tier-C catalog (ADR-0017). A new tunable is added here together with
// its descriptor in registry — nowhere else.
const (
	KeyLogLevel              Key = "log_level"
	KeyLoginBackoffThreshold Key = "login_backoff_threshold"
	KeyLoginBackoffBase      Key = "login_backoff_base"
	KeyLoginBackoffCap       Key = "login_backoff_cap"
	KeyConnectionTestTimeout Key = "connection_test_timeout"
)
