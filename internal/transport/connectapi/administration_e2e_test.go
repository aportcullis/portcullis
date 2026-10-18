package connectapi_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"connectrpc.com/connect"
	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/app/auth"
	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/app/condition"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/celcondition"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/aportcullis/portcullis/internal/transport/connectapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

// administrationAPIEnv qualifies generated clients, real sessions, CEL and PostgreSQL together.
type administrationAPIEnv struct {
	pool                             *pgxpool.Pool
	httpClient                       *http.Client
	serverURL                        string
	organization                     identity.OrganizationID
	actor, actorBinding, managerRole string
	csrf                             string
	users                            portcullisv1connect.UsersClient
	roles                            portcullisv1connect.RolesClient
	bindings                         portcullisv1connect.BindingsClient
	rawUsers                         portcullisv1connect.UsersClient
}

// newAdministrationAPIEnv seeds only a disposable owner-controlled qualification database.
func newAdministrationAPIEnv(t *testing.T) administrationAPIEnv {
	return newAdministrationAPIEnvForMode(t, false)
}

// newAdministrationAPIEnvForMode also qualifies the mandatory production barrier with owner-ready policy data.
func newAdministrationAPIEnvForMode(t *testing.T, prepared bool) administrationAPIEnv {
	t.Helper()
	ctx := context.Background()
	pool := dbtest.FreshPostgres(t)
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	store, auditor := pg.NewIdentityStore(pool), pg.NewAuditStore(pool)
	sessions, err := auth.New(store, crypto.NewArgon2Hasher(crypto.Argon2Params{Memory: 8 * 1024, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 16}, 4), crypto.NewCSRFProtector(loadTestKeyring(t)), auditor, auth.Config{})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := store.ListPermissions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := authz.New(store, catalog)
	if err != nil {
		t.Fatal(err)
	}
	org, err := store.DefaultOrganizationID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := celcondition.New()
	if err != nil {
		t.Fatal(err)
	}
	conditionSettings := condition.DefaultConfig()
	conditionSettings.Timeout = time.Second
	conditions, err := condition.New(adapter, adapter, conditionSettings)
	if err != nil {
		t.Fatal(err)
	}
	repository := pg.NewBindingManagementStore(pool)
	accounts, err := administration.NewAccountPolicyService(repository, auditor, administration.BindingConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	roles, err := administration.NewRolePolicyService(repository, auditor, administration.RolePolicyConfig{Timeout: 5 * time.Second, MaxSubjects: 1024})
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := administration.NewBindingService(repository, conditions, auditor, administration.BindingConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := administration.NewManagementReader(pg.NewManagementReadStore(pool), conditions, administration.BindingConfig{Timeout: 5 * time.Second}, authz.ResourceConfig{MaxBindings: 64, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	options := connect.WithInterceptors(connectapi.NewAuthInterceptor(sessions))
	mux := http.NewServeMux()
	authPath, authHandler := portcullisv1connect.NewAuthHandler(connectapi.NewAuthService(sessions, legacy), options)
	mux.Handle(authPath, authHandler)
	managementOptions := []connect.HandlerOption{options}
	if prepared {
		managementOptions = append(managementOptions, connect.WithInterceptors(connectapi.NewPendingAdministrationInterceptor()))
	}
	for _, route := range connectapi.NewAdministrationRoutes(connectapi.NewUsersService(org, accounts, reader), connectapi.NewRolesService(org, roles, reader), connectapi.NewBindingsService(org, bindings, reader), managementOptions...) {
		mux.Handle(route.Pattern, route.Handler)
	}
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	jar, _ := cookiejar.New(nil)
	client := server.Client()
	client.Jar = jar
	serverURL, _ := url.Parse(server.URL)
	authEnvironment := authTestEnv{pool: pool, jar: jar, serverURL: serverURL, client: portcullisv1connect.NewAuthClient(client, server.URL), setupTokens: sessions}
	csrf := authEnvironment.bootstrapAndLogin(t, "management-api@example.com", "correct-horse-battery")
	environment := administrationAPIEnv{pool: pool, httpClient: client, serverURL: server.URL, organization: org, csrf: csrf, users: portcullisv1connect.NewUsersClient(client, server.URL), roles: portcullisv1connect.NewRolesClient(client, server.URL), bindings: portcullisv1connect.NewBindingsClient(client, server.URL), rawUsers: portcullisv1connect.NewUsersClient(&http.Client{Transport: client.Transport}, server.URL)}
	if err := pool.QueryRow(ctx, `select id from public.users where email='management-api@example.com'`).Scan(&environment.actor); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, 5)
	for _, permission := range identity.BindingManagementPermissions() {
		keys = append(keys, string(permission))
	}
	if _, err := pool.Exec(ctx, `insert into public.permissions (key) select unnest($1::text[]) on conflict do nothing`, keys); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into public.roles (organization_id,name) values ($1,'API qualification manager') returning id`, string(org)).Scan(&environment.managerRole); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into public.role_permissions (role_id,permission_key) select $1,key from public.permissions`, environment.managerRole); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into public.organization_authorization_revisions (organization_id,revision) values ($1,1)`, string(org)); err != nil {
		t.Fatal(err)
	}
	environment.actorBinding = environment.seedBinding(t, environment.actor, environment.managerRole)
	if !prepared {
		if _, err := pool.Exec(ctx, `alter table public.organization_memberships drop column role_id`); err != nil {
			t.Fatal(err)
		}
	}
	return environment
}

// seedBinding inserts one complete owner-only qualification grant.
func (environment administrationAPIEnv) seedBinding(t *testing.T, user, role string) string {
	t.Helper()
	ctx := context.Background()
	transaction, err := environment.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	var binding string
	if err := transaction.QueryRow(ctx, `insert into public.role_bindings (organization_id,user_id,current_version,created_at) values ($1,$2,1,clock_timestamp()) returning id`, string(environment.organization), user).Scan(&binding); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `insert into public.role_binding_versions (binding_id,organization_id,user_id,version,role_id,scope_type,created_at) values ($1,$2,$3,1,$4,'organization',clock_timestamp())`, binding, string(environment.organization), user, role); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return binding
}

// createUser invokes the public RPC to establish a grantless account.
func (environment administrationAPIEnv) createUser(t *testing.T, email string) string {
	t.Helper()
	created, err := environment.users.Create(context.Background(), withCSRF(connect.NewRequest(&portcullisv1.CreateUserRequest{Email: email, DisplayName: "API user"}), environment.csrf))
	if err != nil {
		t.Fatal(err)
	}
	if created.Header().Get("Cache-Control") != "no-store" || created.Msg.GetSetupLink().GetToken() == "" {
		t.Fatalf("setup token caching/presence: %+v", created)
	}
	return created.Msg.GetUser().GetId()
}

func TestAdministrationAPIUserLifecycleAndAuthorizedPages(t *testing.T) {
	environment := newAdministrationAPIEnv(t)
	ctx := context.Background()
	user := environment.createUser(t, "api-user@example.com")
	detail, err := environment.users.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetUserRequest{Id: user}), environment.csrf))
	if err != nil || detail.Msg.GetUser().GetHasPassword() {
		t.Fatalf("get: %+v %v", detail, err)
	}
	listed, err := environment.users.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListUsersRequest{}), environment.csrf))
	if err != nil || listed.Msg.GetPagination().GetTotalCount() != 2 {
		t.Fatalf("list: %+v %v", listed, err)
	}
	link, err := environment.users.IssueSetupLink(ctx, withCSRF(connect.NewRequest(&portcullisv1.IssueUserSetupLinkRequest{Id: user}), environment.csrf))
	if err != nil || link.Msg.GetSetupLink().GetToken() == "" {
		t.Fatalf("setup: %+v %v", link, err)
	}
	disabled, err := environment.users.Disable(ctx, withCSRF(connect.NewRequest(&portcullisv1.DisableUserRequest{Id: user}), environment.csrf))
	if err != nil || disabled.Msg.GetUser().GetStatus() != "disabled" {
		t.Fatalf("disable: %+v %v", disabled, err)
	}
	if _, err := environment.users.IssueSetupLink(ctx, withCSRF(connect.NewRequest(&portcullisv1.IssueUserSetupLinkRequest{Id: user}), environment.csrf)); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("disabled setup: %v", err)
	}
	if _, err := environment.users.Enable(ctx, withCSRF(connect.NewRequest(&portcullisv1.EnableUserRequest{Id: user}), environment.csrf)); err != nil {
		t.Fatal(err)
	}
	_, err = environment.users.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListUsersRequest{Pagination: &portcullisv1.AdministrationPage{Page: 2, PageSize: 10, ExpectedRevision: listed.Msg.GetPagination().GetAuthorizationRevision()}}), environment.csrf))
	if connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("stale page: %v", err)
	}
}

func TestAdministrationAPIRoleAndBindingLifecycle(t *testing.T) {
	environment := newAdministrationAPIEnv(t)
	ctx := context.Background()
	user := environment.createUser(t, "binding-api-user@example.com")
	created, err := environment.roles.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateRoleRequest{Name: "API reader", Permissions: []string{"connections.get"}}), environment.csrf))
	if err != nil {
		t.Fatal(err)
	}
	role := created.Msg.GetRole().GetId()
	if _, err := environment.roles.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetRoleRequest{Id: role}), environment.csrf)); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.roles.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListRolesRequest{}), environment.csrf)); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.roles.ListPermissions(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListRolePermissionsRequest{}), environment.csrf)); err != nil {
		t.Fatal(err)
	}
	updatedRole, err := environment.roles.Update(ctx, withCSRF(connect.NewRequest(&portcullisv1.UpdateRoleRequest{Id: role, ExpectedVersion: 1, Name: "API reader renamed", Permissions: []string{"connections.get"}}), environment.csrf))
	if err != nil || updatedRole.Msg.GetRole().GetVersion() != 2 {
		t.Fatalf("role update: %+v %v", updatedRole, err)
	}
	if _, err := environment.roles.Update(ctx, withCSRF(connect.NewRequest(&portcullisv1.UpdateRoleRequest{Id: role, ExpectedVersion: 1, Name: "Stale overwrite"}), environment.csrf)); connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("stale role: %v", err)
	}
	validated := &portcullisv1.BindingCondition{Title: "Temporary", Expression: `request.time < timestamp("9999-01-01T00:00:00Z")`, LanguageVersion: identity.ConditionLanguageCELV1, EnvironmentVersion: identity.ConditionEnvironmentV1}
	if _, err := environment.bindings.ValidateCondition(ctx, withCSRF(connect.NewRequest(&portcullisv1.ValidateBindingConditionRequest{Condition: validated}), environment.csrf)); err != nil {
		t.Fatal(err)
	}
	grant, err := environment.bindings.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateBindingRequest{UserId: user, RoleId: role, Scope: &portcullisv1.BindingScope{Type: "organization"}, Condition: validated}), environment.csrf))
	if err != nil {
		t.Fatal(err)
	}
	binding := grant.Msg.GetBinding().GetId()
	preserved, err := environment.bindings.Update(ctx, withCSRF(connect.NewRequest(&portcullisv1.UpdateBindingRequest{Id: binding, ExpectedVersion: 1}), environment.csrf))
	if err != nil || preserved.Msg.GetBinding().GetVersion() != 2 {
		t.Fatalf("preserve: %+v %v", preserved, err)
	}
	detail, err := environment.bindings.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetBindingRequest{Id: binding}), environment.csrf))
	if err != nil || detail.Msg.GetBinding().GetSummary().GetVersion() != 2 || detail.Msg.GetBinding().GetCondition().GetExpression() != validated.Expression {
		t.Fatalf("get: %+v %v", detail, err)
	}
	if _, err := environment.roles.Delete(ctx, withCSRF(connect.NewRequest(&portcullisv1.DeleteRoleRequest{Id: role, ExpectedVersion: 2}), environment.csrf)); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("bound role: %v", err)
	}
	cleared, err := environment.bindings.Update(ctx, withCSRF(connect.NewRequest(&portcullisv1.UpdateBindingRequest{Id: binding, ExpectedVersion: 2, ConditionEdit: &portcullisv1.UpdateBindingRequest_ClearCondition{ClearCondition: true}}), environment.csrf))
	if err != nil || cleared.Msg.GetBinding().GetVersion() != 3 {
		t.Fatalf("clear: %+v %v", cleared, err)
	}
	afterClear, err := environment.bindings.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetBindingRequest{Id: binding}), environment.csrf))
	if err != nil || afterClear.Msg.GetBinding().Condition != nil {
		t.Fatalf("condition was not cleared: %+v %v", afterClear, err)
	}
	if _, err := environment.bindings.Revoke(ctx, withCSRF(connect.NewRequest(&portcullisv1.RevokeBindingRequest{Id: binding, ExpectedVersion: 3}), environment.csrf)); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.roles.Delete(ctx, withCSRF(connect.NewRequest(&portcullisv1.DeleteRoleRequest{Id: role, ExpectedVersion: 2}), environment.csrf)); err != nil {
		t.Fatal(err)
	}
}

func TestAdministrationAPIRejectsSessionCSRFAndForgedChanges(t *testing.T) {
	environment := newAdministrationAPIEnv(t)
	ctx := context.Background()
	if _, err := environment.rawUsers.List(ctx, connect.NewRequest(&portcullisv1.ListUsersRequest{})); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("anonymous: %v", err)
	}
	if _, err := environment.users.List(ctx, connect.NewRequest(&portcullisv1.ListUsersRequest{})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("missing CSRF: %v", err)
	}
	if _, err := environment.users.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListUsersRequest{}), "forged")); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("forged CSRF: %v", err)
	}
	if _, err := environment.users.Disable(ctx, withCSRF(connect.NewRequest(&portcullisv1.DisableUserRequest{Id: environment.actor}), environment.csrf)); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("self: %v", err)
	}
	if _, err := environment.users.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetUserRequest{Id: "bad-id"}), environment.csrf)); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("malformed ID: %v", err)
	}
	if _, err := environment.roles.Update(ctx, withCSRF(connect.NewRequest(&portcullisv1.UpdateRoleRequest{Id: environment.managerRole, Name: "Stale", ExpectedVersion: 0}), environment.csrf)); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("missing version: %v", err)
	}
	if _, err := environment.bindings.ValidateCondition(ctx, withCSRF(connect.NewRequest(&portcullisv1.ValidateBindingConditionRequest{Condition: &portcullisv1.BindingCondition{Title: "Invalid", Expression: `resource.id.matches(".*")`, LanguageVersion: identity.ConditionLanguageCELV1, EnvironmentVersion: identity.ConditionEnvironmentV1}}), environment.csrf)); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("unsupported CEL: %v", err)
	}
	if _, err := environment.bindings.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateBindingRequest{UserId: environment.actor, RoleId: environment.managerRole, Scope: &portcullisv1.BindingScope{Type: "organization"}}), environment.csrf)); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("self binding: %v", err)
	}
	if _, err := environment.users.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetUserRequest{Id: "00000000-0000-0000-0000-000000000001"}), environment.csrf)); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("foreign detail: %v", err)
	}
}
