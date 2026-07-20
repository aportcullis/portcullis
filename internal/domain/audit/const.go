package audit

// actor_type values — mirrors the audit_events.actor_type check constraint.
const (
	ActorUser    ActorType = "user"
	ActorSystem  ActorType = "system"
	ActorService ActorType = "service"
)

// action values — UPPER_SNAKE, stable; a queryable audit vocabulary.
const (
	ActionAuthLogin     Action = "AUTH_LOGIN"
	ActionAuthLogout    Action = "AUTH_LOGOUT"
	ActionAuthBootstrap Action = "AUTH_BOOTSTRAP"

	// Connection lifecycle (ADR-0014). Created/Updated/Archived commit in the
	// same transaction as the mutation (ADR-0009); Test is best-effort like
	// failed logins; TLSRelaxed accompanies a create/update that chose a mode
	// with no certificate validation (PRD §8.1).
	ActionConnectionCreated    Action = "CONNECTION_CREATED"
	ActionConnectionUpdated    Action = "CONNECTION_UPDATED"
	ActionConnectionArchived   Action = "CONNECTION_ARCHIVED"
	ActionConnectionTest       Action = "CONNECTION_TEST"
	ActionConnectionTLSRelaxed Action = "CONNECTION_TLS_RELAXED"

	// Connection policy (ADR-0015). PolicyUpdated commits in the same
	// transaction as the version insert; PolicyClassEnabled accompanies an
	// update that newly enabled write or ddl — §4.3's "enabling write/DDL
	// leaves an admin audit event" as a first-class queryable action (the
	// TLSRelaxed companion-event pattern).
	ActionConnectionPolicyUpdated      Action = "CONNECTION_POLICY_UPDATED"
	ActionConnectionPolicyClassEnabled Action = "CONNECTION_POLICY_CLASS_ENABLED"

	// Runtime settings (ADR-0017). SettingUpdated commits in the same
	// transaction as the settings row change (set AND reset), recording key
	// and old→new value verbatim — setting values are operational numbers,
	// never secrets (PRD §8.4: every admin settings change is audited).
	ActionSettingUpdated Action = "SETTING_UPDATED"
)

// outcome values — the terminal result of an action.
const (
	OutcomeSucceeded Outcome = "SUCCEEDED"
	OutcomeFailed    Outcome = "FAILED"
)

// target_type values — what kind of entity an event acted on.
const (
	// TargetTypeUser is the target_type for events acting on a user account.
	TargetTypeUser = "user"
	// TargetTypeConnection is the target_type for events acting on a registered
	// database connection; target_id carries the connection UUID. (The
	// audit_events.connection_id snapshot column stays reserved for
	// execution-path events — ADR-0014.)
	TargetTypeConnection = "connection"
	// TargetTypeSetting is the target_type for SETTING_UPDATED events; the
	// setting key travels in the event metadata (settings rows have no UUID —
	// the key is the identity, ADR-0017).
	TargetTypeSetting = "setting"
)
