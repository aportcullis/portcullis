package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/app/condition"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/celcondition"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

// managementReaderScenario injects the actual predicate and metadata read adapters.
func managementReaderScenario(t *testing.T, repository administration.ManagementReadRepository, maxBindings int) *administration.ManagementReader {
	t.Helper()
	adapter, err := celcondition.New()
	if err != nil {
		t.Fatal(err)
	}
	conditions, err := condition.New(adapter, adapter, condition.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	reader, err := administration.NewManagementReader(repository, conditions, administration.BindingConfig{Timeout: 5 * time.Second}, authz.ResourceConfig{Timeout: 5 * time.Second, MaxBindings: maxBindings})
	if err != nil {
		t.Fatal(err)
	}
	return reader
}

// changingReadRepository coordinates a committed writer after the exact authorization snapshot read.
type changingReadRepository struct {
	repository    administration.ManagementReadRepository
	afterDecision func()
}

func (repository changingReadRepository) WithManagementRead(ctx context.Context, organization identity.OrganizationID, read func(administration.ManagementReadSnapshot) error) error {
	return repository.repository.WithManagementRead(ctx, organization, func(snapshot administration.ManagementReadSnapshot) error {
		return read(changingReadSnapshot{ManagementReadSnapshot: snapshot, afterDecision: repository.afterDecision})
	})
}

// changingReadSnapshot preserves the real repository while exposing a deterministic concurrency boundary.
type changingReadSnapshot struct {
	administration.ManagementReadSnapshot
	afterDecision func()
}

func (snapshot changingReadSnapshot) LoadAuthorizationSnapshot(ctx context.Context, actor identity.UserID, permission identity.Permission, target authz.ResourceTarget, maxBindings int) (authz.AuthorizationSnapshot, error) {
	decision, err := snapshot.ManagementReadSnapshot.LoadAuthorizationSnapshot(ctx, actor, permission, target, maxBindings)
	if err == nil {
		snapshot.afterDecision()
	}
	return decision, err
}

func TestManagementReadKeepsAuthorizationRowsAndTotalInOneSnapshot(t *testing.T) {
	fixture := newBindingManagementFixture(t)
	ctx := context.Background()
	base := pg.NewManagementReadStore(fixture.pool)
	repository := changingReadRepository{repository: base, afterDecision: func() {
		transaction, err := fixture.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = transaction.Rollback(ctx) }()
		if _, err := transaction.Exec(ctx, `update public.role_permissions set deleted_at=clock_timestamp() where role_id=$1 and permission_key='users.list'`, fixture.managerRole); err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.Exec(ctx, `update public.roles set version=version+1 where id=$1`, fixture.managerRole); err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.Exec(ctx, `update public.organization_authorization_revisions set revision=revision+1 where organization_id=$1`, fixture.organization); err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.Exec(ctx, `with account as (insert into public.users (email) values ('page-phantom@example.com') returning id) insert into public.organization_memberships (organization_id,user_id,role_id) select $1,id,$2 from account`, fixture.organization, fixture.role); err != nil {
			t.Fatal(err)
		}
		if err := transaction.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}}
	reader := managementReaderScenario(t, repository, 64)
	page, err := reader.Read(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), administration.ManagementQuery{Kind: identity.ResourceUser})
	if err != nil || page.Revision != 1 || page.TotalCount != 2 || len(page.Users) != 2 {
		t.Fatalf("mixed authorization/data snapshot: %+v %v", page, err)
	}
	current := managementReaderScenario(t, base, 64)
	refused, err := current.Read(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), administration.ManagementQuery{Kind: identity.ResourceUser})
	if !errors.Is(err, authz.ErrPermissionDenied) || refused.TotalCount != 0 || len(refused.Users) != 0 {
		t.Fatalf("revoked list authority: %+v %v", refused, err)
	}
}

func TestManagementReadRefusesBudgetForeignAndInactiveSnapshots(t *testing.T) {
	for _, scenario := range []string{"candidate overflow", "foreign member", "inactive revision", "missing catalog", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newBindingManagementFixture(t)
			ctx := context.Background()
			maxBindings := 64
			query := administration.ManagementQuery{Kind: identity.ResourceUser}
			expected := authz.ErrPermissionDenied
			switch scenario {
			case "candidate overflow":
				maxBindings = 1
				grant := fixture.bindingSchemaFixture
				grant.role = fixture.managerRole
				requireSchemaBinding(t, grant, "organization", nil, nil)
				expected = authz.ErrBindingLimitExceeded
			case "foreign member":
				query.ID = fixture.foreignUser
			case "inactive revision":
				if _, err := fixture.pool.Exec(ctx, `delete from public.organization_authorization_revisions where organization_id=$1`, fixture.organization); err != nil {
					t.Fatal(err)
				}
				expected = authz.ErrAuthorizationInactive
			case "missing catalog":
				if _, err := fixture.pool.Exec(ctx, `delete from public.role_permissions where permission_key='bindings.list'; delete from public.permissions where key='bindings.list'`); err != nil {
					t.Fatal(err)
				}
				expected = administration.ErrBindingManagementInactive
			case "canceled":
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
				expected = context.Canceled
			}
			reader := managementReaderScenario(t, pg.NewManagementReadStore(fixture.pool), maxBindings)
			page, err := reader.Read(ctx, identity.OrganizationID(fixture.organization), identity.UserID(fixture.user), query)
			if !errors.Is(err, expected) || len(page.Users) != 0 || page.TotalCount != 0 {
				t.Fatalf("refused query leaked page/count: %+v %v want %v", page, err, expected)
			}
		})
	}
}
