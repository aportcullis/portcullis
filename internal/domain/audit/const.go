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

	// Connection lifecycle (ADR-0014). Created/Updated/Archived commit in the same transaction as the mutation (ADR-0009); Test is best-effort like failed logins; TLSRelaxed accompanies a create/update that chose a mode with no certificate validation (PRD §8.1).
	ActionConnectionCreated    Action = "CONNECTION_CREATED"
	ActionConnectionUpdated    Action = "CONNECTION_UPDATED"
	ActionConnectionArchived   Action = "CONNECTION_ARCHIVED"
	ActionConnectionTest       Action = "CONNECTION_TEST"
	ActionConnectionTLSRelaxed Action = "CONNECTION_TLS_RELAXED"

	// Connection policy (ADR-0015). PolicyUpdated commits in the same transaction as the version insert; PolicyClassEnabled accompanies an update that newly enabled write or ddl — §4.3's "enabling write/DDL leaves an admin audit event" as a first-class queryable action (the TLSRelaxed companion-event pattern).
	ActionConnectionPolicyUpdated      Action = "CONNECTION_POLICY_UPDATED"
	ActionConnectionPolicyClassEnabled Action = "CONNECTION_POLICY_CLASS_ENABLED"

	// Request transitions commit audit events atomically. Quorum-zero submission also records a system approval; submitted events retain redacted SQL and snapshot evidence (ADR-0018).
	ActionAccessRequestCreated   Action = "ACCESS_REQUEST_CREATED"
	ActionAccessRequestUpdated   Action = "ACCESS_REQUEST_UPDATED"
	ActionAccessRequestSubmitted Action = "ACCESS_REQUEST_SUBMITTED"
	ActionAccessRequestApproved  Action = "ACCESS_REQUEST_APPROVED"
	ActionAccessRequestRejected  Action = "ACCESS_REQUEST_REJECTED"
	ActionAccessRequestCancelled Action = "ACCESS_REQUEST_CANCELLED"
	ActionAccessRequestExpired   Action = "ACCESS_REQUEST_EXPIRED"

	// Runtime settings (ADR-0017). SettingUpdated commits in the same transaction as the settings row change (set AND reset), recording key and old→new value verbatim — setting values are operational numbers, never secrets (PRD §8.4: every admin settings change is audited).
	ActionSettingUpdated Action = "SETTING_UPDATED"

	ActionExecutionStarted       Action = "EXECUTION_STARTED"
	ActionExecutionFinished      Action = "EXECUTION_FINISHED"
	ActionLateCompletionObserved Action = "LATE_COMPLETION_OBSERVED"

	// ActionResultEvicted records the eviction of a live result snapshot by the result store (ADR-0011); metadata carries the cause.
	ActionResultEvicted Action = "RESULT_EVICTED"
	// ActionKeyRotationBatch records one committed key-rotation batch for an organization (ADR-0003); metadata carries the active version and row count.
	ActionKeyRotationBatch Action = "KEY_ROTATION_BATCH"

	// User and role administration (ADR-0053). Each commits with its mutation; escalation, self-administration and last-administrator refusals are recorded best-effort as FAILED events of the attempted action.
	ActionUserCreated         Action = "USER_CREATED"
	ActionUserSetupLinkIssued Action = "USER_SETUP_LINK_ISSUED"
	ActionUserPasswordSet     Action = "USER_PASSWORD_SET"
	ActionUserDisabled        Action = "USER_DISABLED"
	ActionUserEnabled         Action = "USER_ENABLED"
	ActionUserRoleAssigned    Action = "USER_ROLE_ASSIGNED"
	ActionRoleCreated         Action = "ROLE_CREATED"
	ActionRoleUpdated         Action = "ROLE_UPDATED"
	ActionRoleDeleted         Action = "ROLE_DELETED"
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
	// TargetTypeConnection is the target_type for events acting on a registered database connection; target_id carries the connection UUID. (The audit_events.connection_id snapshot column stays reserved for execution-path events — ADR-0014.)
	TargetTypeConnection = "connection"
	// TargetTypeSetting is the target_type for SETTING_UPDATED events; the setting key travels in the event metadata (settings rows have no UUID — the key is the identity, ADR-0017).
	TargetTypeSetting = "setting"
	// TargetTypeAccessRequest is the target_type for ACCESS_REQUEST_* events; target_id carries the request UUID (ADR-0018).
	TargetTypeAccessRequest = "access_request"
	// TargetTypeResultSet is the target_type for RESULT_EVICTED events; target_id carries the result snapshot UUID (ADR-0011).
	TargetTypeResultSet = "result_set"
	// TargetTypeEncryption is the target_type for KEY_ROTATION_BATCH events, which act on an organization's encrypted records as a whole (ADR-0003).
	TargetTypeEncryption = "encryption"
	// TargetTypeRole is the target_type for ROLE_* events; target_id carries the role UUID (ADR-0053).
	TargetTypeRole = "role"
)
