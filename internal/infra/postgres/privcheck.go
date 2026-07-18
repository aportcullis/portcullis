package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// privMatrix is the effective-privilege snapshot of one role/user against the
// audit boundary (ADR-0009). has_*_privilege computes EFFECTIVE privileges
// (direct + membership + PUBLIC; implicit owner and superuser rights evaluate
// true), so drift inherited through another role is caught the same way.
type privMatrix struct {
	connect      bool // CONNECT on the current database
	schemaUsage  bool // USAGE on schema public (without it every runtime query fails)
	temporary    bool // TEMPORARY on the current database (temp relation shadows a permanent one)
	dbCreate     bool // CREATE on the current database (can make new schemas)
	schemaCreate bool // CREATE on schema public (can plant a shadowing table)
	auditRead    bool // SELECT on audit_events
	auditAppend  bool // INSERT on audit_events
	auditMutate  bool // any of UPDATE/DELETE/TRUNCATE/TRIGGER/REFERENCES/MAINTAIN on audit_events
	historyAny   bool // any privilege at all on schema_migrations
}

// canCreateRelation reports whether the principal can create ANY relation that
// could shadow a protected table by name resolution (temp, permanent-in-public,
// or a whole new schema).
func (m privMatrix) canCreateRelation() bool {
	return m.temporary || m.dbCreate || m.schemaCreate
}

// queryPrivilegeMatrix snapshots the boundary privileges of a role or login user.
// The forbidden lists include TRIGGER (with it, a non-owner could CREATE TRIGGER
// — e.g. one that blocks every audit insert), plus REFERENCES and MAINTAIN, so
// "append-only" and "owner-only" mean ALL other table privileges.
func queryPrivilegeMatrix(ctx context.Context, q rowQuerier, name string) (privMatrix, error) {
	var m privMatrix
	err := q.QueryRow(ctx, `
		select has_database_privilege($1, current_database(), 'CONNECT'),
		       has_schema_privilege($1, 'public', 'USAGE'),
		       has_database_privilege($1, current_database(), 'TEMPORARY'),
		       has_database_privilege($1, current_database(), 'CREATE'),
		       has_schema_privilege($1, 'public', 'CREATE'),
		       has_table_privilege($1, 'public.audit_events', 'SELECT'),
		       has_table_privilege($1, 'public.audit_events', 'INSERT'),
		       has_table_privilege($1, 'public.audit_events', 'UPDATE, DELETE, TRUNCATE, TRIGGER, REFERENCES, MAINTAIN'),
		       has_table_privilege($1, 'public.schema_migrations', 'SELECT, INSERT, UPDATE, DELETE, TRUNCATE, TRIGGER, REFERENCES, MAINTAIN')`, name,
	).Scan(&m.connect, &m.schemaUsage, &m.temporary, &m.dbCreate, &m.schemaCreate, &m.auditRead, &m.auditAppend, &m.auditMutate, &m.historyAny)
	return m, err
}

// floorErr reports a missing REQUIRED privilege (CONNECT / schema USAGE / audit
// SELECT+INSERT), or nil. This is the functional floor: without it the server
// cannot operate, so callers treat it as always fatal — never dev-downgradable.
// Schema USAGE is part of the floor because table privileges evaluate
// independently of it: without USAGE every runtime query fails at the schema,
// even with every table grant in place (the drift ADR-0009's rotation risks).
// subject names the checked principal; lacksHint is appended (the role check
// points at the rename/rotation procedure, the connection check does not).
func (m privMatrix) floorErr(subject, lacksHint string) error {
	if !m.connect || !m.schemaUsage || !m.auditRead || !m.auditAppend {
		return safeErrorf("%s lacks required privileges (CONNECT=%t, schema USAGE=%t, audit SELECT=%t, audit INSERT=%t)%s (ADR-0009)", subject, m.connect, m.schemaUsage, m.auditRead, m.auditAppend, lacksHint)
	}
	return nil
}

// excessErr reports a FORBIDDEN privilege (audit mutation / any migration-history
// access), or nil — the over-privilege class the dev flag may downgrade.
func (m privMatrix) excessErr(subject string) error {
	switch {
	case m.canCreateRelation():
		return safeErrorf("%s can create relations (TEMPORARY/CREATE on the database or schema public); a temporary or planted table can shadow the audit table by name resolution (ADR-0009)", subject)
	case m.auditMutate:
		return safeErrorf("%s holds UPDATE, DELETE, TRUNCATE, TRIGGER, REFERENCES, or MAINTAIN on audit_events; the append-only boundary is broken (ADR-0009)", subject)
	case m.historyAny:
		return safeErrorf("%s has access to schema_migrations; migration history must be owner-only (ADR-0009)", subject)
	}
	return nil
}

// verdict reports the first boundary violation of either kind (ADR-0009), or nil.
func (m privMatrix) verdict(subject, lacksHint string) error {
	if err := m.floorErr(subject, lacksHint); err != nil {
		return err
	}
	return m.excessErr(subject)
}

// tableVerbs is the fixed set of table privileges the per-table matrix
// inspects, in query column order.
var tableVerbs = [...]string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "TRIGGER", "REFERENCES", "MAINTAIN"}

// tablePolicy is what the runtime role must and must not hold on one table.
type tablePolicy struct {
	required  []string
	forbidden []string
}

// defaultTablePolicy applies to every table without an entry in tablePolicies:
// SELECT/INSERT/UPDATE (what 0003's default privileges grant after 0010's
// DELETE revoke) and none of DELETE or the schema-shaping verbs — every entity
// is soft-delete-only (data.md), no runtime query issues DELETE, and TRIGGER
// anywhere is schema modification. Because unknown tables get this policy, a
// future migration that creates a table without runtime grants — e.g. run by
// a principal whose default privileges don't cover it (ADR-0009) — fails the
// very first boot instead of the first RPC. Conversely, a table that needs
// looser or tighter verbs (a hard-deleting cache, a SENSITIVE table's extra
// revokes) fails boot until its tablePolicies entry lands, enforcing data.md's
// review gate in code. connections needs no entry: archive is an UPDATE and
// its no-DELETE boundary (ADR-0014) IS this default.
var defaultTablePolicy = tablePolicy{
	required:  []string{"SELECT", "INSERT", "UPDATE"},
	forbidden: []string{"DELETE", "TRUNCATE", "TRIGGER", "REFERENCES", "MAINTAIN"},
}

// tablePolicies is the single source of the per-table exceptions — the code
// form of data.md's sensitive-table judgments.
var tablePolicies = map[string]tablePolicy{
	// Append-only evidence: read and append, never mutate (ADR-0009).
	"audit_events": {
		required:  []string{"SELECT", "INSERT"},
		forbidden: []string{"UPDATE", "DELETE", "TRUNCATE", "TRIGGER", "REFERENCES", "MAINTAIN"},
	},
	// Migration history is owner-only (ADR-0009).
	"schema_migrations": {
		forbidden: []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "TRIGGER", "REFERENCES", "MAINTAIN"},
	},
}

// querier is the multi-row query surface verifyTablePrivileges needs; both
// *pgxpool.Conn (migration postflight) and *pgxpool.Pool (runtime boot check)
// satisfy it.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// verifyTablePrivileges checks the role's effective privileges on EVERY base
// table and sequence in schema public against the policy table. It returns the
// two violation classes separately because callers treat them differently:
// a missing required privilege is a functional failure (always fatal), while a
// forbidden privilege is the over-privilege class VerifyRuntimeConnection may
// downgrade behind the dev flag. Views/matviews/foreign tables are out of
// scope — the bug class this guards (default privileges binding to the
// creating role, ADR-0009) is about base relations.
func verifyTablePrivileges(ctx context.Context, q querier, role, subject string) (missing, forbidden error, _ error) {
	rows, err := q.Query(ctx, `
		select c.relname,
		       has_table_privilege($1, c.oid, 'SELECT'),
		       has_table_privilege($1, c.oid, 'INSERT'),
		       has_table_privilege($1, c.oid, 'UPDATE'),
		       has_table_privilege($1, c.oid, 'DELETE'),
		       has_table_privilege($1, c.oid, 'TRUNCATE'),
		       has_table_privilege($1, c.oid, 'TRIGGER'),
		       has_table_privilege($1, c.oid, 'REFERENCES'),
		       has_table_privilege($1, c.oid, 'MAINTAIN')
		from pg_class c
		join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = 'public' and c.relkind in ('r', 'p')
		order by c.relname`, role)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var table string
		held := make([]bool, len(tableVerbs))
		if err := rows.Scan(&table, &held[0], &held[1], &held[2], &held[3], &held[4], &held[5], &held[6], &held[7]); err != nil {
			return nil, nil, err
		}
		has := map[string]bool{}
		for i, verb := range tableVerbs {
			has[verb] = held[i]
		}
		policy, ok := tablePolicies[table]
		if !ok {
			policy = defaultTablePolicy
		}
		for _, verb := range policy.required {
			if missing == nil && !has[verb] {
				missing = safeErrorf("%s lacks required privilege %s on table public.%s — a migration likely created the table without runtime grants (default privileges bind to the creating role; ADR-0009)", subject, verb, table)
			}
		}
		for _, verb := range policy.forbidden {
			if forbidden == nil && has[verb] {
				forbidden = safeErrorf("%s holds forbidden privilege %s on table public.%s — sensitive-table boundary (docs/conventions/data.md, ADR-0009)", subject, verb, table)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	// Sequences share the bug class (0005 noted the coverage gap): the runtime
	// needs USAGE for identity/serial columns, and UPDATE (setval — a rewind is
	// a duplicate-key denial of service) is over-privilege.
	seqRows, err := q.Query(ctx, `
		select c.relname,
		       has_sequence_privilege($1, c.oid, 'USAGE'),
		       has_sequence_privilege($1, c.oid, 'UPDATE')
		from pg_class c
		join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = 'public' and c.relkind = 'S'
		order by c.relname`, role)
	if err != nil {
		return nil, nil, err
	}
	defer seqRows.Close()
	for seqRows.Next() {
		var seq string
		var usage, update bool
		if err := seqRows.Scan(&seq, &usage, &update); err != nil {
			return nil, nil, err
		}
		if missing == nil && !usage {
			missing = safeErrorf("%s lacks required privilege USAGE on sequence public.%s (ADR-0009)", subject, seq)
		}
		if forbidden == nil && update {
			forbidden = safeErrorf("%s holds forbidden privilege UPDATE on sequence public.%s — setval can rewind identity allocation (ADR-0009)", subject, seq)
		}
	}
	return missing, forbidden, seqRows.Err()
}
