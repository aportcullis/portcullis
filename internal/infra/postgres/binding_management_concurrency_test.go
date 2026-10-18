package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

// insertBindingPolicyMember adds a qualification member with an unchanged legacy role assignment.
func insertBindingPolicyMember(t *testing.T, fixture bindingManagementFixture, email string) string {
	t.Helper()
	ctx := context.Background()
	var user string
	if err := fixture.pool.QueryRow(ctx, `insert into public.users (email) values ($1) returning id`, email).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(ctx, `insert into public.organization_memberships (organization_id,user_id,role_id) values ($1,$2,$3)`, fixture.organization, user, fixture.role); err != nil {
		t.Fatal(err)
	}
	return user
}

func TestBindingManagementStoreSerializesCompetingRecoveryRemoval(t *testing.T) {
	fixture := newBindingManagementFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	catalog, err := pg.NewIdentityStore(fixture.pool).ListPermissions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	required := identity.SelectRecoveryPermissions(catalog)
	rest := make([]identity.Permission, 0, len(required))
	for _, permission := range required {
		if permission != identity.PermissionBindingsUpdate {
			rest = append(rest, permission)
		}
	}
	controller := insertBindingPolicyRole(t, fixture, "Competing controller", []identity.Permission{identity.PermissionBindingsDelete, identity.PermissionBindingsUpdate})
	piece := insertBindingPolicyRole(t, fixture, "Competing recovery piece", []identity.Permission{identity.PermissionBindingsUpdate})
	restRole := insertBindingPolicyRole(t, fixture, "Competing recovery rest", rest)
	appendOwnerBindingVersion(t, fixture, fixture.actorBinding, controller, "organization", nil, nil)
	otherActor := insertBindingPolicyMember(t, fixture, "other-controller@example.com")
	grant := fixture.bindingSchemaFixture
	grant.user = otherActor
	grant.role = controller
	requireSchemaBinding(t, grant, "organization", nil, nil)
	otherTarget := insertBindingPolicyMember(t, fixture, "other-recovery@example.com")
	bindings := make(map[string]string, 2)
	for _, target := range []string{fixture.target, otherTarget} {
		grant.user = target
		grant.role = piece
		bindings[target] = requireSchemaBinding(t, grant, "organization", nil, nil)
		grant.role = restRole
		requireSchemaBinding(t, grant, "organization", nil, nil)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		_, err := fixture.service.Revoke(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), identity.BindingID(bindings[fixture.target]), 1)
		results <- err
	}()
	go func() {
		<-start
		_, err := fixture.service.Update(ctx, identity.OrganizationID(fixture.organization), identity.UserID(otherActor), identity.BindingID(bindings[otherTarget]), 1, identity.BindingUpdate{ConditionMode: identity.ConditionReplace, Condition: identity.Condition{Title: "Conditional recovery is insufficient", Expression: `true`, LanguageVersion: identity.ConditionLanguageCELV1, EnvironmentVersion: identity.ConditionEnvironmentV1}})
		results <- err
	}()
	close(start)
	var succeeded, protected int
	for range 2 {
		select {
		case err := <-results:
			switch {
			case err == nil:
				succeeded++
			case errors.Is(err, identity.ErrLastAdministrator):
				protected++
			default:
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	var remaining, revision int
	if err := fixture.pool.QueryRow(ctx, `select (select count(*) from public.role_bindings binding join public.role_binding_versions version on version.binding_id=binding.id and version.organization_id=binding.organization_id and version.version=binding.current_version where binding.organization_id=$1 and version.role_id=$2 and version.revoked_at is null and version.condition_expression is null), (select revision from public.organization_authorization_revisions where organization_id=$1)`, fixture.organization, piece).Scan(&remaining, &revision); err != nil {
		t.Fatal(err)
	}
	if succeeded != 1 || protected != 1 || remaining != 1 || revision != 2 {
		t.Fatalf("recovery write skew: success=%d protected=%d recovery=%d revision=%d", succeeded, protected, remaining, revision)
	}
}

func TestBindingManagementStoreCancelsLockWaitWithoutSideEffects(t *testing.T) {
	fixture := newBindingManagementFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	before := readBindingPolicyCounts(t, fixture)
	err := pg.NewBindingManagementStore(fixture.pool).WithBindingTransaction(ctx, identity.OrganizationID(fixture.organization), func(administration.BindingTransaction) error {
		blocked, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
		defer cancel()
		mutation, err := fixture.service.Create(blocked, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), fixture.bindingDefinition())
		if !errors.Is(err, context.DeadlineExceeded) || mutation != (administration.BindingMutation{}) {
			t.Fatalf("lock deadline: %+v %v", mutation, err)
		}
		return nil
	})
	if err != nil || before != readBindingPolicyCounts(t, fixture) {
		t.Fatalf("cancelled mutation: %v", err)
	}
}

func TestBindingManagementStoreObservesMutationTimeAfterWaitingForPolicyLock(t *testing.T) {
	fixture := newBindingManagementFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	settings := fixture.pool.Config().Copy()
	applicationName := "binding-clock-" + fixture.organization
	settings.ConnConfig.RuntimeParams["application_name"] = applicationName
	pool, err := pgxpool.NewWithConfig(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	service := bindingManagementService(t, fixture.bindingSchemaFixture, pg.NewBindingManagementStore(pool))
	type mutationResult struct {
		mutation administration.BindingMutation
		err      error
	}
	results := make(chan mutationResult, 1)
	var heldAt time.Time
	err = pg.NewBindingManagementStore(fixture.pool).WithBindingTransaction(ctx, identity.OrganizationID(fixture.organization), func(transaction administration.BindingTransaction) error {
		go func() {
			mutation, err := service.Create(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), fixture.bindingDefinition())
			results <- mutationResult{mutation, err}
		}()
		waitBindingPolicyLock(t, ctx, fixture.pool, applicationName)
		var observeErr error
		heldAt, observeErr = transaction.ObserveBindingMutationTime(ctx, identity.UserID(fixture.target))
		return observeErr
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case observed := <-results:
		if observed.err != nil || !observed.mutation.OccurredAt.After(heldAt) {
			t.Fatalf("pre-lock mutation timestamp: held=%v mutation=%+v error=%v", heldAt, observed.mutation, observed.err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestBindingManagementStoreObservesTimeAfterSessionRowLockWait(t *testing.T) {
	fixture := newBindingManagementFixture(t)
	administrator := administrationEnv{store: pg.NewIdentityStore(fixture.pool)}
	administrator.openSession(t, identity.UserID(fixture.target))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	settings := fixture.pool.Config().Copy()
	applicationName := "binding-session-clock-" + fixture.organization
	settings.ConnConfig.RuntimeParams["application_name"] = applicationName
	pool, err := pgxpool.NewWithConfig(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	service := bindingManagementService(t, fixture.bindingSchemaFixture, pg.NewBindingManagementStore(pool))
	owner, err := fixture.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Rollback(ctx) }()
	if _, err := owner.Exec(ctx, `select id from public.sessions where user_id=$1 and revoked_at is null order by id for update`, fixture.target); err != nil {
		t.Fatal(err)
	}
	type mutationResult struct {
		mutation administration.BindingMutation
		err      error
	}
	results := make(chan mutationResult, 1)
	go func() {
		mutation, err := service.Create(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), fixture.bindingDefinition())
		results <- mutationResult{mutation, err}
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := fixture.pool.QueryRow(ctx, `select exists(select 1 from pg_stat_activity where application_name=$1 and wait_event_type='Lock' and wait_event in ('transactionid','tuple'))`, applicationName).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	var heldAt time.Time
	if err := owner.QueryRow(ctx, `select clock_timestamp()`).Scan(&heldAt); err != nil {
		t.Fatal(err)
	}
	if err := owner.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case observed := <-results:
		if observed.err != nil || !observed.mutation.OccurredAt.After(heldAt) {
			t.Fatalf("session lock timestamp: held=%v mutation=%+v error=%v", heldAt, observed.mutation, observed.err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
