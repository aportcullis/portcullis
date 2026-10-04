package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

// recordAuditTarget writes one event in an organization and returns its stored id.
func recordAuditTarget(t *testing.T, store *pg.AuditStore, org identity.OrganizationID, target string) string {
	t.Helper()
	ctx := context.Background()
	if err := store.Record(ctx, audit.Event{
		OrganizationID: org, ActorType: audit.ActorSystem, ActorService: "test",
		Action: audit.ActionAuthLogin, TargetType: "user", TargetID: target, Outcome: audit.OutcomeSucceeded,
	}); err != nil {
		t.Fatalf("Record %s: %v", target, err)
	}
	page, err := store.List(ctx, org, audit.ListParams{Page: 1, PageSize: 100, SortDescending: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, event := range page.Events {
		if event.TargetID == target {
			return event.ID
		}
	}
	t.Fatalf("event %s is not listed in its own organization", target)
	return ""
}

func TestMutationEventsTakeTheMutationsOrganization(t *testing.T) {
	f := newConnFixture(t)
	ctx := context.Background()
	eventOrganization := func(target string) identity.OrganizationID {
		t.Helper()
		var org identity.OrganizationID
		if err := f.pool.QueryRow(ctx, `select organization_id::text from audit_events where target_id = $1`, target).Scan(&org); err != nil {
			t.Fatalf("event for %s: %v", target, err)
		}
		return org
	}

	unscoped := f.newConn(t, unique("Unscoped"))
	if err := f.store.Create(ctx, unscoped, sealedStub(1), connEvent(audit.ActionConnectionCreated, unscoped.ID)); err != nil {
		t.Fatalf("Create with an unscoped event: %v", err)
	}
	if got := eventOrganization(string(unscoped.ID)); got != f.org {
		t.Errorf("unscoped event stored in %s, want the connection's %s", got, f.org)
	}

	scoped := f.newConn(t, unique("Scoped"))
	matching := connEvent(audit.ActionConnectionCreated, scoped.ID)
	matching.OrganizationID = f.org
	if err := f.store.Create(ctx, scoped, sealedStub(1), matching); err != nil {
		t.Fatalf("Create with a matching event: %v", err)
	}
	if got := eventOrganization(string(scoped.ID)); got != f.org {
		t.Errorf("matching event stored in %s, want %s", got, f.org)
	}

	for _, foreign := range []identity.OrganizationID{identity.OrganizationID(uuid.NewString()), "not-a-uuid"} {
		forged := f.newConn(t, unique("Forged"))
		event := connEvent(audit.ActionConnectionCreated, forged.ID)
		event.OrganizationID = foreign
		if err := f.store.Create(ctx, forged, sealedStub(1), event); !errors.Is(err, audit.ErrOrganizationMismatch) {
			t.Errorf("Create with an event in %q = %v, want ErrOrganizationMismatch", foreign, err)
		}
		if _, err := f.store.GetByID(ctx, f.org, forged.ID); err == nil {
			t.Errorf("a refused cross-organization event left connection %s behind", forged.ID)
		}
	}
}

func TestAuditReadsAndWritesStayInTheCallersOrganization(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := pg.NewAuditStore(pool)
	defaultOrg, err := store.DefaultOrganizationID(ctx)
	if err != nil {
		t.Fatalf("DefaultOrganizationID: %v", err)
	}
	var orgB identity.OrganizationID
	if err := pool.QueryRow(ctx, `insert into organizations (slug, name) values ($1, 'Org B') returning id::text`, uuid.NewString()).Scan(&orgB); err != nil {
		t.Fatalf("create org B: %v", err)
	}

	defaultEvent := recordAuditTarget(t, store, defaultOrg, "default-target")
	orgBEvent := recordAuditTarget(t, store, orgB, "org-b-target")
	recordAuditTarget(t, store, orgB, "org-b-second")

	t.Run("org B lists only its own events", func(t *testing.T) {
		page, err := store.List(ctx, orgB, audit.ListParams{Page: 1, PageSize: 100, SortDescending: true})
		if err != nil {
			t.Fatalf("List org B: %v", err)
		}
		if page.TotalCount != 2 || len(page.Events) != 2 {
			t.Fatalf("org B page = %d events / total %d, want 2 / 2", len(page.Events), page.TotalCount)
		}
		for _, event := range page.Events {
			if event.OrganizationID != orgB || event.ID == defaultEvent {
				t.Errorf("org B listed %+v", event)
			}
		}
	})

	t.Run("the default organization does not see org B", func(t *testing.T) {
		page, err := store.List(ctx, defaultOrg, audit.ListParams{Page: 1, PageSize: 100, SortDescending: false})
		if err != nil {
			t.Fatalf("List default: %v", err)
		}
		if page.TotalCount != 1 || len(page.Events) != 1 || page.Events[0].ID != defaultEvent {
			t.Errorf("default page = %+v, want only %s", page.Events, defaultEvent)
		}
	})

	t.Run("detail reads resolve inside their own organization", func(t *testing.T) {
		got, err := store.Get(ctx, orgB, orgBEvent)
		if err != nil || got.ID != orgBEvent || got.OrganizationID != orgB {
			t.Errorf("Get org B event in org B = %+v, %v", got, err)
		}
		got, err = store.Get(ctx, defaultOrg, defaultEvent)
		if err != nil || got.OrganizationID != defaultOrg {
			t.Errorf("Get default event in default org = %+v, %v", got, err)
		}
	})

	t.Run("an unknown organization reads an empty trail", func(t *testing.T) {
		page, err := store.List(ctx, identity.OrganizationID(uuid.NewString()), audit.ListParams{Page: 3, PageSize: 10, SortDescending: true})
		if err != nil || page.TotalCount != 0 || len(page.Events) != 0 || page.Page != 1 {
			t.Errorf("unknown org page = %+v, %v; want empty page 1", page, err)
		}
	})

	t.Run("cross-organization detail ids are not found", func(t *testing.T) {
		if _, err := store.Get(ctx, defaultOrg, orgBEvent); !errors.Is(err, audit.ErrEventNotFound) {
			t.Errorf("Get org B event from default org = %v, want ErrEventNotFound", err)
		}
		if _, err := store.Get(ctx, orgB, defaultEvent); !errors.Is(err, audit.ErrEventNotFound) {
			t.Errorf("Get default event from org B = %v, want ErrEventNotFound", err)
		}
	})

	t.Run("an empty organization is refused instead of defaulted", func(t *testing.T) {
		var before int
		if err := pool.QueryRow(ctx, `select count(*) from audit_events`).Scan(&before); err != nil {
			t.Fatal(err)
		}
		if err := store.Record(ctx, audit.Event{ActorType: audit.ActorSystem, Action: audit.ActionAuthLogin, Outcome: audit.OutcomeFailed}); !errors.Is(err, audit.ErrOrganizationRequired) {
			t.Errorf("Record without organization = %v, want ErrOrganizationRequired", err)
		}
		var after int
		if err := pool.QueryRow(ctx, `select count(*) from audit_events`).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if after != before {
			t.Errorf("refused event wrote %d rows", after-before)
		}
		if _, err := store.List(ctx, "", audit.ListParams{Page: 1, PageSize: 10}); !errors.Is(err, audit.ErrOrganizationRequired) {
			t.Errorf("List without organization = %v, want ErrOrganizationRequired", err)
		}
		if _, err := store.Get(ctx, "", defaultEvent); !errors.Is(err, audit.ErrOrganizationRequired) {
			t.Errorf("Get without organization = %v, want ErrOrganizationRequired", err)
		}
	})
}
