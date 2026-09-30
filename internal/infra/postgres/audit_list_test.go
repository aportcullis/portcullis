package postgres_test

import (
	"context"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

func TestAuditStoreListReadsBackEvents(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	ids := pg.NewIdentityStore(pool)
	org, err := ids.DefaultOrganizationID(ctx)
	if err != nil {
		t.Fatalf("DefaultOrganizationID: %v", err)
	}
	u, err := ids.CreateUser(ctx, "audit@example.com", "Audit")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	store := pg.NewAuditStore(pool)

	for i := 0; i < 3; i++ {
		if err := store.Record(ctx, audit.Event{
			OrganizationID: org,
			ActorType:      audit.ActorUser,
			Action:         audit.ActionAuthLogin,
			Outcome:        audit.OutcomeFailed,
		}); err != nil {
			t.Fatalf("Record #%d: %v", i, err)
		}
	}

	if err := store.Record(ctx, audit.Event{
		OrganizationID: org,
		ActorType:      audit.ActorUser,
		ActorUserID:    &u.ID,
		Action:         audit.ActionAuthLogout,
		TargetType:     audit.TargetTypeUser,
		TargetID:       string(u.ID),
		Outcome:        audit.OutcomeSucceeded,
		RequestID:      "req-xyz",
		SourceIP:       "9.9.9.9",
		Metadata:       map[string]any{"note": "hello"},
	}); err != nil {
		t.Fatalf("Record populated: %v", err)
	}

	page, err := store.List(ctx, audit.ListParams{Page: 1, PageSize: 100, SortDescending: true})
	if err != nil {
		t.Fatalf("List desc: %v", err)
	}
	if page.TotalCount != 4 {
		t.Fatalf("total = %d, want 4", page.TotalCount)
	}
	desc := page.Events
	if len(desc) != 4 {
		t.Fatalf("List desc returned %d events, want 4", len(desc))
	}

	newest := desc[0]
	if newest.Action != audit.ActionAuthLogout {
		t.Errorf("newest action = %q, want AUTH_LOGOUT", newest.Action)
	}
	for idx := 1; idx < len(desc); idx++ {
		if desc[idx].OccurredAt.After(desc[idx-1].OccurredAt) {
			t.Fatalf("desc events not ordered newest-first at index %d", idx)
		}
	}

	if newest.SourceIP != "9.9.9.9" {
		t.Errorf("SourceIP = %q, want 9.9.9.9", newest.SourceIP)
	}
	if _, leaked := newest.Metadata["source_ip"]; leaked {
		t.Error("source_ip must be lifted out of Metadata, not left in it")
	}
	if newest.Metadata["note"] != "hello" {
		t.Errorf("Metadata[note] = %v, want hello", newest.Metadata["note"])
	}
	if newest.RequestID != "req-xyz" {
		t.Errorf("RequestID = %q, want req-xyz", newest.RequestID)
	}
	if newest.ActorUserID == nil || *newest.ActorUserID != u.ID {
		t.Errorf("ActorUserID = %v, want %s", newest.ActorUserID, u.ID)
	}
	if newest.ID == "" || newest.OccurredAt.IsZero() {
		t.Errorf("read event missing store-assigned ID/OccurredAt: id=%q ts=%v", newest.ID, newest.OccurredAt)
	}

	ascending, err := store.List(ctx, audit.ListParams{Page: 1, PageSize: 100, SortDescending: false})
	if err != nil {
		t.Fatalf("List asc: %v", err)
	}
	asc := ascending.Events
	if len(asc) != 4 || asc[len(asc)-1].Action != audit.ActionAuthLogout {
		t.Errorf("asc order wrong: newest (logout) should be last, got last=%q", asc[len(asc)-1].Action)
	}
	for idx := 1; idx < len(asc); idx++ {
		if asc[idx].OccurredAt.Before(asc[idx-1].OccurredAt) {
			t.Fatalf("asc events not ordered oldest-first at index %d", idx)
		}
	}

	page1, err := store.List(ctx, audit.ListParams{Page: 1, PageSize: 2, SortDescending: true})
	if err != nil {
		t.Fatalf("List page1: %v", err)
	}
	page2, err := store.List(ctx, audit.ListParams{Page: 2, PageSize: 2, SortDescending: true})
	if err != nil {
		t.Fatalf("List page2: %v", err)
	}
	if len(page1.Events) != 2 || len(page2.Events) != 2 {
		t.Fatalf("page sizes = %d/%d, want 2/2", len(page1.Events), len(page2.Events))
	}
	if page1.Events[0].ID == page2.Events[0].ID {
		t.Error("offset pagination returned overlapping rows")
	}
}
