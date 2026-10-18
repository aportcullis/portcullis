package connectapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"connectrpc.com/connect"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

func TestAdministrationAPIIgnoresForgedActorAndOrganizationClaims(t *testing.T) {
	environment := newAdministrationAPIEnv(t)
	ctx := context.Background()
	reserve := environment.createUser(t, "forged-reserve@example.com")
	environment.seedBinding(t, reserve, environment.managerRole)
	controller := environment.seedRole(t, "Only empty role creation", []string{"roles.create"})
	environment.replaceActorGrant(t, controller, nil)
	var foreignOrganization string
	if err := environment.pool.QueryRow(ctx, `insert into public.organizations (name,slug) values ('Foreign API','foreign-api') returning id`).Scan(&foreignOrganization); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name        string
		permissions []string
		status      int
	}{
		{"Forged actor cannot escalate", []string{"connections.get"}, http.StatusForbidden},
		{"Forged organization cannot redirect", nil, http.StatusOK},
	} {
		payload, err := json.Marshal(map[string]any{"name": scenario.name, "permissions": scenario.permissions, "organizationId": foreignOrganization, "actorUserId": reserve})
		if err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, environment.serverURL+"/portcullis.v1.Roles/Create", strings.NewReader(string(payload)))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Connect-Protocol-Version", "1")
		request.Header.Set("X-CSRF-Token", environment.csrf)
		response, err := environment.httpClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil || response.StatusCode != scenario.status || response.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("forged claims: status=%d body=%s %v", response.StatusCode, body, err)
		}
	}
	var createdOrganization, auditedActor string
	if err := environment.pool.QueryRow(ctx, `select role.organization_id::text,event.actor_user_id::text from public.roles role join public.audit_events event on event.target_id=role.id::text and event.action='ROLE_CREATED' where role.name='Forged organization cannot redirect'`).Scan(&createdOrganization, &auditedActor); err != nil || createdOrganization != string(environment.organization) || auditedActor != environment.actor {
		t.Fatalf("client claimed authority: org=%s actor=%s %v", createdOrganization, auditedActor, err)
	}
}

func TestAdministrationAPIPreservesUnknownConditionsAndRejectsAmbiguousEdits(t *testing.T) {
	environment := newAdministrationAPIEnv(t)
	ctx := context.Background()
	target := environment.createUser(t, "unknown-source-target@example.com")
	role := environment.seedRole(t, "Unknown source role", []string{"connections.get"})
	binding := environment.seedBinding(t, target, role)
	if _, err := environment.bindings.Update(ctx, withCSRF(connect.NewRequest(&portcullisv1.UpdateBindingRequest{Id: binding, ExpectedVersion: 1, ConditionEdit: &portcullisv1.UpdateBindingRequest_ClearCondition{ClearCondition: false}}), environment.csrf)); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("false explicit removal: %v", err)
	}
	emptyRole := ""
	if _, err := environment.bindings.Update(ctx, withCSRF(connect.NewRequest(&portcullisv1.UpdateBindingRequest{Id: binding, ExpectedVersion: 1, RoleId: &emptyRole}), environment.csrf)); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("empty present role: %v", err)
	}
	if _, err := environment.bindings.Update(ctx, withCSRF(connect.NewRequest(&portcullisv1.UpdateBindingRequest{Id: binding, ExpectedVersion: 1, ConditionEdit: &portcullisv1.UpdateBindingRequest_ReplaceCondition{ReplaceCondition: &portcullisv1.BindingCondition{Title: "Explicit false", Expression: `false`, LanguageVersion: "cel-v1", EnvironmentVersion: "portcullis-v1"}}}), environment.csrf)); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.bindings.Update(ctx, withCSRF(connect.NewRequest(&portcullisv1.UpdateBindingRequest{Id: binding, ExpectedVersion: 1}), environment.csrf)); connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("stale binding: %v", err)
	}
	transaction, err := environment.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, err := transaction.Exec(ctx, `insert into public.role_binding_versions (binding_id,organization_id,user_id,version,role_id,scope_type,condition_title,condition_expression,condition_language_version,condition_environment_version,created_at)
 select binding_id,organization_id,user_id,3,role_id,scope_type,'Future contract','future.source.secret','cel-v2','portcullis-v2',clock_timestamp() from public.role_binding_versions where binding_id=$1 and version=2`, binding); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `update public.role_bindings set current_version=3 where id=$1`, binding); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	detail, err := environment.bindings.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetBindingRequest{Id: binding}), environment.csrf))
	if err != nil || detail.Msg.GetBinding().GetCondition().GetExpression() != "future.source.secret" || detail.Msg.GetBinding().GetCondition().GetLanguageVersion() != "cel-v2" {
		t.Fatalf("future source lost: %+v %v", detail, err)
	}
	for _, edit := range []*portcullisv1.UpdateBindingRequest{
		{Id: binding, ExpectedVersion: 3},
		{Id: binding, ExpectedVersion: 3, ConditionEdit: &portcullisv1.UpdateBindingRequest_ClearCondition{ClearCondition: true}},
	} {
		if _, err := environment.bindings.Update(ctx, withCSRF(connect.NewRequest(edit), environment.csrf)); connect.CodeOf(err) != connect.CodeInvalidArgument || strings.Contains(err.Error(), "future.source.secret") {
			t.Fatalf("future contract overwritten or leaked: %v", err)
		}
	}
}

func TestAdministrationAPISharedRoleMutationRevokesTheCallingSession(t *testing.T) {
	environment := newAdministrationAPIEnv(t)
	ctx := context.Background()
	catalog, err := environment.roles.ListPermissions(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListRolePermissionsRequest{}), environment.csrf))
	if err != nil {
		t.Fatal(err)
	}
	permissions := make([]string, 0, len(catalog.Msg.GetPermissions()))
	for _, permission := range catalog.Msg.GetPermissions() {
		if permission != "connections.get" {
			permissions = append(permissions, permission)
		}
	}
	if _, err := environment.roles.Update(ctx, withCSRF(connect.NewRequest(&portcullisv1.UpdateRoleRequest{Id: environment.managerRole, ExpectedVersion: 1, Name: "Actor shared role", Permissions: permissions}), environment.csrf)); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.users.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListUsersRequest{}), environment.csrf)); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("revoked session reused: %v", err)
	}
}

func TestAdministrationAPIRollsBackSuccessEvidenceFailureWithoutLeakingDiagnostics(t *testing.T) {
	environment := newAdministrationAPIEnv(t)
	ctx := context.Background()
	var before int64
	if err := environment.pool.QueryRow(ctx, `select revision from public.organization_authorization_revisions where organization_id=$1`, string(environment.organization)).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.pool.Exec(ctx, `create function public.refuse_api_audit() returns trigger language plpgsql as $$begin raise exception 'private-api-storage-marker'; end$$; create trigger refuse_api_audit before insert on public.audit_events for each row when (new.outcome='SUCCEEDED' and new.action='ROLE_CREATED') execute function public.refuse_api_audit()`); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.roles.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateRoleRequest{Name: "Must roll back"}), environment.csrf)); connect.CodeOf(err) != connect.CodeUnavailable || strings.Contains(err.Error(), "private-api-storage-marker") {
		t.Fatalf("fault disclosure: %v", err)
	}
	var after, created int64
	if err := environment.pool.QueryRow(ctx, `select (select revision from public.organization_authorization_revisions where organization_id=$1),(select count(*) from public.roles where name='Must roll back')`, string(environment.organization)).Scan(&after, &created); err != nil || before != after || created != 0 {
		t.Fatalf("partial API commit: before=%d after=%d created=%d %v", before, after, created, err)
	}
}

func TestAdministrationAPIForeignResourcesAndCanonicalAliasesAreNotAuthority(t *testing.T) {
	environment := newAdministrationAPIEnv(t)
	ctx := context.Background()
	localUser := environment.createUser(t, "local-reference@example.com")
	var foreignOrganization, foreignUser, foreignRole string
	if err := environment.pool.QueryRow(ctx, `insert into public.organizations (name,slug) values ('Foreign references','foreign-references') returning id`).Scan(&foreignOrganization); err != nil {
		t.Fatal(err)
	}
	if err := environment.pool.QueryRow(ctx, `insert into public.users (email) values ('foreign-reference@example.com') returning id`).Scan(&foreignUser); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.pool.Exec(ctx, `insert into public.organization_memberships (organization_id,user_id) values ($1,$2)`, foreignOrganization, foreignUser); err != nil {
		t.Fatal(err)
	}
	if err := environment.pool.QueryRow(ctx, `insert into public.roles (organization_id,name) values ($1,'Foreign reference role') returning id`, foreignOrganization).Scan(&foreignRole); err != nil {
		t.Fatal(err)
	}
	foreign := environment
	foreign.organization = identity.OrganizationID(foreignOrganization)
	foreignBinding := foreign.seedBinding(t, foreignUser, foreignRole)
	if _, err := environment.users.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetUserRequest{Id: foreignUser}), environment.csrf)); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("foreign member: %v", err)
	}
	if _, err := environment.roles.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetRoleRequest{Id: foreignRole}), environment.csrf)); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("foreign role: %v", err)
	}
	if _, err := environment.bindings.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetBindingRequest{Id: foreignBinding}), environment.csrf)); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("foreign binding: %v", err)
	}
	for _, reference := range []struct{ user, role string }{{foreignUser, environment.managerRole}, {localUser, foreignRole}} {
		if _, err := environment.bindings.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateBindingRequest{UserId: reference.user, RoleId: reference.role, Scope: &portcullisv1.BindingScope{Type: "organization"}}), environment.csrf)); connect.CodeOf(err) != connect.CodeNotFound {
			t.Fatalf("foreign mutation reference: %v", err)
		}
	}
	for _, alias := range []string{"00000000-0000-0000-0000-00000000000A", strings.ReplaceAll(environment.actor, "-", "")} {
		if _, err := environment.users.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetUserRequest{Id: alias}), environment.csrf)); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatalf("canonical alias: %v", err)
		}
	}
}
