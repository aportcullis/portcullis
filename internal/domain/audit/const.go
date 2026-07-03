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
)

// outcome values — the terminal result of an action.
const (
	OutcomeSucceeded Outcome = "SUCCEEDED"
	OutcomeFailed    Outcome = "FAILED"
)

// TargetTypeUser is the target_type for events acting on a user account.
const TargetTypeUser = "user"
