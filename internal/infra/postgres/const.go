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

// uniqueViolationCode is PostgreSQL's SQLSTATE for unique_violation. The store
// branches on it to map a duplicate to a domain sentinel (the driver reports the
// code separately from the free-text message, which ErrorLogFields strips).
const uniqueViolationCode = "23505"

// Unique-constraint / index names the store maps to domain sentinels (a unique
// index violation reports the index name in ConstraintName).
const (
	usersEmailLowerIndex    = "users_email_lower_idx"
	connectionsOrgNameIndex = "connections_org_name"
)

// The dangerous cluster-attribute policy (ADR-0009): attributes a
// least-privilege runtime principal must never hold. The SQL predicate and the
// operator-facing list are kept together — the single source the owner-side
// migration checks (privcheck.go) and the runtime-connection check share, so
// they can't drift.
const (
	unsafeRoleAttrsSQL  = `rolsuper or rolcreaterole or rolcreatedb or rolbypassrls or rolreplication`
	unsafeRoleAttrsList = "SUPERUSER/CREATEROLE/CREATEDB/BYPASSRLS/REPLICATION"
)
