package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/app/condition"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/celcondition"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

// bindingRuntimeStore qualifies reads using the restricted production database role.
func bindingRuntimeStore(t *testing.T, fixture bindingSchemaFixture, tracers ...pgx.QueryTracer) *pg.BindingStore {
	t.Helper()
	settings := fixture.pool.Config().Copy()
	if len(tracers) > 0 {
		settings.ConnConfig.Tracer = tracers[0]
	}
	settings.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		_, err := connection.Exec(ctx, `set role portcullis_runtime`)
		return err
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pg.NewBindingStore(pool)
}

// activateBindingFixture seeds only owner-controlled qualification data, never production backfill.
func activateBindingFixture(t *testing.T, fixture bindingSchemaFixture) {
	t.Helper()
	if _, err := fixture.pool.Exec(context.Background(), `insert into public.organization_authorization_revisions (organization_id,revision) values ($1,1) on conflict do nothing`, fixture.organization); err != nil {
		t.Fatal(err)
	}
}

// bindingResourceService connects the real repository and pure CEL adapter for qualification.
func bindingResourceService(t *testing.T, fixture bindingSchemaFixture) *authz.ResourceService {
	t.Helper()
	adapter, err := celcondition.New()
	if err != nil {
		t.Fatal(err)
	}
	conditions, err := condition.New(adapter, adapter, condition.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := pg.NewIdentityStore(fixture.pool).ListPermissions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	limits := authz.DefaultResourceConfig()
	limits.Timeout = 5 * time.Second
	service, err := authz.NewResourceService(bindingRuntimeStore(t, fixture), conditions, catalog, limits)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestBindingStoreResolvesScopedResourcesWithCEL(t *testing.T) {
	fixture := newBindingSchemaFixture(t)
	activateBindingFixture(t, fixture)
	expression := `resource.connection_id == "` + fixture.connection + `" && request.time < timestamp("9999-01-01T00:00:00Z")`
	binding := requireSchemaBinding(t, fixture, "connection", &fixture.connection, &expression)
	service := bindingResourceService(t, fixture)
	for _, scenario := range []struct {
		resource   identity.ResourceType
		permission identity.Permission
	}{
		{identity.ResourceConnection, identity.PermissionConnectionsGet},
		{identity.ResourceConnection, identity.PermissionConnectionsList},
		{identity.ResourcePolicy, identity.PermissionPoliciesGet},
		{identity.ResourcePolicy, identity.PermissionPoliciesUpdate},
	} {
		t.Run(string(scenario.permission), func(t *testing.T) {
			decision, err := service.AuthorizeResource(context.Background(), identity.UserID(fixture.user), scenario.permission, authz.ResourceTarget{OrganizationID: identity.OrganizationID(fixture.organization), Type: scenario.resource, ID: fixture.connection})
			if err != nil || decision.BindingID != identity.BindingID(binding) || decision.Resource.ConnectionID != fixture.connection || decision.EvaluatedAt.IsZero() || decision.Revision != 1 {
				t.Fatalf("checkpoint=%+v error=%v", decision, err)
			}
		})
	}
	for _, scenario := range []struct {
		name                   string
		organization, user, id string
		resource               identity.ResourceType
		permission             identity.Permission
	}{
		{"foreign connection", fixture.organization, fixture.user, fixture.foreignConnection, identity.ResourceConnection, identity.PermissionConnectionsGet},
		{"foreign membership", fixture.organization, fixture.foreignUser, fixture.connection, identity.ResourceConnection, identity.PermissionConnectionsGet},
		{"foreign organization", fixture.foreignOrganization, fixture.user, fixture.foreignConnection, identity.ResourceConnection, identity.PermissionConnectionsGet},
		{"connection cannot create connections", fixture.organization, fixture.user, fixture.connection, identity.ResourceConnection, identity.PermissionConnectionsCreate},
		{"connection cannot manage users", fixture.organization, fixture.user, fixture.user, identity.ResourceUser, identity.PermissionUsersUpdate},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			_, err := service.AuthorizeResource(context.Background(), identity.UserID(scenario.user), scenario.permission, authz.ResourceTarget{OrganizationID: identity.OrganizationID(scenario.organization), Type: scenario.resource, ID: scenario.id})
			if !errors.Is(err, authz.ErrPermissionDenied) {
				t.Fatalf("unsafe authorization=%v", err)
			}
		})
	}
}

func TestBindingStoreRequiresFreshLiveAuthority(t *testing.T) {
	fixture := newBindingSchemaFixture(t)
	service := bindingResourceService(t, fixture)
	target := authz.ResourceTarget{OrganizationID: identity.OrganizationID(fixture.organization), Type: identity.ResourceConnection, ID: fixture.connection}
	authorize := func() error {
		_, err := service.AuthorizeResource(context.Background(), identity.UserID(fixture.user), identity.PermissionConnectionsGet, target)
		return err
	}
	if err := authorize(); !errors.Is(err, authz.ErrAuthorizationInactive) {
		t.Fatalf("inactive store fell back to legacy admin: %v", err)
	}
	activateBindingFixture(t, fixture)
	if err := authorize(); !errors.Is(err, authz.ErrPermissionDenied) {
		t.Fatalf("empty bindings fell back to legacy admin: %v", err)
	}
	expired := `request.time < timestamp("2000-01-01T00:00:00Z")`
	requireSchemaBinding(t, fixture, "organization", nil, &expired)
	invalid := `undeclared.attribute == "yes"`
	requireSchemaBinding(t, fixture, "organization", nil, &invalid)
	if err := authorize(); !errors.Is(err, authz.ErrPermissionDenied) {
		t.Fatalf("invalid or expired condition granted: %v", err)
	}
	binding := requireSchemaBinding(t, fixture, "organization", nil, nil)
	if err := authorize(); err != nil {
		t.Fatalf("additive unconditional grant: %v", err)
	}
	if _, err := fixture.pool.Exec(context.Background(), `update public.users set status='disabled' where id=$1`, fixture.user); err != nil {
		t.Fatal(err)
	}
	if err := authorize(); !errors.Is(err, authz.ErrPermissionDenied) {
		t.Fatalf("disabled account: %v", err)
	}
	if _, err := fixture.pool.Exec(context.Background(), `update public.users set status='active' where id=$1`, fixture.user); err != nil {
		t.Fatal(err)
	}
	if err := authorize(); err != nil {
		t.Fatalf("reactivated account: %v", err)
	}
	transaction, err := fixture.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	if _, err = transaction.Exec(context.Background(), `insert into public.role_binding_versions (binding_id,organization_id,user_id,version,role_id,scope_type,created_at,revoked_at) values ($1,$2,$3,2,$4,'organization',clock_timestamp(),clock_timestamp())`, binding, fixture.organization, fixture.user, fixture.role); err != nil {
		t.Fatal(err)
	}
	if _, err = transaction.Exec(context.Background(), `update public.role_bindings set current_version=2 where organization_id=$1 and id=$2`, fixture.organization, binding); err != nil {
		t.Fatal(err)
	}
	if _, err = transaction.Exec(context.Background(), `update public.organization_authorization_revisions set revision=revision+1 where organization_id=$1`, fixture.organization); err != nil {
		t.Fatal(err)
	}
	if err = transaction.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := authorize(); !errors.Is(err, authz.ErrPermissionDenied) {
		t.Fatalf("revocation reused cached grant: %v", err)
	}
}

func TestBindingStorePreservesLegacyPermissionSetsInQualificationCopy(t *testing.T) {
	fixture := newBindingSchemaFixture(t)
	activateBindingFixture(t, fixture)
	ctx := context.Background()
	legacy := pg.NewIdentityStore(fixture.pool)
	// Exercise every default role plus an empty role and a custom live/deleted bundle.
	if _, err := fixture.pool.Exec(ctx, `insert into public.roles (organization_id,name) values ($1,'Empty qualification role'),($1,'Custom qualification role')`, fixture.organization); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `insert into public.role_permissions (role_id,permission_key,deleted_at) select id,'connections.get',null from public.roles where organization_id=$1 and name='Custom qualification role' union all select id,'requests.execute',clock_timestamp() from public.roles where organization_id=$1 and name='Custom qualification role'`, fixture.organization); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `with members as (insert into public.users (email) select 'qualification-'||id::text||'@example.com' from public.roles where organization_id=$1 returning id,email) insert into public.organization_memberships (organization_id,user_id,role_id) select $1,members.id,role.id from members join public.roles role on members.email='qualification-'||role.id::text||'@example.com' where role.organization_id=$1`, fixture.organization); err != nil {
		t.Fatal(err)
	}
	// Copy current membership assignments only inside this disposable qualification database.
	transaction, err := fixture.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	rows, err := transaction.Query(ctx, `select user_id::text,role_id::text from public.organization_memberships where organization_id=$1 order by user_id`, fixture.organization)
	if err != nil {
		t.Fatal(err)
	}
	type memberRole struct{ user, role string }
	members := make([]memberRole, 0)
	for rows.Next() {
		var member memberRole
		if err := rows.Scan(&member.user, &member.role); err != nil {
			t.Fatal(err)
		}
		members = append(members, member)
	}
	rows.Close()
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	for _, member := range members {
		candidate := fixture
		candidate.user = member.user
		candidate.role = member.role
		if _, err := insertSchemaBinding(ctx, transaction, candidate, "organization", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	catalog, err := legacy.ListPermissions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	store := bindingRuntimeStore(t, fixture)
	for _, member := range members {
		before, err := legacy.PermissionsForUser(ctx, identity.OrganizationID(fixture.organization), identity.UserID(member.user))
		if err != nil {
			t.Fatal(err)
		}
		for _, permission := range catalog {
			snapshot, err := store.LoadAuthorizationSnapshot(ctx, identity.UserID(member.user), permission, authz.ResourceTarget{OrganizationID: identity.OrganizationID(fixture.organization), Type: identity.ResourceConnection, ID: fixture.connection}, 64)
			if err != nil || !snapshot.ActiveMember || (len(snapshot.Grants) > 0) != slices.Contains(before, permission) {
				t.Fatalf("user=%s permission=%s legacy=%v grants=%v error=%v", member.user, permission, before, snapshot.Grants, err)
			}
		}
	}
}

func TestBindingStoreRejectsCandidateOverflowAndInvalidInputs(t *testing.T) {
	fixture := newBindingSchemaFixture(t)
	activateBindingFixture(t, fixture)
	requireSchemaBinding(t, fixture, "organization", nil, nil)
	requireSchemaBinding(t, fixture, "organization", nil, nil)
	store := bindingRuntimeStore(t, fixture)
	target := authz.ResourceTarget{OrganizationID: identity.OrganizationID(fixture.organization), Type: identity.ResourceConnection, ID: fixture.connection}
	if _, err := store.LoadAuthorizationSnapshot(context.Background(), identity.UserID(fixture.user), identity.PermissionConnectionsGet, target, 1); !errors.Is(err, authz.ErrBindingLimitExceeded) {
		t.Fatalf("truncated grant set: %v", err)
	}
	for _, limit := range []int{0, -1, authz.MaxResourceBindings + 1} {
		if _, err := store.LoadAuthorizationSnapshot(context.Background(), identity.UserID(fixture.user), identity.PermissionConnectionsGet, target, limit); !errors.Is(err, authz.ErrInvalidSnapshot) {
			t.Fatalf("invalid limit=%d: %v", limit, err)
		}
	}
	target.ID = "not-a-uuid"
	if _, err := store.LoadAuthorizationSnapshot(context.Background(), identity.UserID(fixture.user), identity.PermissionConnectionsGet, target, 64); !errors.Is(err, authz.ErrPermissionDenied) {
		t.Fatalf("malformed resource: %v", err)
	}
}

// seedBindingResultAncestry creates opaque envelopes without reading SQL, credentials or result values.
func seedBindingResultAncestry(t *testing.T, fixture bindingSchemaFixture) (string, string) {
	t.Helper()
	ctx := context.Background()
	var requestID, resultID string
	if err := fixture.pool.QueryRow(ctx, `insert into public.access_requests (id,organization_id,connection_id,requester_id,payload_key_version,payload_wrapped_dek,payload_nonce,payload_ciphertext) values (gen_random_uuid(),$1,$2,$3,1,'opaque','opaque','opaque') returning id`, fixture.organization, fixture.connection, fixture.user).Scan(&requestID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.pool.QueryRow(ctx, `insert into result_cache.result_sets (id,organization_id,owner_user_id,row_count,byte_size,created_at,expires_at,last_accessed_at,key_version,wrapped_dek) values (gen_random_uuid(),$1,$2,0,0,clock_timestamp(),clock_timestamp()+interval '10 minutes',clock_timestamp(),1,'opaque') returning id`, fixture.organization, fixture.user).Scan(&resultID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `insert into public.query_executions (request_id,organization_id,owner,attempt_id,heartbeat,deadline,started_at,result_id,result_expires_at) values ($1,$2,'qualification',gen_random_uuid(),clock_timestamp(),clock_timestamp()+interval '1 minute',clock_timestamp(),$3,clock_timestamp()+interval '10 minutes')`, requestID, fixture.organization, resultID); err != nil {
		t.Fatal(err)
	}
	return requestID, resultID
}

func TestBindingStoreResolvesRequestExecutionAndResultAncestry(t *testing.T) {
	fixture := newBindingSchemaFixture(t)
	activateBindingFixture(t, fixture)
	requestID, resultID := seedBindingResultAncestry(t, fixture)
	requireSchemaBinding(t, fixture, "connection", &fixture.connection, nil)
	service := bindingResourceService(t, fixture)
	for _, scenario := range []struct {
		resource   identity.ResourceType
		id         string
		permission identity.Permission
	}{
		{identity.ResourceRequest, requestID, identity.PermissionRequestsGet},
		{identity.ResourceRequest, requestID, identity.PermissionRequestsApprove},
		{identity.ResourceExecution, requestID, identity.PermissionRequestsExecute},
		{identity.ResourceResult, resultID, identity.PermissionRequestsGet},
	} {
		t.Run(string(scenario.resource)+"/"+string(scenario.permission), func(t *testing.T) {
			decision, err := service.AuthorizeResource(context.Background(), identity.UserID(fixture.user), scenario.permission, authz.ResourceTarget{OrganizationID: identity.OrganizationID(fixture.organization), Type: scenario.resource, ID: scenario.id})
			if err != nil || decision.Resource.ConnectionID != fixture.connection {
				t.Fatalf("ancestry=%+v error=%v", decision, err)
			}
		})
	}
	for _, scenario := range []struct {
		resource identity.ResourceType
		id       string
	}{
		{identity.ResourceRequest, resultID}, {identity.ResourceExecution, resultID}, {identity.ResourceResult, requestID},
	} {
		t.Run("wrong identity/"+string(scenario.resource), func(t *testing.T) {
			_, err := service.AuthorizeResource(context.Background(), identity.UserID(fixture.user), identity.PermissionRequestsGet, authz.ResourceTarget{OrganizationID: identity.OrganizationID(fixture.organization), Type: scenario.resource, ID: scenario.id})
			if !errors.Is(err, authz.ErrPermissionDenied) {
				t.Fatal(err)
			}
		})
	}
	// Duplicate result ancestry is corruption, never permission to pick a convenient connection.
	var duplicateRequest string
	if err := fixture.pool.QueryRow(context.Background(), `insert into public.access_requests (id,organization_id,connection_id,requester_id,payload_key_version,payload_wrapped_dek,payload_nonce,payload_ciphertext) values (gen_random_uuid(),$1,$2,$3,1,'opaque','opaque','opaque') returning id`, fixture.organization, fixture.connection, fixture.user).Scan(&duplicateRequest); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(context.Background(), `insert into public.query_executions (request_id,organization_id,owner,attempt_id,heartbeat,deadline,started_at,result_id,result_expires_at) values ($1,$2,'duplicate',gen_random_uuid(),clock_timestamp(),clock_timestamp()+interval '1 minute',clock_timestamp(),$3,clock_timestamp()+interval '10 minutes')`, duplicateRequest, fixture.organization, resultID); err != nil {
		t.Fatal(err)
	}
	_, err := service.AuthorizeResource(context.Background(), identity.UserID(fixture.user), identity.PermissionRequestsGet, authz.ResourceTarget{OrganizationID: identity.OrganizationID(fixture.organization), Type: identity.ResourceResult, ID: resultID})
	if !errors.Is(err, authz.ErrInvalidSnapshot) {
		t.Fatalf("ambiguous result ancestry: %v", err)
	}
}

// bindingSnapshotTraceKey identifies the first membership read in a qualification transaction.
type bindingSnapshotTraceKey struct{}

// bindingSnapshotBarrier coordinates a committed policy change after the reader establishes its snapshot.
type bindingSnapshotBarrier struct {
	ready, release chan struct{}
	once           sync.Once
}

func (barrier *bindingSnapshotBarrier) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(data.SQL, "-- name: GetBindingAuthorizationContext") {
		return context.WithValue(ctx, bindingSnapshotTraceKey{}, true)
	}
	return ctx
}

func (barrier *bindingSnapshotBarrier) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if ctx.Value(bindingSnapshotTraceKey{}) == true {
		barrier.once.Do(func() {
			close(barrier.ready)
			select {
			case <-barrier.release:
			case <-ctx.Done():
			}
		})
	}
}

func TestBindingStoreKeepsRevisionAndGrantsInOneSnapshot(t *testing.T) {
	fixture := newBindingSchemaFixture(t)
	activateBindingFixture(t, fixture)
	requireSchemaBinding(t, fixture, "organization", nil, nil)
	barrier := &bindingSnapshotBarrier{ready: make(chan struct{}), release: make(chan struct{})}
	store := bindingRuntimeStore(t, fixture, barrier)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	target := authz.ResourceTarget{OrganizationID: identity.OrganizationID(fixture.organization), Type: identity.ResourceConnection, ID: fixture.connection}
	type snapshotResult struct {
		snapshot authz.AuthorizationSnapshot
		err      error
	}
	completed := make(chan snapshotResult, 1)
	go func() {
		snapshot, err := store.LoadAuthorizationSnapshot(ctx, identity.UserID(fixture.user), identity.PermissionConnectionsGet, target, 64)
		completed <- snapshotResult{snapshot, err}
	}()
	select {
	case <-barrier.ready:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	transaction, err := fixture.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, err := transaction.Exec(ctx, `update public.role_permissions set deleted_at=clock_timestamp() where role_id=$1 and permission_key='connections.get'`, fixture.role); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `update public.organization_authorization_revisions set revision=2 where organization_id=$1`, fixture.organization); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	close(barrier.release)
	select {
	case observed := <-completed:
		if observed.err != nil || observed.snapshot.Revision != 1 || len(observed.snapshot.Grants) != 1 {
			t.Fatalf("mixed snapshot: %+v error=%v", observed.snapshot, observed.err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	snapshot, err := store.LoadAuthorizationSnapshot(ctx, identity.UserID(fixture.user), identity.PermissionConnectionsGet, target, 64)
	if err != nil || snapshot.Revision != 2 || len(snapshot.Grants) != 0 {
		t.Fatalf("stale next read: %+v error=%v", snapshot, err)
	}
}

func TestBindingStoreUnionsRolesAndObservesRoleEdits(t *testing.T) {
	fixture := newBindingSchemaFixture(t)
	activateBindingFixture(t, fixture)
	ctx := context.Background()
	var firstRole, secondRole string
	for _, descriptor := range []struct {
		name, permission string
		role             *string
	}{{"Connection reader", "connections.get", &firstRole}, {"Request executor", "requests.execute", &secondRole}} {
		if err := fixture.pool.QueryRow(ctx, `insert into public.roles (organization_id,name) values ($1,$2) returning id`, fixture.organization, descriptor.name).Scan(descriptor.role); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.pool.Exec(ctx, `insert into public.role_permissions (role_id,permission_key) values ($1,$2)`, *descriptor.role, descriptor.permission); err != nil {
			t.Fatal(err)
		}
		candidate := fixture
		candidate.role = *descriptor.role
		requireSchemaBinding(t, candidate, "connection", &fixture.connection, nil)
	}
	store := bindingRuntimeStore(t, fixture)
	target := authz.ResourceTarget{OrganizationID: identity.OrganizationID(fixture.organization), Type: identity.ResourceConnection, ID: fixture.connection}
	read := func(permission identity.Permission) authz.AuthorizationSnapshot {
		snapshot, err := store.LoadAuthorizationSnapshot(ctx, identity.UserID(fixture.user), permission, target, 64)
		if err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	if len(read(identity.PermissionConnectionsGet).Grants) != 1 || len(read(identity.PermissionRequestsExecute).Grants) != 1 {
		t.Fatal("independent roles did not contribute their own permissions")
	}
	if _, err := fixture.pool.Exec(ctx, `update public.roles set version=version+1 where organization_id=$1 and id=$2`, fixture.organization, firstRole); err != nil {
		t.Fatal(err)
	}
	if snapshot := read(identity.PermissionConnectionsGet); snapshot.Grants[0].RoleVersion != 2 {
		t.Fatalf("stale role version: %+v", snapshot)
	}
	if _, err := fixture.pool.Exec(ctx, `update public.role_permissions set deleted_at=clock_timestamp() where role_id=$1 and permission_key='connections.get'`, firstRole); err != nil {
		t.Fatal(err)
	}
	if len(read(identity.PermissionConnectionsGet).Grants) != 0 || len(read(identity.PermissionRequestsExecute).Grants) != 1 {
		t.Fatal("deleted permission or unrelated grant changed incorrectly")
	}
	if _, err := fixture.pool.Exec(ctx, `update public.roles set deleted_at=clock_timestamp() where organization_id=$1 and id=$2`, fixture.organization, secondRole); err != nil {
		t.Fatal(err)
	}
	if len(read(identity.PermissionRequestsExecute).Grants) != 0 {
		t.Fatal("deleted role still grants permission")
	}
}

func TestBindingStoreRequiresOrganizationGrantsForAdministrativeResources(t *testing.T) {
	fixture := newBindingSchemaFixture(t)
	activateBindingFixture(t, fixture)
	requireSchemaBinding(t, fixture, "connection", &fixture.connection, nil)
	service := bindingResourceService(t, fixture)
	var auditID string
	if err := fixture.pool.QueryRow(context.Background(), `select id from public.audit_events where organization_id=$1 order by id limit 1`, fixture.organization).Scan(&auditID); err != nil {
		t.Fatal(err)
	}
	scenarios := []struct {
		resource   identity.ResourceType
		id         string
		permission identity.Permission
	}{
		{identity.ResourceOrganization, fixture.organization, identity.PermissionConnectionsCreate},
		{identity.ResourceOrganization, fixture.organization, identity.PermissionConnectionsTest},
		{identity.ResourceUser, fixture.user, identity.PermissionUsersGet},
		{identity.ResourceRole, fixture.role, identity.PermissionRolesGet},
		{identity.ResourceAudit, auditID, identity.PermissionAuditGet},
	}
	for _, scenario := range scenarios {
		_, err := service.AuthorizeResource(context.Background(), identity.UserID(fixture.user), scenario.permission, authz.ResourceTarget{OrganizationID: identity.OrganizationID(fixture.organization), Type: scenario.resource, ID: scenario.id})
		if !errors.Is(err, authz.ErrPermissionDenied) {
			t.Fatalf("connection scope leaked into %s: %v", scenario.permission, err)
		}
	}
	requireSchemaBinding(t, fixture, "organization", nil, nil)
	for _, scenario := range scenarios {
		decision, err := service.AuthorizeResource(context.Background(), identity.UserID(fixture.user), scenario.permission, authz.ResourceTarget{OrganizationID: identity.OrganizationID(fixture.organization), Type: scenario.resource, ID: scenario.id})
		if err != nil || decision.Resource.ConnectionID != "" {
			t.Fatalf("organization %s: %+v error=%v", scenario.permission, decision, err)
		}
	}
}

func TestBindingResultAncestrySupportsIndexedIdentityLookup(t *testing.T) {
	fixture := newBindingSchemaFixture(t)
	_, resultID := seedBindingResultAncestry(t, fixture)
	ctx := context.Background()
	transaction, err := fixture.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, err := transaction.Exec(ctx, `set local enable_seqscan=off`); err != nil {
		t.Fatal(err)
	}
	var plan []byte
	if err := transaction.QueryRow(ctx, `explain (format json) select request_id from public.query_executions where organization_id=$1 and result_id=$2`, fixture.organization, resultID).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := json.Unmarshal(plan, &decoded); err != nil {
		t.Fatal(err)
	}
	if !hasBindingResultIndexCondition(decoded) {
		t.Fatalf("result ancestry lacks an index condition for both identities: %s", plan)
	}
}

// hasBindingResultIndexCondition accepts any plan with both lookup identities in an index condition.
func hasBindingResultIndexCondition(value any) bool {
	switch node := value.(type) {
	case map[string]any:
		if predicate, ok := node["Index Cond"].(string); ok && strings.Contains(predicate, "result_id") && strings.Contains(predicate, "organization_id") {
			return true
		}
		for _, child := range node {
			if hasBindingResultIndexCondition(child) {
				return true
			}
		}
	case []any:
		for _, child := range node {
			if hasBindingResultIndexCondition(child) {
				return true
			}
		}
	}
	return false
}
