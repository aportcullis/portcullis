package postgres

// Advisory-lock classes — the first argument of the two-arg pg_advisory_xact_lock,
// so each purpose gets its own keyspace and a per-object id in one class can never
// collide with another class's lock.
const (
	lockClassBootstrap int32 = 1 // first-run bootstrap (one global lock, object 0)
	lockClassSession   int32 = 2 // per-user session rotation
	lockClassMigrate   int32 = 3 // schema migration (one global lock, object 0)
)

// defaultRuntimeRole is the runtime role name the migration SQL is written
// against; Migrate substitutes it when WithRuntimeRole configures another name.
const defaultRuntimeRole = "portcullis_runtime"
