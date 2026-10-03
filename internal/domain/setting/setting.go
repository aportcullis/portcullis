// Package setting defines validated runtime tunables and DB override → env seed → compiled default precedence (ADR-0017).
package setting

// Key names one operator-tunable setting. Keys are the exact config keys (PORTCULLIS_<KEY> in the environment, the `key` column in the settings table), so one name identifies a setting across every layer.
type Key string

// The Tier-C catalog (ADR-0017). A new tunable is added here together with its descriptor in registry — nowhere else.
const (
	KeyLogLevel              Key = "log_level"
	KeyLoginBackoffThreshold Key = "login_backoff_threshold"
	KeyLoginBackoffBase      Key = "login_backoff_base"
	KeyLoginBackoffCap       Key = "login_backoff_cap"
	KeyConnectionTestTimeout Key = "connection_test_timeout"
	KeyApprovalValidity      Key = "approval_validity"
	KeyExecutionLockTimeout  Key = "execution_lock_timeout"
)
