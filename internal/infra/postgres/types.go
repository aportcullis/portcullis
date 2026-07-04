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
