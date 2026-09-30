package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// VerifyRuntimeConnection checks SESSION_USER, which can regain its own privileges with SET ROLE NONE; checking current_user alone misses excess login privileges (ADR-0009).
func VerifyRuntimeConnection(ctx context.Context, pool *pgxpool.Pool, runtimeRole string) error {
	// Wrong database / never migrated: fatal (not ErrRuntimeInsecure) — a pointer at the schema rather than a confusing SQL error on first use.
	var auditOK, historyOK bool
	if err := pool.QueryRow(ctx,
		`select to_regclass('public.audit_events') is not null, to_regclass('public.schema_migrations') is not null`,
	).Scan(&auditOK, &historyOK); err != nil {
		return fmt.Errorf("inspect runtime database: %w", err)
	}
	if !auditOK || !historyOK {
		return safeErrorf("runtime database has no audit_events/schema_migrations — is PORTCULLIS_DATABASE_URL pointing at the migrated database?")
	}

	// One query resolves every property of the session user, so all the ErrRuntimeInsecure errors below are our own crafted strings — never a wrapped query/connect error that could echo the DSN (the dev flag logs the reason text).
	var user string
	var member, unsafeAttrs, ownerReach bool
	var strayRoles []string
	if err := pool.QueryRow(ctx, `
		select session_user,
		       session_user = $1 or pg_has_role(session_user, $1, 'MEMBER'),
		       -- Dangerous cluster attributes held directly by the session user
		       -- (predicate shared with the owner-side checks — see privcheck.go).
		       (select `+unsafeRoleAttrsSQL+`
		        from pg_roles where rolname = session_user),
		       -- Owner reach over any object whose owner can DROP/ALTER the audit
		       -- table: the database, the public schema, or either protected table.
		       -- USAGE = the owner's privileges are live right now (e.g. an INHERIT
		       -- membership, or being the owner itself); SET = can become the owner;
		       -- ADMIN OPTION = can grant itself SET at will, so it can become the owner.
		       (select bool_or(pg_has_role(session_user, o.oid, 'USAGE') or pg_has_role(session_user, o.oid, 'SET') or pg_has_role(session_user, o.oid, 'MEMBER WITH ADMIN OPTION'))
		        from (
		            select datdba as oid from pg_database where datname = current_database()
		            union all
		            select nspowner from pg_namespace where nspname = 'public'
		            union all
		            select relowner from pg_class where oid = 'public.audit_events'::regclass
		            union all
		            select relowner from pg_class where oid = 'public.schema_migrations'::regclass
		        ) o),
		       -- Every OTHER role reachable via SET ROLE: an escalation path
		       -- regardless of what it currently holds. A membership WITH ADMIN OPTION
		       -- is equally an escalation path even when granted SET FALSE — the member
		       -- can grant ITSELF the SET option (GRANT r TO self WITH SET TRUE) and then
		       -- SET ROLE. A plain SET FALSE, no-admin membership is inert and stays
		       -- allowed; INHERIT-only strays leak concrete privileges, which the matrix
		       -- and owner-USAGE checks catch.
		       coalesce((
		           select array_agg(r.rolname order by r.rolname)
		           from pg_roles r
		           where (pg_has_role(session_user, r.oid, 'SET') or pg_has_role(session_user, r.oid, 'MEMBER WITH ADMIN OPTION'))
		             and r.rolname <> session_user
		             and r.rolname <> $1
		       ), '{}')`,
		runtimeRole,
	).Scan(&user, &member, &unsafeAttrs, &ownerReach, &strayRoles); err != nil {
		return fmt.Errorf("inspect runtime connection: %w", err)
	}

	// Functional floor first, and ALWAYS fatal: a server that cannot connect or write audit rows must not boot, dev flag or not.
	m, err := queryPrivilegeMatrix(ctx, pool, user)
	if err != nil {
		return fmt.Errorf("verify runtime user %q: %w", user, err)
	}
	if err := m.floorErr(fmt.Sprintf("runtime user %q", user), ""); err != nil {
		return err
	}

	// Over-privilege violations: downgradable by the explicit dev flag only. The two checks below are the ONLY shapes the flag is meant to permit — the intentional single-role dev setup, where DATABASE_URL is the schema owner (ownerReach) or a superuser (a dangerous attribute). Both hold implicitly without any membership.
	if unsafeAttrs {
		return safeErrorf("%w: runtime user %q holds a dangerous attribute (%s) (ADR-0009)", ErrRuntimeInsecure, user, unsafeRoleAttrsList)
	}
	if ownerReach {
		return safeErrorf("%w: runtime user %q owns (or can use/become the owner of) the database, public schema, or a protected table — the server must not run with owner/superuser credentials (ADR-0009)", ErrRuntimeInsecure, user)
	}
	// Check runtime membership before excess grants so a wrong principal remains fatal even when development mode permits excess privileges.
	if !member {
		return safeErrorf("runtime user %q is not a member of runtime role %q (ADR-0009) — grant it or fix PORTCULLIS_DATABASE_URL/PORTCULLIS_RUNTIME_ROLE", user, runtimeRole)
	}
	if len(strayRoles) > 0 {
		return safeErrorf("%w: runtime user %q can SET ROLE into %s beyond the runtime role — it must be a member of only %q (ADR-0009)", ErrRuntimeInsecure, user, strings.Join(strayRoles, ", "), runtimeRole)
	}
	if err := m.excessErr(fmt.Sprintf("runtime user %q", user)); err != nil {
		return safeErrorf("%w: %s", ErrRuntimeInsecure, err)
	}
	// Missing required grants remain fatal; development mode may downgrade only excess grants after identity and membership checks.
	missing, forbidden, err := verifyTablePrivileges(ctx, pool, user, fmt.Sprintf("runtime user %q", user))
	if err != nil {
		return fmt.Errorf("verify runtime user %q table privileges: %w", user, err)
	}
	if missing != nil {
		return missing
	}
	if forbidden != nil {
		return safeErrorf("%w: %s", ErrRuntimeInsecure, forbidden)
	}
	return nil
}
