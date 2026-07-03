package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// VerifyRuntimeConnection asserts that the connection the server actually runs
// on upholds the audit boundary (ADR-0009) — verifying the configured group
// role alone is not enough, because the DSN's login user is what acts: it could
// be the schema owner or a superuser (implicit, irrevocable privileges), carry
// direct grants, inherit another privileged role, or point at a different
// database entirely. Run against the runtime pool right after connecting;
// PORTCULLIS_ALLOW_PRIVILEGED_RUNTIME downgrades a failure to a warning for
// single-role dev setups.
func VerifyRuntimeConnection(ctx context.Context, pool *pgxpool.Pool, runtimeRole string) error {
	// Wrong database / never migrated: fail with a pointer at the schema rather
	// than a confusing SQL error on first use.
	var auditOK, historyOK bool
	if err := pool.QueryRow(ctx,
		`select to_regclass('audit_events') is not null, to_regclass('schema_migrations') is not null`,
	).Scan(&auditOK, &historyOK); err != nil {
		return fmt.Errorf("inspect runtime database: %w", err)
	}
	if !auditOK || !historyOK {
		return fmt.Errorf("runtime database has no audit_events/schema_migrations — is PORTCULLIS_DATABASE_URL pointing at the migrated database?")
	}

	var user string
	var member, canBecomeOwner bool
	if err := pool.QueryRow(ctx, `
		select current_user,
		       current_user = $1 or pg_has_role(current_user, $1, 'MEMBER'),
		       pg_has_role(current_user, (select relowner from pg_class where oid = 'audit_events'::regclass), 'MEMBER')`,
		runtimeRole,
	).Scan(&user, &member, &canBecomeOwner); err != nil {
		return fmt.Errorf("inspect runtime connection: %w", err)
	}
	if !member {
		return fmt.Errorf("runtime user %q is not a member of runtime role %q (ADR-0009) — grant it or fix PORTCULLIS_DATABASE_URL/PORTCULLIS_RUNTIME_ROLE", user, runtimeRole)
	}
	if err := checkRuntimeRoleAttributes(ctx, pool, user); err != nil {
		return err
	}
	// The owner can ALTER the audit table and disable its append-only triggers —
	// a runtime user that can SET ROLE into the owner voids the boundary.
	// (A superuser evaluates true here and on the matrix below.)
	if canBecomeOwner {
		return fmt.Errorf("runtime user %q can become the audit_events owner; the server must not run with owner/superuser credentials (ADR-0009) — use a least-privilege user, or set PORTCULLIS_ALLOW_PRIVILEGED_RUNTIME=true for local development only", user)
	}

	m, err := queryPrivilegeMatrix(ctx, pool, user)
	if err != nil {
		return fmt.Errorf("verify runtime user %q: %w", user, err)
	}
	switch {
	case !m.connect || !m.auditRead || !m.auditAppend:
		return fmt.Errorf("runtime user %q lacks required privileges (CONNECT=%t, audit SELECT=%t, audit INSERT=%t) (ADR-0009)", user, m.connect, m.auditRead, m.auditAppend)
	case m.auditMutate:
		return fmt.Errorf("runtime user %q holds UPDATE, DELETE, TRUNCATE, TRIGGER, REFERENCES, or MAINTAIN on audit_events; the append-only boundary is broken (ADR-0009)", user)
	case m.historyAny:
		return fmt.Errorf("runtime user %q has access to schema_migrations; migration history must be owner-only (ADR-0009)", user)
	}
	return nil
}
