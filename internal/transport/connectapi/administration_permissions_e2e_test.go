package connectapi_test

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

// seedRole prepares reusable definition data independently from membership assignments.
func (environment administrationAPIEnv) seedRole(t *testing.T, name string, permissions []string) string {
	t.Helper()
	ctx := context.Background()
	var role string
	if err := environment.pool.QueryRow(ctx, `insert into public.roles (organization_id,name) values ($1,$2) returning id`, string(environment.organization), name).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.pool.Exec(ctx, `insert into public.role_permissions (role_id,permission_key) select $1,unnest($2::text[])`, role, permissions); err != nil {
		t.Fatal(err)
	}
	return role
}

// replaceActorGrant appends owner-only fixture authority without altering immutable history.
func (environment administrationAPIEnv) replaceActorGrant(t *testing.T, role string, conditionSource *string) {
	t.Helper()
	ctx := context.Background()
	transaction, err := environment.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, err := transaction.Exec(ctx, `insert into public.role_binding_versions (binding_id,organization_id,user_id,version,role_id,scope_type,condition_title,condition_expression,condition_language_version,condition_environment_version,created_at)
 select id,organization_id,user_id,current_version+1,$2,'organization',case when $3::text is null then null else 'Fixture' end,$3,case when $3::text is null then null else 'cel-v1' end,case when $3::text is null then null else 'portcullis-v1' end,clock_timestamp()
 from public.role_bindings where id=$1`, environment.actorBinding, role, conditionSource); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `update public.role_bindings set current_version=current_version+1 where id=$1`, environment.actorBinding); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestAdministrationAPIPreservesConditionalReadAuthorityAndRefusesDelegation(t *testing.T) {
	environment := newAdministrationAPIEnv(t)
	ctx := context.Background()
	user := environment.createUser(t, "conditional-reader-target@example.com")
	source := `request.time < timestamp("9999-01-01T00:00:00Z")`
	environment.replaceActorGrant(t, environment.managerRole, &source)
	if _, err := environment.users.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListUsersRequest{}), environment.csrf)); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.users.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetUserRequest{Id: user}), environment.csrf)); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.roles.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListRolesRequest{}), environment.csrf)); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.bindings.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListBindingsRequest{}), environment.csrf)); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func() error{
		func() error {
			_, err := environment.users.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateUserRequest{Email: "forbidden@example.com", DisplayName: "Denied"}), environment.csrf))
			return err
		},
		func() error {
			_, err := environment.users.Disable(ctx, withCSRF(connect.NewRequest(&portcullisv1.DisableUserRequest{Id: user}), environment.csrf))
			return err
		},
		func() error {
			_, err := environment.roles.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateRoleRequest{Name: "Denied"}), environment.csrf))
			return err
		},
		func() error {
			_, err := environment.bindings.ValidateCondition(ctx, withCSRF(connect.NewRequest(&portcullisv1.ValidateBindingConditionRequest{Condition: &portcullisv1.BindingCondition{Title: "Denied", Expression: `true`, LanguageVersion: "cel-v1", EnvironmentVersion: "portcullis-v1"}}), environment.csrf))
			return err
		},
	} {
		if err := change(); connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatalf("conditional delegation: %v", err)
		}
	}
	expired := `request.time < timestamp("2000-01-01T00:00:00Z")`
	environment.replaceActorGrant(t, environment.managerRole, &expired)
	response, err := environment.users.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListUsersRequest{}), environment.csrf))
	if connect.CodeOf(err) != connect.CodePermissionDenied || response != nil {
		t.Fatalf("expired list leaked data: %+v %v", response, err)
	}
	role := environment.seedRole(t, "Only update validation", []string{"bindings.update"})
	environment.replaceActorGrant(t, role, nil)
	if _, err := environment.bindings.ValidateCondition(ctx, withCSRF(connect.NewRequest(&portcullisv1.ValidateBindingConditionRequest{Condition: &portcullisv1.BindingCondition{Title: "Update form", Expression: `false`, LanguageVersion: "cel-v1", EnvironmentVersion: "portcullis-v1"}}), environment.csrf)); err != nil {
		t.Fatalf("update-only validation: %v", err)
	}
}

func TestAdministrationAPISeparatesSummariesDetailsAndRevisionPages(t *testing.T) {
	environment := newAdministrationAPIEnv(t)
	ctx := context.Background()
	for index := range 11 {
		environment.createUser(t, fmt.Sprintf("page-%d@example.com", index))
	}
	first, err := environment.users.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListUsersRequest{Pagination: &portcullisv1.AdministrationPage{PageSize: 10}}), environment.csrf))
	if err != nil || len(first.Msg.GetUsers()) != 10 || first.Msg.GetPagination().GetTotalCount() != 12 {
		t.Fatalf("first page: %+v %v", first, err)
	}
	second, err := environment.users.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListUsersRequest{Pagination: &portcullisv1.AdministrationPage{Page: 2, PageSize: 10, ExpectedRevision: first.Msg.GetPagination().GetAuthorizationRevision()}}), environment.csrf))
	if err != nil || len(second.Msg.GetUsers()) != 2 || second.Msg.GetPagination().GetTotalCount() != 12 {
		t.Fatalf("second page: %+v %v", second, err)
	}
	identifiers := make(map[string]bool, 12)
	for _, user := range append(first.Msg.GetUsers(), second.Msg.GetUsers()...) {
		if identifiers[user.Id] {
			t.Fatalf("duplicate page row: %s", user.Id)
		}
		identifiers[user.Id] = true
	}
	encoded, err := protojson.Marshal(first.Msg)
	if err != nil || strings.Contains(string(encoded), "hasPassword") {
		t.Fatalf("account detail leaked: %s %v", encoded, err)
	}
	listedBindings, err := environment.bindings.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListBindingsRequest{}), environment.csrf))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = protojson.Marshal(listedBindings.Msg)
	if err != nil || strings.Contains(string(encoded), "expression") {
		t.Fatalf("condition detail leaked: %s %v", encoded, err)
	}
	listOnly := environment.seedRole(t, "Summary-only reader", []string{"users.list", "roles.list", "bindings.list"})
	environment.replaceActorGrant(t, listOnly, nil)
	if _, err := environment.users.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListUsersRequest{}), environment.csrf)); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.roles.ListPermissions(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListRolePermissionsRequest{}), environment.csrf)); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.users.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetUserRequest{Id: environment.actor}), environment.csrf)); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("list widened get: %v", err)
	}
	if _, err := environment.roles.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetRoleRequest{Id: environment.managerRole}), environment.csrf)); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("list widened role get: %v", err)
	}
	if _, err := environment.bindings.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetBindingRequest{Id: environment.actorBinding}), environment.csrf)); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("list widened binding get: %v", err)
	}
}

func TestAdministrationAPIRoleAndBindingRaceProtectsLastAdministrator(t *testing.T) {
	environment := newAdministrationAPIEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	firstUser := environment.createUser(t, "first-recovery@example.com")
	secondUser := environment.createUser(t, "second-recovery@example.com")
	catalog, err := pg.NewIdentityStore(environment.pool).ListPermissions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rest := make([]string, 0, len(catalog))
	for _, permission := range identity.SelectRecoveryPermissions(catalog) {
		if permission != identity.PermissionBindingsGet {
			rest = append(rest, string(permission))
		}
	}
	controller := environment.seedRole(t, "RPC recovery controller", []string{"roles.update", "bindings.delete", "bindings.get"})
	firstPiece := environment.seedRole(t, "RPC first piece", []string{"bindings.get"})
	secondPiece := environment.seedRole(t, "RPC second piece", []string{"bindings.get"})
	restRole := environment.seedRole(t, "RPC recovery rest", rest)
	environment.seedBinding(t, firstUser, firstPiece)
	environment.seedBinding(t, firstUser, restRole)
	secondBinding := environment.seedBinding(t, secondUser, secondPiece)
	environment.seedBinding(t, secondUser, restRole)
	environment.replaceActorGrant(t, controller, nil)
	start, results := make(chan struct{}), make(chan error, 2)
	go func() {
		<-start
		_, err := environment.roles.Update(ctx, withCSRF(connect.NewRequest(&portcullisv1.UpdateRoleRequest{Id: firstPiece, ExpectedVersion: 1, Name: "Removed first piece"}), environment.csrf))
		results <- err
	}()
	go func() {
		<-start
		_, err := environment.bindings.Revoke(ctx, withCSRF(connect.NewRequest(&portcullisv1.RevokeBindingRequest{Id: secondBinding, ExpectedVersion: 1}), environment.csrf))
		results <- err
	}()
	close(start)
	var succeeded, protected int
	for range 2 {
		select {
		case err := <-results:
			if err == nil {
				succeeded++
			} else if connect.CodeOf(err) == connect.CodeFailedPrecondition {
				protected++
			} else {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if succeeded != 1 || protected != 1 {
		t.Fatalf("RPC recovery race: succeeded=%d protected=%d", succeeded, protected)
	}
}

func TestAdministrationAPIActivationPreservesGrantsAndOpensManagement(t *testing.T) {
	environment := newAdministrationAPIEnv(t)
	ctx := context.Background()
	store := pg.NewIdentityStore(environment.pool)
	before, err := store.PermissionsForUser(ctx, environment.organization, identity.UserID(environment.actor))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := environment.rawUsers.List(ctx, connect.NewRequest(&portcullisv1.ListUsersRequest{})); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("prepared anonymous: %v", err)
	}
	if _, err := environment.users.List(ctx, connect.NewRequest(&portcullisv1.ListUsersRequest{})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("prepared CSRF: %v", err)
	}
	if _, err := environment.users.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListUsersRequest{}), environment.csrf)); err != nil {
		t.Fatalf("activated read: %v", err)
	}
	if _, err := environment.roles.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetRoleRequest{Id: environment.managerRole}), environment.csrf)); err != nil {
		t.Fatalf("activated role detail: %v", err)
	}
	if _, err := environment.roles.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateRoleRequest{Name: "Activated role"}), environment.csrf)); err != nil {
		t.Fatalf("activated write: %v", err)
	}
	after, err := store.PermissionsForUser(ctx, environment.organization, identity.UserID(environment.actor))
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("legacy authority changed: %v %v %v", before, after, err)
	}
	var created int
	if err := environment.pool.QueryRow(ctx, `select count(*) from public.roles where name='Activated role'`).Scan(&created); err != nil || created != 1 {
		t.Fatalf("activated write: %d %v", created, err)
	}
}

// seedConnection creates metadata ancestry without opening a target database.
func (environment administrationAPIEnv) seedConnection(t *testing.T) string {
	t.Helper()
	var connection string
	if err := environment.pool.QueryRow(context.Background(), `with descriptor as (
 insert into public.connections (id,organization_id,db_type,display_name,host,port,database_name,tls_mode,target_fingerprint,created_by,current_policy_version,archived_at)
 values (gen_random_uuid(),$1,'postgresql','API scope','localhost',5432,'test','disable','fixture',$2,1,clock_timestamp()) returning id,organization_id,created_by)
 insert into public.connection_policy_versions (connection_id,organization_id,version,read_allowed,write_allowed,ddl_allowed,read_required_approvals,write_required_approvals,ddl_required_approvals,query_timeout_seconds,max_rows,max_result_bytes,created_by)
 select id,organization_id,1,true,false,false,1,1,1,30,10000,4096,created_by from descriptor returning connection_id`, string(environment.organization), environment.actor).Scan(&connection); err != nil {
		t.Fatal(err)
	}
	return connection
}

func TestAdministrationAPIConnectionScopesDoNotBecomeOrganizationAuthority(t *testing.T) {
	environment := newAdministrationAPIEnv(t)
	ctx := context.Background()
	connection := environment.seedConnection(t)
	user := environment.createUser(t, "connection-binding-user@example.com")
	role := environment.seedRole(t, "Connection reader", []string{"connections.get"})
	created, err := environment.bindings.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateBindingRequest{UserId: user, RoleId: role, Scope: &portcullisv1.BindingScope{Type: "connection", ConnectionId: connection}}), environment.csrf))
	if err != nil || created.Msg.GetBinding().GetVersion() != 1 {
		t.Fatalf("connection grant: %+v %v", created, err)
	}
	createdDetail, err := environment.bindings.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetBindingRequest{Id: created.Msg.GetBinding().GetId()}), environment.csrf))
	if err != nil || createdDetail.Msg.GetBinding().GetSummary().GetScope().GetConnectionId() != connection {
		t.Fatalf("stored connection scope: %+v %v", createdDetail, err)
	}
	binding := created.Msg.GetBinding().GetId()
	updated, err := environment.bindings.Update(ctx, withCSRF(connect.NewRequest(&portcullisv1.UpdateBindingRequest{Id: binding, ExpectedVersion: 1, Scope: &portcullisv1.BindingScope{Type: "organization"}}), environment.csrf))
	if err != nil || updated.Msg.GetBinding().GetVersion() != 2 {
		t.Fatalf("explicit scope update: %+v %v", updated, err)
	}
	updatedDetail, err := environment.bindings.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetBindingRequest{Id: binding}), environment.csrf))
	if err != nil || updatedDetail.Msg.GetBinding().GetSummary().GetScope().GetType() != "organization" {
		t.Fatalf("stored changed scope: %+v %v", updatedDetail, err)
	}
	if _, err := environment.bindings.Revoke(ctx, withCSRF(connect.NewRequest(&portcullisv1.RevokeBindingRequest{Id: binding, ExpectedVersion: 2}), environment.csrf)); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.bindings.Revoke(ctx, withCSRF(connect.NewRequest(&portcullisv1.RevokeBindingRequest{Id: binding, ExpectedVersion: 3}), environment.csrf)); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("terminal revocation: %v", err)
	}
	for _, scope := range []*portcullisv1.BindingScope{{Type: "connection"}, {Type: "organization", ConnectionId: connection}, {Type: "connection", ConnectionId: "00000000-0000-0000-0000-000000000001"}, {Type: "unknown"}} {
		if _, err := environment.bindings.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateBindingRequest{UserId: user, RoleId: role, Scope: scope}), environment.csrf)); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatalf("invalid/foreign scope: %v", err)
		}
	}
	transaction, err := environment.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, err := transaction.Exec(ctx, `insert into public.role_binding_versions (binding_id,organization_id,user_id,version,role_id,scope_type,connection_id,created_at) select id,organization_id,user_id,current_version+1,$2,'connection',$3,clock_timestamp() from public.role_bindings where id=$1`, environment.actorBinding, environment.managerRole, connection); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `update public.role_bindings set current_version=current_version+1 where id=$1`, environment.actorBinding); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.roles.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListRolesRequest{}), environment.csrf)); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("connection role widened list: %v", err)
	}
	if _, err := environment.roles.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetRoleRequest{Id: role}), environment.csrf)); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("connection role widened detail: %v", err)
	}
	if _, err := environment.users.Disable(ctx, withCSRF(connect.NewRequest(&portcullisv1.DisableUserRequest{Id: user}), environment.csrf)); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("connection role widened mutation: %v", err)
	}
}
