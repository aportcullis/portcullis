package postgres_test

import (
	"context"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

// Scenario: recorded events are read back with Count + a page, in both directions,
// with the source IP lifted back out of the metadata JSONB and request_id restored
// (the read shape mirrors the write shape).
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
	// Insert oldest→newest; occurred_at defaults to now(), so sequential inserts are
	// monotonic and the newest lands first under (occurred_at desc, id desc).
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
	// A fully-populated event last, so it is the newest and carries the fields we
	// round-trip: actor, request id, source IP (folded into metadata on write), and
	// remaining metadata.
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

	total, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if total != 4 {
		t.Fatalf("Count = %d, want 4", total)
	}

	desc, err := store.List(ctx, audit.ListParams{Limit: 100, Offset: 0, SortDescending: true})
	if err != nil {
		t.Fatalf("List desc: %v", err)
	}
	if len(desc) != 4 {
		t.Fatalf("List desc returned %d events, want 4", len(desc))
	}

	// Newest first: the fully-populated logout was inserted last, so it heads the
	// desc list, and the slice is non-increasing by occurred_at.
	newest := desc[0]
	if newest.Action != audit.ActionAuthLogout {
		t.Errorf("newest action = %q, want AUTH_LOGOUT", newest.Action)
	}
	for i := 1; i < len(desc); i++ {
		if desc[i].OccurredAt.After(desc[i-1].OccurredAt) {
			t.Fatalf("desc events not ordered newest-first at index %d", i)
		}
	}

	// Field round-trip: source IP lifted out of metadata, remaining metadata kept,
	// request id and actor restored, store-assigned id/occurred_at present.
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

	// Ascending: same rows, oldest first — the populated logout is now last.
	asc, err := store.List(ctx, audit.ListParams{Limit: 100, Offset: 0, SortDescending: false})
	if err != nil {
		t.Fatalf("List asc: %v", err)
	}
	if len(asc) != 4 || asc[len(asc)-1].Action != audit.ActionAuthLogout {
		t.Errorf("asc order wrong: newest (logout) should be last, got last=%q", asc[len(asc)-1].Action)
	}
	for i := 1; i < len(asc); i++ {
		if asc[i].OccurredAt.Before(asc[i-1].OccurredAt) {
			t.Fatalf("asc events not ordered oldest-first at index %d", i)
		}
	}

	// OFFSET pagination: two pages of 2, no overlap.
	page1, err := store.List(ctx, audit.ListParams{Limit: 2, Offset: 0, SortDescending: true})
	if err != nil {
		t.Fatalf("List page1: %v", err)
	}
	page2, err := store.List(ctx, audit.ListParams{Limit: 2, Offset: 2, SortDescending: true})
	if err != nil {
		t.Fatalf("List page2: %v", err)
	}
	if len(page1) != 2 || len(page2) != 2 {
		t.Fatalf("page sizes = %d/%d, want 2/2", len(page1), len(page2))
	}
	if page1[0].ID == page2[0].ID {
		t.Error("offset pagination returned overlapping rows")
	}
}
