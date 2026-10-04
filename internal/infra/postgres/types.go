package postgres

import "time"

// MigrateOption customizes Migrate (functional options, so existing callers stay source-compatible).
type MigrateOption func(*migrateConfig)

// migrateConfig collects the resolved Migrate options.
type migrateConfig struct {
	// runtimeRole is the name of the least-privilege runtime role the migrations create and grant (ADR-0009). Configurable so installs sharing one PostgreSQL cluster don't share (and thereby merge) a role.
	runtimeRole string
	// lockWait bounds how long Migrate waits for another instance's migration lock (ADR-0009).
	lockWait time.Duration
	// timeouts bound each migration transaction's lock waits and statements.
	timeouts migrationTimeouts
}

// migrationTimeouts bounds one migration transaction; a lock timeout rolls back and retries the whole file.
type migrationTimeouts struct {
	lockTimeout      time.Duration
	statementTimeout time.Duration
	attempts         int
	retryBackoff     time.Duration
}

// migrationFile is one embedded migration with the sha256 of its exact bytes.
type migrationFile struct {
	version  string
	body     string
	checksum []byte
}
