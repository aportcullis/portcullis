package config

import (
	"regexp"
	"time"
)

// runtimeRolePattern is the shape runtime_role must have: it becomes a SQL
// identifier in migrations (PostgreSQL caps identifiers at 63 bytes). The same
// check runs again in postgres.Migrate (defense in depth).
var runtimeRolePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// maxArgon2Concurrent caps the configurable hash concurrency. Each Argon2id hash
// costs ~64 MiB, so 256 already permits ~16 GiB in flight — well beyond any real
// deployment and a firm guard against a fat-fingered value causing an OOM.
const maxArgon2Concurrent = 256

// maxDrainDelay caps the readiness-drain window. It blocks shutdown before
// in-flight requests are even drained (the delay and the shutdown timeout are
// sequential, ADR-0010), so a fat-fingered value (e.g. "2h" instead of "2s") would
// hang the process well past any orchestrator's grace period. A few seconds is the
// intended range; five minutes is a generous ceiling that still catches a typo.
const maxDrainDelay = 5 * time.Minute
