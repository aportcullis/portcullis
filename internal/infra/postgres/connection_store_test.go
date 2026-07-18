package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

// connFixture creates the org-scoped prerequisites (a creator user) and
// returns a builder for valid domain connections with unique display names.
type connFixture struct {
	pool  *pgxpool.Pool
	store *pg.ConnectionStore
	org   identity.OrganizationID
	user  identity.UserID
}

func newConnFixture(t *testing.T) connFixture {
	t.Helper()
	pool := dbtest.Postgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ids := pg.NewIdentityStore(pool)
	org, err := ids.DefaultOrganizationID(ctx)
	if err != nil {
		t.Fatalf("org: %v", err)
	}
	u, err := ids.CreateUser(ctx, unique("conn-admin")+"@example.com", "Conn Admin")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return connFixture{pool: pool, store: pg.NewConnectionStore(pool), org: org, user: u.ID}
}

func (f connFixture) newConn(t *testing.T, name string) connection.Connection {
	t.Helper()
	target, err := connection.NewTarget("db.example.com", 5432, "appdb")
	if err != nil {
		t.Fatal(err)
	}
	c, err := connection.New(
		connection.ConnectionID(uuid.NewString()), f.org, connection.DBTypePostgreSQL,
		name, target, connection.TLSModeVerifyFull, f.user,
		time.Now().UTC().Truncate(time.Microsecond), // timestamptz stores microseconds
	)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func sealedStub(version uint32) connection.SealedCredential {
	return connection.SealedCredential{
		KeyVersion: version,
		WrappedDEK: []byte("wrapped-dek"),
		Nonce:      []byte("nonce-123456"),
		Ciphertext: []byte("ciphertext"),
	}
}

func connEvent(action audit.Action, target connection.ConnectionID) audit.Event {
	return audit.Event{
		ActorType: audit.ActorUser, Action: action,
		TargetType: audit.TargetTypeConnection, TargetID: string(target),
		Outcome: audit.OutcomeSucceeded,
	}
}

func TestConnectionStoreRoundTrip(t *testing.T) {
	f := newConnFixture(t)
	ctx := context.Background()

	c := f.newConn(t, unique("Prod"))
	sealed := sealedStub(1)
	if err := f.store.Create(ctx, c, sealed, connEvent(audit.ActionConnectionCreated, c.ID)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := f.store.GetByID(ctx, f.org, c.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.DisplayName != c.DisplayName || got.Target != c.Target || got.TLSMode != c.TLSMode ||
		got.Fingerprint != c.Fingerprint || got.CreatedBy != f.user || got.DBType != connection.DBTypePostgreSQL {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got, c)
	}
	if got.IsArchived() {
		t.Error("new connection must be active")
	}

	gotConn, gotSealed, err := f.store.TestMaterial(ctx, f.org, c.ID)
	if err != nil {
		t.Fatalf("TestMaterial: %v", err)
	}
	if gotConn.ID != c.ID || gotConn.Target != c.Target {
		t.Errorf("TestMaterial descriptor = %+v, want the stored target %+v", gotConn.Target, c.Target)
	}
	if gotSealed.KeyVersion != sealed.KeyVersion || string(gotSealed.Ciphertext) != string(sealed.Ciphertext) ||
		string(gotSealed.WrappedDEK) != string(sealed.WrappedDEK) || string(gotSealed.Nonce) != string(sealed.Nonce) {
		t.Errorf("sealed round trip = %+v, want %+v", gotSealed, sealed)
	}

	if _, err := f.store.GetByID(ctx, f.org, connection.ConnectionID(uuid.NewString())); !errors.Is(err, connection.ErrNotFound) {
		t.Errorf("GetByID(missing) = %v, want ErrNotFound", err)
	}
}

func TestConnectionStoreNameUniquePerOrgUntilArchived(t *testing.T) {
	f := newConnFixture(t)
	ctx := context.Background()
	name := unique("Shared name")

	first := f.newConn(t, name)
	if err := f.store.Create(ctx, first, sealedStub(1), connEvent(audit.ActionConnectionCreated, first.ID)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Same name, case-folded, while the first is active → ErrNameTaken.
	dup := f.newConn(t, name)
	if err := f.store.Create(ctx, dup, sealedStub(1), connEvent(audit.ActionConnectionCreated, dup.ID)); !errors.Is(err, connection.ErrNameTaken) {
		t.Fatalf("duplicate name err = %v, want ErrNameTaken", err)
	}
	// Archiving frees the name (partial unique on archived_at is null).
	if _, err := f.store.Archive(ctx, f.org, first.ID, connEvent(audit.ActionConnectionArchived, first.ID)); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if err := f.store.Create(ctx, dup, sealedStub(1), connEvent(audit.ActionConnectionCreated, dup.ID)); err != nil {
		t.Fatalf("name should be reusable after archive: %v", err)
	}
}

func TestConnectionStoreArchive(t *testing.T) {
	f := newConnFixture(t)
	ctx := context.Background()

	c := f.newConn(t, unique("Archive me"))
	if err := f.store.Create(ctx, c, sealedStub(1), connEvent(audit.ActionConnectionCreated, c.ID)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	archived, err := f.store.Archive(ctx, f.org, c.ID, connEvent(audit.ActionConnectionArchived, c.ID))
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if !archived.IsArchived() {
		t.Error("Archive should return the archived row")
	}
	// Descriptor snapshot survives; credential is discarded (PRD §4.3).
	if archived.Fingerprint != c.Fingerprint || archived.DisplayName != c.DisplayName {
		t.Error("archive must keep the descriptor snapshot")
	}
	var metadata string
	if err := f.pool.QueryRow(ctx, `
		select metadata::text from public.audit_events
		where target_id = $1 and action = 'CONNECTION_ARCHIVED'
		order by occurred_at desc, id desc limit 1`, string(c.ID)).Scan(&metadata); err != nil {
		t.Fatalf("read archive audit metadata: %v", err)
	}
	for _, want := range []string{c.DisplayName, string(c.DBType), c.Fingerprint} {
		if !strings.Contains(metadata, want) {
			t.Errorf("archive audit metadata %s is missing snapshot value %q", metadata, want)
		}
	}
	if _, _, err := f.store.TestMaterial(ctx, f.org, c.ID); !errors.Is(err, connection.ErrArchived) {
		t.Errorf("TestMaterial after archive = %v, want ErrArchived", err)
	}
	if _, err := f.store.Archive(ctx, f.org, c.ID, connEvent(audit.ActionConnectionArchived, c.ID)); !errors.Is(err, connection.ErrAlreadyArchived) {
		t.Errorf("second Archive = %v, want ErrAlreadyArchived", err)
	}
	if _, err := f.store.ReplaceConfig(ctx, archived, archived.Version, sealedStub(1)); !errors.Is(err, connection.ErrArchived) {
		t.Errorf("ReplaceConfig on archived = %v, want ErrArchived", err)
	}
	// Archived rows remain listable only when asked for.
	active, err := f.store.List(ctx, f.org, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range active {
		if a.ID == c.ID {
			t.Error("archived connection must not appear in the active list")
		}
	}
	all, err := f.store.List(ctx, f.org, true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range all {
		if a.ID == c.ID {
			found = true
		}
	}
	if !found {
		t.Error("archived connection must appear when includeArchived")
	}
}

// The audit events ride the mutation's transaction: a failing event write must
// roll the whole mutation back (ADR-0009), and a successful mutation must land
// its events.
func TestConnectionStoreAuditSameTransaction(t *testing.T) {
	f := newConnFixture(t)
	pool := dbtest.Postgres(t)
	ctx := context.Background()

	// Success: create with two events (the relaxed-TLS shape) lands both.
	c := f.newConn(t, unique("Audited"))
	events := []audit.Event{
		connEvent(audit.ActionConnectionCreated, c.ID),
		connEvent(audit.ActionConnectionTLSRelaxed, c.ID),
	}
	if err := f.store.Create(ctx, c, sealedStub(1), events...); err != nil {
		t.Fatalf("Create: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx,
		`select count(*) from audit_events where target_type = 'connection' and target_id = $1`,
		string(c.ID)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("audit events for %s = %d, want 2 (same tx as the insert)", c.ID, n)
	}

	// Failure injection: an event whose organization id is not a UUID makes the
	// audit insert fail inside the tx — the archive must roll back with it.
	bad := connEvent(audit.ActionConnectionArchived, c.ID)
	bad.OrganizationID = "not-a-uuid"
	if _, err := f.store.Archive(ctx, f.org, c.ID, bad); err == nil {
		t.Fatal("archive with a failing audit write should error")
	}
	got, err := f.store.GetByID(ctx, f.org, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.IsArchived() {
		t.Fatal("failed audit write must roll back the archive")
	}
	if _, _, err := f.store.TestMaterial(ctx, f.org, c.ID); err != nil {
		t.Errorf("credential must survive the rolled-back archive: %v", err)
	}
}

// Rows are invisible outside their organization (ADR-0004 mandatory test).
func TestConnectionStoreCrossOrgIsolation(t *testing.T) {
	f := newConnFixture(t)
	pool := dbtest.Postgres(t)
	ctx := context.Background()

	c := f.newConn(t, unique("Org scoped"))
	if err := f.store.Create(ctx, c, sealedStub(1), connEvent(audit.ActionConnectionCreated, c.ID)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var orgB string
	if err := pool.QueryRow(ctx,
		`insert into organizations (slug, name) values ($1, 'Org B') returning id::text`,
		unique("org-b")).Scan(&orgB); err != nil {
		t.Fatalf("create second org: %v", err)
	}
	other := identity.OrganizationID(orgB)

	if _, err := f.store.GetByID(ctx, other, c.ID); !errors.Is(err, connection.ErrNotFound) {
		t.Errorf("cross-org GetByID = %v, want ErrNotFound", err)
	}
	if _, _, err := f.store.TestMaterial(ctx, other, c.ID); !errors.Is(err, connection.ErrNotFound) {
		t.Errorf("cross-org TestMaterial = %v, want ErrNotFound", err)
	}
	if _, err := f.store.Archive(ctx, other, c.ID, connEvent(audit.ActionConnectionArchived, c.ID)); !errors.Is(err, connection.ErrNotFound) {
		t.Errorf("cross-org Archive = %v, want ErrNotFound", err)
	}
	list, err := f.store.List(ctx, other, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range list {
		if got.ID == c.ID {
			t.Error("cross-org List must not leak the connection")
		}
	}
}

// The table's credential invariants hold at the SQL level, not just in Go.
func TestConnectionsTableCredentialConstraints(t *testing.T) {
	newConnFixture(t) // ensures migrations ran
	pool := dbtest.Postgres(t)
	ctx := context.Background()

	var orgID, userID string
	if err := pool.QueryRow(ctx, `select id::text from organizations where slug = 'default'`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select id::text from users limit 1`).Scan(&userID); err != nil {
		t.Fatal(err)
	}

	// A partial envelope (key version without ciphertext) is unrepresentable.
	_, err := pool.Exec(ctx, `insert into connections
		(id, organization_id, db_type, display_name, host, port, database_name, tls_mode, target_fingerprint,
		 credential_key_version, created_by)
		values (gen_random_uuid(), $1, 'postgresql', $2, 'h', 5432, 'd', 'verify-full', 'fp', 1, $3)`,
		orgID, unique("partial"), userID)
	if err == nil {
		t.Error("partial credential envelope should violate connections_credential_all_or_none")
	}

	// An active row without a credential is unrepresentable.
	_, err = pool.Exec(ctx, `insert into connections
		(id, organization_id, db_type, display_name, host, port, database_name, tls_mode, target_fingerprint, created_by)
		values (gen_random_uuid(), $1, 'postgresql', $2, 'h', 5432, 'd', 'verify-full', 'fp', $3)`,
		orgID, unique("bare"), userID)
	if err == nil {
		t.Error("active row without credential should violate connections_active_has_credential")
	}

	// An archived row holding a credential is unrepresentable.
	_, err = pool.Exec(ctx, `insert into connections
		(id, organization_id, db_type, display_name, host, port, database_name, tls_mode, target_fingerprint,
		 credential_key_version, credential_wrapped_dek, credential_nonce, credential_ciphertext, created_by, archived_at)
		values (gen_random_uuid(), $1, 'postgresql', $2, 'h', 5432, 'd', 'verify-full', 'fp',
		 1, 'x'::bytea, 'y'::bytea, 'z'::bytea, $3, now())`,
		orgID, unique("zombie"), userID)
	if err == nil {
		t.Error("archived row with credential should violate connections_archived_has_no_credential")
	}
}

func TestConnectionStoreRenameAndReplace(t *testing.T) {
	f := newConnFixture(t)
	ctx := context.Background()

	c := f.newConn(t, unique("Original"))
	if err := f.store.Create(ctx, c, sealedStub(1), connEvent(audit.ActionConnectionCreated, c.ID)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	newName := unique("Renamed")
	renamed, err := f.store.Rename(ctx, f.org, c.ID, newName, connEvent(audit.ActionConnectionUpdated, c.ID))
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if renamed.DisplayName != newName {
		t.Errorf("DisplayName = %q, want %q", renamed.DisplayName, newName)
	}
	if renamed.Version != c.Version+1 {
		t.Errorf("rename version = %d, want %d", renamed.Version, c.Version+1)
	}

	target, err := connection.NewTarget("replica.example.com", 5433, "otherdb")
	if err != nil {
		t.Fatal(err)
	}
	updated := renamed
	updated.Target = target
	updated.TLSMode = connection.TLSModeVerifyCA
	updated.Fingerprint = target.Fingerprint(updated.DBType)
	replaced, err := f.store.ReplaceConfig(ctx, updated, renamed.Version, sealedStub(2), connEvent(audit.ActionConnectionUpdated, c.ID))
	if err != nil {
		t.Fatalf("ReplaceConfig: %v", err)
	}
	if replaced.Target != target || replaced.TLSMode != connection.TLSModeVerifyCA {
		t.Errorf("ReplaceConfig round trip = %+v", replaced)
	}
	if replaced.Version != renamed.Version+1 {
		t.Errorf("config replacement version = %d, want %d", replaced.Version, renamed.Version+1)
	}
	// TestMaterial reads descriptor + credential from ONE row, so after a config
	// replace it returns the NEW target AND the NEW credential together — never
	// the old target paired with the new credential (the TOCTOU the split reads
	// allowed, ADR-0014).
	testConn, sealed, err := f.store.TestMaterial(ctx, f.org, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sealed.KeyVersion != 2 {
		t.Errorf("credential key version = %d, want the replaced envelope (2)", sealed.KeyVersion)
	}
	if testConn.Target != target {
		t.Errorf("TestMaterial target = %+v, want the replaced target %+v (descriptor and credential must be one snapshot)", testConn.Target, target)
	}

	if _, err := f.store.Rename(ctx, f.org, connection.ConnectionID(uuid.NewString()), "x", connEvent(audit.ActionConnectionUpdated, "missing")); !errors.Is(err, connection.ErrNotFound) {
		t.Errorf("Rename(missing) = %v, want ErrNotFound", err)
	}
}

func TestConnectionStoreReplaceRejectsStaleDescriptor(t *testing.T) {
	f := newConnFixture(t)
	ctx := context.Background()

	c := f.newConn(t, unique("Original"))
	if err := f.store.Create(ctx, c, sealedStub(1), connEvent(audit.ActionConnectionCreated, c.ID)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := f.store.Rename(ctx, f.org, c.ID, unique("Renamed"), connEvent(audit.ActionConnectionUpdated, c.ID)); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	target, err := connection.NewTarget("replica.example.com", 5433, "otherdb")
	if err != nil {
		t.Fatal(err)
	}
	stale := c
	stale.Target = target
	stale.TLSMode = connection.TLSModeVerifyCA
	stale.Fingerprint = target.Fingerprint(stale.DBType)

	if _, err := f.store.ReplaceConfig(ctx, stale, c.Version, sealedStub(2), connEvent(audit.ActionConnectionUpdated, c.ID)); !errors.Is(err, connection.ErrConflict) {
		t.Fatalf("ReplaceConfig with stale descriptor = %v, want ErrConflict", err)
	}
	got, err := f.store.GetByID(ctx, f.org, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName == c.DisplayName {
		t.Error("stale config replacement overwrote the concurrent rename")
	}
}

// The version column's positivity is enforced by the schema, not only the
// application (ADR-0014, migration 0009).
func TestConnectionVersionPositiveConstraint(t *testing.T) {
	f := newConnFixture(t)
	ctx := context.Background()

	c := f.newConn(t, unique("Versioned"))
	if err := f.store.Create(ctx, c, sealedStub(1), connEvent(audit.ActionConnectionCreated, c.ID)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err := f.pool.Exec(ctx, `update public.connections set version = 0 where id = $1`, string(c.ID))
	if err == nil {
		t.Error("update to version = 0 should violate connections_version_positive")
	}
}
