package postgres_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/app/condition"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/celcondition"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

// bindingManagementFixture seeds owner-only policy data without changing production grants or migrations.
type bindingManagementFixture struct {
	bindingSchemaFixture
	target, managerRole, readerRole, actorBinding string
	service                                       *administration.BindingService
}

// bindingManagementService connects the actual locked repository, CEL preparation and audit adapters.
func bindingManagementService(t *testing.T, fixture bindingSchemaFixture, repository administration.BindingRepository) *administration.BindingService {
	t.Helper()
	adapter, err := celcondition.New()
	if err != nil {
		t.Fatal(err)
	}
	conditions, err := condition.New(adapter, adapter, condition.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	service, err := administration.NewBindingService(repository, conditions, pg.NewAuditStore(fixture.pool), administration.BindingConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

// newBindingManagementFixture prepares independent membership and unrestricted qualification grants.
func newBindingManagementFixture(t *testing.T) bindingManagementFixture {
	t.Helper()
	fixture := bindingManagementFixture{bindingSchemaFixture: newBindingSchemaFixture(t)}
	ctx := context.Background()
	if _, err := fixture.pool.Exec(ctx, `insert into public.permissions (key) select unnest($1::text[]) on conflict do nothing`, bindingPermissionStrings(identity.BindingManagementPermissions())); err != nil {
		t.Fatal(err)
	}
	if err := fixture.pool.QueryRow(ctx, `insert into public.roles (organization_id,name) values ($1,'Policy qualification manager') returning id`, fixture.organization).Scan(&fixture.managerRole); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `insert into public.role_permissions (role_id,permission_key) select $1,key from public.permissions`, fixture.managerRole); err != nil {
		t.Fatal(err)
	}
	if err := fixture.pool.QueryRow(ctx, `insert into public.roles (organization_id,name) values ($1,'Qualification reader') returning id`, fixture.organization).Scan(&fixture.readerRole); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `insert into public.role_permissions (role_id,permission_key) values ($1,'connections.get')`, fixture.readerRole); err != nil {
		t.Fatal(err)
	}
	if err := fixture.pool.QueryRow(ctx, `insert into public.users (email) values ('binding-target@example.com') returning id`).Scan(&fixture.target); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `insert into public.organization_memberships (organization_id,user_id,role_id) values ($1,$2,$3)`, fixture.organization, fixture.target, fixture.role); err != nil {
		t.Fatal(err)
	}
	activateBindingFixture(t, fixture.bindingSchemaFixture)
	manager := fixture.bindingSchemaFixture
	manager.role = fixture.managerRole
	fixture.actorBinding = requireSchemaBinding(t, manager, "organization", nil, nil)
	fixture.service = bindingManagementService(t, fixture.bindingSchemaFixture, pg.NewBindingManagementStore(fixture.pool))
	return fixture
}

// bindingPermissionStrings converts qualification catalog data for owner inserts.
func bindingPermissionStrings(permissions []identity.Permission) []string {
	keys := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		keys = append(keys, string(permission))
	}
	return keys
}

// bindingDefinition supplies a local target and independently reusable role definition.
func (fixture bindingManagementFixture) bindingDefinition() identity.BindingDefinition {
	return identity.BindingDefinition{UserID: identity.UserID(fixture.target), RoleID: identity.RoleID(fixture.readerRole), Scope: identity.ResourceScope{Type: identity.ScopeOrganization}}
}

func TestBindingManagementStoreCommitsLifecycleAndPreservesLegacyPermissions(t *testing.T) {
	fixture := newBindingManagementFixture(t)
	ctx := context.Background()
	legacy := pg.NewIdentityStore(fixture.pool)
	before, err := legacy.PermissionsForUser(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.target))
	if err != nil {
		t.Fatal(err)
	}
	administrator := administrationEnv{store: legacy}
	administrator.openSession(t, identity.UserID(fixture.target))
	definition := fixture.bindingDefinition()
	definition.HasCondition = true
	definition.Condition = identity.Condition{Title: "Temporary", Expression: `request.time < timestamp("9999-01-01T00:00:00Z")`, LanguageVersion: identity.ConditionLanguageCELV1, EnvironmentVersion: identity.ConditionEnvironmentV1}
	created, err := fixture.service.Create(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), definition)
	if err != nil {
		t.Fatal(err)
	}
	resourceService := bindingResourceService(t, fixture.bindingSchemaFixture)
	requireResourcePermission := func(permission identity.Permission, allowed bool, revision int64) {
		t.Helper()
		decision, err := resourceService.AuthorizeResource(ctx, identity.UserID(fixture.target), permission, authz.ResourceTarget{OrganizationID: identity.OrganizationID(fixture.organization), Type: identity.ResourceConnection, ID: fixture.connection})
		if allowed {
			if err != nil || decision.BindingID != created.Binding.ID || decision.Revision != revision {
				t.Fatalf("committed binding read: %+v %v", decision, err)
			}
		} else if !errors.Is(err, authz.ErrPermissionDenied) {
			t.Fatalf("legacy or stale binding authority: %+v %v", decision, err)
		}
	}
	requireResourcePermission(identity.PermissionConnectionsGet, true, 2)
	requireResourcePermission(identity.PermissionConnectionsDelete, false, 2)
	preserved, err := fixture.service.Update(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), created.Binding.ID, 1, identity.BindingUpdate{Scope: &identity.ResourceScope{Type: identity.ScopeConnection, ConnectionID: fixture.connection}})
	if err != nil || preserved.Binding.Condition != definition.Condition {
		t.Fatalf("preserve: %+v %v", preserved, err)
	}
	requireResourcePermission(identity.PermissionConnectionsGet, true, 3)
	replacement := definition.Condition
	replacement.Expression = `false`
	if _, err := fixture.service.Update(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), created.Binding.ID, 2, identity.BindingUpdate{ConditionMode: identity.ConditionReplace, Condition: replacement}); err != nil {
		t.Fatal(err)
	}
	requireResourcePermission(identity.PermissionConnectionsGet, false, 4)
	cleared, err := fixture.service.Update(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), created.Binding.ID, 3, identity.BindingUpdate{ConditionMode: identity.ConditionClear})
	if err != nil || cleared.Binding.HasCondition {
		t.Fatalf("clear: %+v %v", cleared, err)
	}
	requireResourcePermission(identity.PermissionConnectionsGet, true, 5)
	revoked, err := fixture.service.Revoke(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), created.Binding.ID, 4)
	if err != nil || !revoked.Binding.Revoked || revoked.Binding.Version != 5 || revoked.Revision != 6 {
		t.Fatalf("revoke: %+v %v", revoked, err)
	}
	requireResourcePermission(identity.PermissionConnectionsGet, false, 6)
	var versionCount, successEvents, activeSessions int
	var originalSource string
	if err := fixture.pool.QueryRow(ctx, `select (select count(*) from public.role_binding_versions where organization_id=$1 and binding_id=$2), (select condition_expression from public.role_binding_versions where organization_id=$1 and binding_id=$2 and version=1), (select count(*) from public.audit_events where organization_id=$1 and target_id=$2::text and outcome='SUCCEEDED'), (select count(*) from public.sessions where user_id=$3 and revoked_at is null)`, fixture.organization, string(created.Binding.ID), fixture.target).Scan(&versionCount, &originalSource, &successEvents, &activeSessions); err != nil {
		t.Fatal(err)
	}
	if versionCount != 5 || originalSource != definition.Condition.Expression || successEvents != 5 || activeSessions != 0 {
		t.Fatalf("history/evidence: versions=%d events=%d sessions=%d source=%q", versionCount, successEvents, activeSessions, originalSource)
	}
	var sourceLeaked bool
	if err := fixture.pool.QueryRow(ctx, `select exists(select 1 from public.audit_events where organization_id=$1 and target_id=$2 and metadata::text like '%timestamp(%')`, fixture.organization, string(created.Binding.ID)).Scan(&sourceLeaked); err != nil || sourceLeaked {
		t.Fatalf("condition source in ordinary evidence: %t %v", sourceLeaked, err)
	}
	after, err := legacy.PermissionsForUser(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.target))
	if err != nil || !slices.Equal(before, after) {
		t.Fatalf("legacy grant changed: before=%v after=%v error=%v", before, after, err)
	}
	if _, err := fixture.service.Revoke(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), created.Binding.ID, 4); !errors.Is(err, identity.ErrBindingConflict) {
		t.Fatalf("stale revoke: %v", err)
	}
	if _, err := fixture.service.Update(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), created.Binding.ID, 5, identity.BindingUpdate{RoleID: identity.RoleID(fixture.readerRole)}); !errors.Is(err, identity.ErrBindingRevoked) {
		t.Fatalf("revival: %v", err)
	}
}

// bindingPolicyCounts captures observable atomic state across a refused mutation.
type bindingPolicyCounts struct{ heads, versions, revision, events, sessions int64 }

func readBindingPolicyCounts(t *testing.T, fixture bindingManagementFixture) bindingPolicyCounts {
	t.Helper()
	var counts bindingPolicyCounts
	if err := fixture.pool.QueryRow(context.Background(), `select (select count(*) from public.role_bindings where organization_id=$1), (select count(*) from public.role_binding_versions where organization_id=$1), (select revision from public.organization_authorization_revisions where organization_id=$1), (select count(*) from public.audit_events where organization_id=$1 and outcome='SUCCEEDED'), (select count(*) from public.sessions where user_id=$2 and revoked_at is null)`, fixture.organization, fixture.target).Scan(&counts.heads, &counts.versions, &counts.revision, &counts.events, &counts.sessions); err != nil {
		t.Fatal(err)
	}
	return counts
}

func TestBindingManagementStoreRefusesInvalidAndForeignWrites(t *testing.T) {
	fixture := newBindingManagementFixture(t)
	ctx := context.Background()
	for _, scenario := range []struct {
		name  string
		alter func(*identity.BindingDefinition)
		want  error
	}{
		{"self", func(definition *identity.BindingDefinition) { definition.UserID = identity.UserID(fixture.user) }, identity.ErrSelfAdministration},
		{"foreign member", func(definition *identity.BindingDefinition) { definition.UserID = identity.UserID(fixture.foreignUser) }, identity.ErrUserNotFound},
		{"foreign role", func(definition *identity.BindingDefinition) { definition.RoleID = identity.RoleID(fixture.foreignRole) }, identity.ErrRoleNotFound},
		{"foreign connection", func(definition *identity.BindingDefinition) {
			definition.Scope = identity.ResourceScope{Type: identity.ScopeConnection, ConnectionID: fixture.foreignConnection}
		}, identity.ErrInvalidResourceScope},
		{"invalid CEL", func(definition *identity.BindingDefinition) {
			definition.HasCondition = true
			definition.Condition = identity.Condition{Title: "Invalid", Expression: `resource.id.matches(".*")`, LanguageVersion: identity.ConditionLanguageCELV1, EnvironmentVersion: identity.ConditionEnvironmentV1}
		}, identity.ErrInvalidCondition},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			definition := fixture.bindingDefinition()
			scenario.alter(&definition)
			before := readBindingPolicyCounts(t, fixture)
			_, err := fixture.service.Create(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), definition)
			if !errors.Is(err, scenario.want) || !reflect.DeepEqual(before, readBindingPolicyCounts(t, fixture)) {
				t.Fatalf("refusal: %v want %v", err, scenario.want)
			}
		})
	}
}

func TestBindingManagementStoreRollsBackHistoryRevisionSessionAndAuditFailures(t *testing.T) {
	for _, failure := range []struct{ name, table, operation, when string }{
		{"audit", "audit_events", "INSERT", "new.action like 'BINDING_%' and new.outcome='SUCCEEDED'"},
		{"revision", "organization_authorization_revisions", "UPDATE", "true"},
		{"session", "sessions", "UPDATE", "true"},
	} {
		t.Run(failure.name, func(t *testing.T) {
			fixture := newBindingManagementFixture(t)
			legacy := pg.NewIdentityStore(fixture.pool)
			administrator := administrationEnv{store: legacy}
			administrator.openSession(t, identity.UserID(fixture.target))
			ctx := context.Background()
			if _, err := fixture.pool.Exec(ctx, `create function public.refuse_binding_fixture_write() returns trigger language plpgsql as $$begin if `+failure.when+` then raise exception 'qualification rollback'; end if; return new; end$$; create trigger refuse_binding_fixture_write before `+failure.operation+` on public.`+failure.table+` for each row execute function public.refuse_binding_fixture_write()`); err != nil {
				t.Fatal(err)
			}
			before := readBindingPolicyCounts(t, fixture)
			mutation, err := fixture.service.Create(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), fixture.bindingDefinition())
			if !errors.Is(err, administration.ErrBindingManagementUnavailable) || mutation != (administration.BindingMutation{}) || !reflect.DeepEqual(before, readBindingPolicyCounts(t, fixture)) {
				t.Fatalf("atomic failure: %+v %v", mutation, err)
			}
		})
	}
}

func TestBindingManagementStoreUsesIndependentMembership(t *testing.T) {
	fixture := newBindingManagementFixture(t)
	// Removing the legacy assignment only in this disposable database proves the new policy boundary is independent.
	if _, err := fixture.pool.Exec(context.Background(), `alter table public.organization_memberships drop column role_id`); err != nil {
		t.Fatal(err)
	}
	mutation, err := fixture.service.Create(context.Background(), identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), fixture.bindingDefinition())
	if err != nil || mutation.Binding.UserID != identity.UserID(fixture.target) {
		t.Fatalf("independent membership: %+v %v", mutation, err)
	}
}

// appendOwnerBindingVersion models an independently committed policy writer in qualification databases.
func appendOwnerBindingVersion(t *testing.T, fixture bindingManagementFixture, binding string, role string, scope string, connection *string, expression *string) {
	t.Helper()
	ctx := context.Background()
	transaction, err := fixture.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	if _, err := transaction.Exec(ctx, `insert into public.role_binding_versions (binding_id,organization_id,user_id,version,role_id,scope_type,connection_id,condition_title,condition_expression,condition_language_version,condition_environment_version,created_at)
 select binding.id,binding.organization_id,binding.user_id,binding.current_version+1,$3,$4,$5,case when $6::text is null then null else 'Qualification' end,$6,case when $6::text is null then null else 'cel-v1' end,case when $6::text is null then null else 'portcullis-v1' end,clock_timestamp() from public.role_bindings binding where binding.organization_id=$1 and binding.id=$2`, fixture.organization, binding, role, scope, connection, expression); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(ctx, `update public.role_bindings set current_version=current_version+1 where organization_id=$1 and id=$2`, fixture.organization, binding); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestBindingManagementStoreNeverFlattensConditionalScopedOrLegacyAuthority(t *testing.T) {
	for _, scenario := range []string{"conditional manager", "connection manager", "disabled manager", "missing delegation permission", "conditional delegation", "scoped delegation", "no binding despite legacy admin"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newBindingManagementFixture(t)
			ctx := context.Background()
			want := identity.ErrActorNotAuthorized
			switch scenario {
			case "conditional manager":
				expression := `true`
				appendOwnerBindingVersion(t, fixture, fixture.actorBinding, fixture.managerRole, "organization", nil, &expression)
			case "connection manager":
				appendOwnerBindingVersion(t, fixture, fixture.actorBinding, fixture.managerRole, "connection", &fixture.connection, nil)
			case "disabled manager":
				if _, err := fixture.pool.Exec(ctx, `update public.users set status='disabled' where id=$1`, fixture.user); err != nil {
					t.Fatal(err)
				}
			case "missing delegation permission":
				if _, err := fixture.pool.Exec(ctx, `update public.role_permissions set deleted_at=clock_timestamp() where role_id=$1 and permission_key='connections.get'`, fixture.managerRole); err != nil {
					t.Fatal(err)
				}
				want = identity.ErrPrivilegeEscalation
			case "conditional delegation", "scoped delegation":
				if _, err := fixture.pool.Exec(ctx, `update public.role_permissions set deleted_at=clock_timestamp() where role_id=$1 and permission_key='connections.get'`, fixture.managerRole); err != nil {
					t.Fatal(err)
				}
				limited := fixture.bindingSchemaFixture
				limited.role = fixture.readerRole
				if scenario == "conditional delegation" {
					expression := `true`
					requireSchemaBinding(t, limited, "organization", nil, &expression)
				} else {
					requireSchemaBinding(t, limited, "connection", &fixture.connection, nil)
				}
				want = identity.ErrPrivilegeEscalation
			case "no binding despite legacy admin":
				if _, err := fixture.pool.Exec(ctx, `insert into public.role_binding_versions (binding_id,organization_id,user_id,version,role_id,scope_type,created_at,revoked_at) select binding_id,organization_id,user_id,2,role_id,scope_type,clock_timestamp(),clock_timestamp() from public.role_binding_versions where organization_id=$1 and binding_id=$2 and version=1`, fixture.organization, fixture.actorBinding); err != nil {
					t.Fatal(err)
				}
				if _, err := fixture.pool.Exec(ctx, `update public.role_bindings set current_version=2 where organization_id=$1 and id=$2`, fixture.organization, fixture.actorBinding); err != nil {
					t.Fatal(err)
				}
			}
			before := readBindingPolicyCounts(t, fixture)
			_, err := fixture.service.Create(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), fixture.bindingDefinition())
			if !errors.Is(err, want) || before != readBindingPolicyCounts(t, fixture) {
				t.Fatalf("unsafe manager: %v want %v", err, want)
			}
		})
	}
}

// insertBindingPolicyRole creates a custom permission bundle without changing any legacy assignment.
func insertBindingPolicyRole(t *testing.T, fixture bindingManagementFixture, name string, permissions []identity.Permission) string {
	t.Helper()
	ctx := context.Background()
	var role string
	if err := fixture.pool.QueryRow(ctx, `insert into public.roles (organization_id,name) values ($1,$2) returning id`, fixture.organization, name).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `insert into public.role_permissions (role_id,permission_key) select $1,unnest($2::text[])`, role, bindingPermissionStrings(permissions)); err != nil {
		t.Fatal(err)
	}
	return role
}

func TestBindingManagementStoreProtectsFullCatalogRecoveryAcrossSplitRoles(t *testing.T) {
	for _, scenario := range []string{"last administrator", "conditional reserve", "scoped reserve", "disabled reserve", "independent reserve", "split reserve"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newBindingManagementFixture(t)
			ctx := context.Background()
			catalog, err := pg.NewIdentityStore(fixture.pool).ListPermissions(ctx)
			if err != nil {
				t.Fatal(err)
			}
			piecePermission := identity.PermissionBindingsUpdate
			required := identity.SelectRecoveryPermissions(catalog)
			rest := slices.DeleteFunc(slices.Clone(required), func(permission identity.Permission) bool { return permission == piecePermission })
			controller := insertBindingPolicyRole(t, fixture, "Limited controller", []identity.Permission{identity.PermissionBindingsDelete, piecePermission})
			piece := insertBindingPolicyRole(t, fixture, "Recovery piece", []identity.Permission{piecePermission})
			restRole := insertBindingPolicyRole(t, fixture, "Recovery rest", rest)
			appendOwnerBindingVersion(t, fixture, fixture.actorBinding, controller, "organization", nil, nil)
			target := fixture.bindingSchemaFixture
			target.user = fixture.target
			target.role = piece
			targetBinding := requireSchemaBinding(t, target, "organization", nil, nil)
			target.role = restRole
			requireSchemaBinding(t, target, "organization", nil, nil)
			if scenario != "last administrator" {
				reserve := fixture.bindingSchemaFixture
				reserve.user = fixture.foreignUser
				reserve.role = fixture.managerRole
				if _, err := fixture.pool.Exec(ctx, `insert into public.organization_memberships (organization_id,user_id,role_id) values ($1,$2,$3)`, fixture.organization, fixture.foreignUser, fixture.role); err != nil {
					t.Fatal(err)
				}
				var expression *string
				scope := "organization"
				var connection *string
				switch scenario {
				case "conditional reserve":
					source := `true`
					expression = &source
				case "scoped reserve":
					scope = "connection"
					connection = &fixture.connection
				case "disabled reserve":
					if _, err := fixture.pool.Exec(ctx, `update public.users set status='disabled' where id=$1`, fixture.foreignUser); err != nil {
						t.Fatal(err)
					}
				case "split reserve":
					reserve.role = piece
					requireSchemaBinding(t, reserve, "organization", nil, nil)
					reserve.role = restRole
				}
				requireSchemaBinding(t, reserve, scope, connection, expression)
			}
			before := readBindingPolicyCounts(t, fixture)
			mutation, err := fixture.service.Revoke(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), identity.BindingID(targetBinding), 1)
			allowed := scenario == "independent reserve" || scenario == "split reserve"
			if allowed {
				if err != nil || !mutation.Binding.Revoked {
					t.Fatalf("reserve eligibility: %+v %v", mutation, err)
				}
			} else if !errors.Is(err, identity.ErrLastAdministrator) || before != readBindingPolicyCounts(t, fixture) {
				t.Fatalf("full catalog recovery: %+v %v", mutation, err)
			}
		})
	}
}

func TestBindingManagementStoreKeepsPreparedRuntimeReadOnly(t *testing.T) {
	fixture := newBindingManagementFixture(t)
	ctx := context.Background()
	settings := fixture.pool.Config().Copy()
	settings.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		_, err := connection.Exec(ctx, `set role portcullis_runtime`)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	service := bindingManagementService(t, fixture.bindingSchemaFixture, pg.NewBindingManagementStore(pool))
	before := readBindingPolicyCounts(t, fixture)
	if _, err := service.Create(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), fixture.bindingDefinition()); !errors.Is(err, administration.ErrBindingManagementUnavailable) || before != readBindingPolicyCounts(t, fixture) {
		t.Fatalf("prepared runtime mutation: %v", err)
	}
	if err := pg.Migrate(ctx, fixture.pool); err == nil {
		t.Fatal("activation accepted owner-seeded preparation policy")
	}
}

func TestBindingManagementStoreSerializesOptimisticWriters(t *testing.T) {
	fixture := newBindingManagementFixture(t)
	subject := fixture.bindingSchemaFixture
	subject.user = fixture.target
	subject.role = fixture.readerRole
	binding := requireSchemaBinding(t, subject, "organization", nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, scope := range []identity.ResourceScope{{Type: identity.ScopeOrganization}, {Type: identity.ScopeConnection, ConnectionID: fixture.connection}} {
		go func() {
			<-start
			_, err := fixture.service.Update(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), identity.BindingID(binding), 1, identity.BindingUpdate{Scope: &scope})
			results <- err
		}()
	}
	close(start)
	var succeeded, conflicts int
	for range 2 {
		select {
		case err := <-results:
			switch {
			case err == nil:
				succeeded++
			case errors.Is(err, identity.ErrBindingConflict):
				conflicts++
			default:
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	var versions, revision int
	if err := fixture.pool.QueryRow(ctx, `select (select count(*) from public.role_binding_versions where organization_id=$1 and binding_id=$2), (select revision from public.organization_authorization_revisions where organization_id=$1)`, fixture.organization, binding).Scan(&versions, &revision); err != nil {
		t.Fatal(err)
	}
	if succeeded != 1 || conflicts != 1 || versions != 2 || revision != 2 {
		t.Fatalf("competing writes: success=%d conflict=%d versions=%d revision=%d", succeeded, conflicts, versions, revision)
	}
}

// waitBindingPolicyLock verifies a real blocked database statement before committing a competing policy change.
func waitBindingPolicyLock(t *testing.T, ctx context.Context, pool *pgxpool.Pool, applicationName string) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := pool.QueryRow(ctx, `select exists(select 1 from pg_stat_activity where application_name=$1 and wait_event_type='Lock' and wait_event='advisory')`, applicationName).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func TestBindingManagementStoreReauthorizesAfterPolicyLockWait(t *testing.T) {
	fixture := newBindingManagementFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reserve := fixture.bindingSchemaFixture
	reserve.user = fixture.target
	reserve.role = fixture.managerRole
	requireSchemaBinding(t, reserve, "organization", nil, nil)
	settings := fixture.pool.Config().Copy()
	applicationName := "binding-wait-" + fixture.organization
	settings.ConnConfig.RuntimeParams["application_name"] = applicationName
	pool, err := pgxpool.NewWithConfig(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	service := bindingManagementService(t, fixture.bindingSchemaFixture, pg.NewBindingManagementStore(pool))
	results := make(chan error, 1)
	err = pg.NewBindingManagementStore(fixture.pool).WithBindingTransaction(ctx, identity.OrganizationID(fixture.organization), func(transaction administration.BindingTransaction) error {
		go func() {
			_, err := service.Create(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), fixture.bindingDefinition())
			results <- err
		}()
		waitBindingPolicyLock(t, ctx, fixture.pool, applicationName)
		binding, err := transaction.LockBindingVersion(ctx, identity.BindingID(fixture.actorBinding))
		if err != nil {
			return err
		}
		binding.Version++
		binding.Revoked = true
		at, err := transaction.ObserveBindingMutationTime(ctx, binding.UserID)
		if err != nil {
			return err
		}
		if err := transaction.AppendBindingVersion(ctx, binding, 1); err != nil {
			return err
		}
		actor := identity.UserID(fixture.target)
		_, err = transaction.FinalizeBindingChange(ctx, identity.UserID(fixture.user), audit.Event{ActorType: audit.ActorUser, ActorUserID: &actor, OrganizationID: identity.OrganizationID(fixture.organization), OccurredAt: at, Action: audit.ActionBindingRevoked, TargetType: audit.TargetTypeBinding, TargetID: fixture.actorBinding, Outcome: audit.OutcomeSucceeded})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-results:
		if !errors.Is(err, identity.ErrActorNotAuthorized) {
			t.Fatalf("stale actor authority survived lock wait: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestBindingManagementStoreRejectsUUIDAliasesAtPolicyBoundary(t *testing.T) {
	fixture := newBindingManagementFixture(t)
	ctx := context.Background()
	const canonicalUser = "a1111111-1111-4111-8111-111111111111"
	if _, err := fixture.pool.Exec(ctx, `insert into public.users (id,email) values ($1,'canonical-policy@example.com');`, canonicalUser); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `insert into public.organization_memberships (organization_id,user_id,role_id) values ($1,$2,$3)`, fixture.organization, canonicalUser, fixture.role); err != nil {
		t.Fatal(err)
	}
	manager := fixture.bindingSchemaFixture
	manager.user = canonicalUser
	manager.role = fixture.managerRole
	requireSchemaBinding(t, manager, "organization", nil, nil)
	for _, scenario := range []struct {
		name, actor, target string
		want                error
	}{
		{"uppercase target", canonicalUser, strings.ToUpper(canonicalUser), identity.ErrUserNotFound},
		{"compact target", canonicalUser, strings.ReplaceAll(canonicalUser, "-", ""), identity.ErrUserNotFound},
		{"uppercase actor", strings.ToUpper(canonicalUser), canonicalUser, identity.ErrActorNotAuthorized},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			definition := fixture.bindingDefinition()
			definition.UserID = identity.UserID(scenario.target)
			before := readBindingPolicyCounts(t, fixture)
			_, err := fixture.service.Create(ctx, identity.OrganizationID(fixture.organization), identity.UserID(scenario.actor), definition)
			if !errors.Is(err, scenario.want) || before != readBindingPolicyCounts(t, fixture) {
				t.Fatalf("UUID alias bypass: %v want %v", err, scenario.want)
			}
		})
	}
	const canonicalOrganization = "b2222222-2222-4222-8222-222222222222"
	if _, err := fixture.pool.Exec(ctx, `insert into public.organizations (id,name,slug) values ($1,'Canonical qualification','canonical-qualification')`, canonicalOrganization); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `insert into public.organization_authorization_revisions (organization_id,revision) values ($1,1)`, canonicalOrganization); err != nil {
		t.Fatal(err)
	}
	called := false
	err := pg.NewBindingManagementStore(fixture.pool).WithBindingTransaction(ctx, identity.OrganizationID(strings.ToUpper(canonicalOrganization)), func(administration.BindingTransaction) error { called = true; return nil })
	if !errors.Is(err, administration.ErrBindingManagementInactive) || called {
		t.Fatalf("UUID alias split policy lock keys: called=%t error=%v", called, err)
	}
}
