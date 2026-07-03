package postgres_test

import (
	"context"
	"crypto/sha256"
	"strings"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

func TestAuditStoreRecordsAppendOnlyEvent(t *testing.T) {
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

	// Scenario: a login event with an actor, request id, and source IP is
	// persisted with every field intact, and the row can never be mutated.
	if err := store.Record(ctx, audit.Event{
		OrganizationID: org,
		ActorType:      audit.ActorUser,
		ActorUserID:    &u.ID,
		Action:         audit.ActionAuthLogin,
		TargetType:     audit.TargetTypeUser,
		TargetID:       string(u.ID),
		Outcome:        audit.OutcomeSucceeded,
		RequestID:      "req-123",
		SourceIP:       "9.9.9.9",
		Metadata:       map[string]any{"note": "e2e"},
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	// An actor-less failure (unknown email) must also persist (nullable actor).
	if err := store.Record(ctx, audit.Event{
		OrganizationID: org,
		ActorType:      audit.ActorUser,
		Action:         audit.ActionAuthLogin,
		TargetType:     audit.TargetTypeUser,
		Outcome:        audit.OutcomeFailed,
	}); err != nil {
		t.Fatalf("Record (no actor): %v", err)
	}

	var action, outcome, actorID, requestID, metadata string
	if err := pool.QueryRow(ctx, `
		select action, outcome, coalesce(actor_user_id::text, ''), coalesce(request_id, ''), metadata::text
		from audit_events where outcome = 'SUCCEEDED'`,
	).Scan(&action, &outcome, &actorID, &requestID, &metadata); err != nil {
		t.Fatalf("query event: %v", err)
	}
	if action != string(audit.ActionAuthLogin) || actorID != string(u.ID) || requestID != "req-123" {
		t.Errorf("row = %s/%s actor=%s req=%s", action, outcome, actorID, requestID)
	}
	if !strings.Contains(metadata, "9.9.9.9") || !strings.Contains(metadata, "e2e") {
		t.Errorf("metadata = %s, want source_ip and note", metadata)
	}

	var failedActor *string
	if err := pool.QueryRow(ctx,
		`select actor_user_id::text from audit_events where outcome = 'FAILED'`,
	).Scan(&failedActor); err != nil {
		t.Fatalf("query actor-less event: %v", err)
	}
	if failedActor != nil {
		t.Errorf("actor-less event stored actor %v, want NULL", *failedActor)
	}

	// Append-only: the trigger must reject mutation.
	if _, err := pool.Exec(ctx, `update audit_events set outcome = 'FAILED' where outcome = 'SUCCEEDED'`); err == nil {
		t.Error("UPDATE on audit_events succeeded, want append-only rejection")
	}
}

// Scenario (ADR-0009): the audit event of a state-changing op commits in the
// SAME transaction — if the event cannot be written, the state change must roll
// back, and the happy path leaves exactly one event per op.
func TestStateChangingOpsCommitAuditAtomically(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := pg.NewIdentityStore(pool)

	countRows := func(table string) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, `select count(*) from `+table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}

	// Bootstrap commits user + membership + one AUTH_BOOTSTRAP event atomically,
	// with the actor completed inside the transaction.
	u, err := store.BootstrapAdmin(ctx, "admin@example.com", "Admin", "phc-hash",
		audit.Event{ActorType: audit.ActorUser, Action: audit.ActionAuthBootstrap, TargetType: audit.TargetTypeUser, Outcome: audit.OutcomeSucceeded, RequestID: "req-boot"})
	if err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	var bootActor string
	if err := pool.QueryRow(ctx,
		`select actor_user_id::text from audit_events where action = 'AUTH_BOOTSTRAP'`,
	).Scan(&bootActor); err != nil {
		t.Fatalf("bootstrap event: %v", err)
	}
	if bootActor != string(u.ID) {
		t.Errorf("bootstrap event actor = %s, want %s (completed in-tx)", bootActor, u.ID)
	}

	// A rotation whose event cannot be mapped must roll back the WHOLE tx: no new
	// session, no event.
	sessionsBefore, eventsBefore := countRows("sessions"), countRows("audit_events")
	badActor := identity.UserID("not-a-uuid")
	h := sha256.Sum256([]byte("tok-bad"))
	sess := identity.NewSession("", u.ID, time.Now(), time.Hour, 2*time.Hour)
	if _, err := store.RotateSession(ctx, u.ID, sess, h[:],
		audit.Event{ActorType: audit.ActorUser, ActorUserID: &badActor, Action: audit.ActionAuthLogin, TargetType: audit.TargetTypeUser, Outcome: audit.OutcomeSucceeded}); err == nil {
		t.Fatal("RotateSession with an unmappable audit event should fail")
	}
	if countRows("sessions") != sessionsBefore || countRows("audit_events") != eventsBefore {
		t.Error("failed audit write must roll back the whole rotation (atomicity)")
	}

	// Happy-path rotate and revoke leave exactly one event each.
	h2 := sha256.Sum256([]byte("tok-good"))
	created, err := store.RotateSession(ctx, u.ID, sess, h2[:],
		audit.Event{ActorType: audit.ActorUser, ActorUserID: &u.ID, Action: audit.ActionAuthLogin, TargetType: audit.TargetTypeUser, Outcome: audit.OutcomeSucceeded})
	if err != nil {
		t.Fatalf("RotateSession: %v", err)
	}
	if err := store.RevokeSession(ctx, created.ID,
		audit.Event{ActorType: audit.ActorUser, ActorUserID: &u.ID, Action: audit.ActionAuthLogout, TargetType: audit.TargetTypeUser, Outcome: audit.OutcomeSucceeded}); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	for action, want := range map[string]int{"AUTH_BOOTSTRAP": 1, "AUTH_LOGIN": 1, "AUTH_LOGOUT": 1} {
		var n int
		if err := pool.QueryRow(ctx, `select count(*) from audit_events where action = $1`, action).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", action, err)
		}
		if n != want {
			t.Errorf("%s events = %d, want %d", action, n, want)
		}
	}
}
