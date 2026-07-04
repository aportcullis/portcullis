package postgres

import "context"

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
