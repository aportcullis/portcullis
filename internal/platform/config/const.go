package config

import (
	"regexp"
	"time"
)

// runtimeRolePattern is the shape runtime_role must have: it becomes a SQL identifier in migrations (PostgreSQL caps identifiers at 63 bytes). The same check runs again in postgres.Migrate (defense in depth).
var runtimeRolePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// maxArgon2Concurrent caps the configurable hash concurrency. Each Argon2id hash costs ~64 MiB, so 256 already permits ~16 GiB in flight — well beyond any real deployment and a firm guard against a fat-fingered value causing an OOM.
const maxArgon2Concurrent = 256

// Progressive-backoff guardrails (ADR-0006) live in the domain settings registry (ADR-0017): the same bounds validate the env seed here and every DB-store write/read, so the two paths cannot drift.

// GoogleCallbackPath is the only OAuth callback route the server mounts, so a google_redirect_url with any other path can never complete a login. The route pattern lives in transport (connectapi.OIDCCallbackPattern); a transport test asserts the two literals match — config is a leaf package and must not import transport (ADR-0007).
const GoogleCallbackPath = "/auth/google/callback"

// Connection-test timeout bounds (ADR-0014) likewise live in the domain settings registry (setting.MinConnectionTestTimeout / Max…).

// Metadata pool defaults and bounds (ADR-0010): boot-only settings sized for a single self-hosted instance; the bounds reject typos that would exhaust PostgreSQL connections or disable timeouts.
const (
	defaultDatabaseMaxConns                 = 16
	minDatabaseMaxConns                     = 2
	maxDatabaseMaxConns                     = 200
	defaultDatabaseAcquireTimeout           = 10 * time.Second
	minDatabaseAcquireTimeout               = 100 * time.Millisecond
	maxDatabaseAcquireTimeout               = time.Minute
	defaultDatabaseStatementTimeout         = 30 * time.Second
	minDatabaseStatementTimeout             = time.Second
	maxDatabaseStatementTimeout             = 10 * time.Minute
	defaultDatabaseLockTimeout              = 10 * time.Second
	minDatabaseLockTimeout                  = 100 * time.Millisecond
	maxDatabaseLockTimeout                  = 5 * time.Minute
	defaultDatabaseIdleInTransactionTimeout = time.Minute
	minDatabaseIdleInTransactionTimeout     = time.Second
	maxDatabaseIdleInTransactionTimeout     = time.Hour
)

// maxDrainDelay bounds the readiness delay so a configuration typo cannot stall shutdown before request draining begins.
const maxDrainDelay = 5 * time.Minute
