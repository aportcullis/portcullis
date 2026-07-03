package postgres

// MigrateOption customizes Migrate (functional options, so existing callers
// stay source-compatible).
type MigrateOption func(*migrateConfig)

// migrateConfig collects the resolved Migrate options.
type migrateConfig struct {
	// runtimeRole is the name of the least-privilege runtime role the migrations
	// create and grant (ADR-0009). Configurable so installs sharing one
	// PostgreSQL cluster don't share (and thereby merge) a role.
	runtimeRole string
}

// privMatrix is the effective-privilege snapshot of one role/user against the
// audit boundary (ADR-0009), shared by the configured-role check (owner
// connection) and the runtime-connection check so the two can't drift apart.
type privMatrix struct {
	connect     bool // CONNECT on the current database
	auditRead   bool // SELECT on audit_events
	auditAppend bool // INSERT on audit_events
	auditMutate bool // any of UPDATE/DELETE/TRUNCATE/TRIGGER/REFERENCES/MAINTAIN on audit_events
	historyAny  bool // any privilege at all on schema_migrations
}
