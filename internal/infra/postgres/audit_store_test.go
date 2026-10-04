package postgres_test

import (
	"context"
	"crypto/sha256"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

func TestAuditListPageAndTotalComeFromOneSnapshot(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := pg.NewAuditStore(pool)
	org, err := store.DefaultOrganizationID(ctx)
	if err != nil {
		t.Fatalf("DefaultOrganizationID: %v", err)
	}

	for range 7 {
		if err := store.Record(ctx, audit.Event{
			OrganizationID: org,
			ActorType:      audit.ActorSystem, ActorService: "test", Action: audit.ActionAuthLogin,
			TargetType: "user", TargetID: "u", Outcome: audit.OutcomeSucceeded,
		}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	for _, tc := range []struct {
		name      string
		params    audit.ListParams
		wantItems int
		wantPage  int
		wantTotal int64
	}{
		{"first page", audit.ListParams{Page: 1, PageSize: 2, SortDescending: true}, 2, 1, 7},
		{"last partial page", audit.ListParams{Page: 4, PageSize: 2, SortDescending: true}, 1, 4, 7},
		{"far past the end clamps to the last page", audit.ListParams{Page: 9, PageSize: 2, SortDescending: true}, 1, 4, 7},
		{"one past the end clamps", audit.ListParams{Page: 5, PageSize: 2, SortDescending: false}, 1, 4, 7},
		{"whole set on one page", audit.ListParams{Page: 1, PageSize: 50, SortDescending: true}, 7, 1, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := store.List(ctx, org, tc.params)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(page.Events) != tc.wantItems || page.Page != tc.wantPage || page.TotalCount != tc.wantTotal {
				t.Errorf("page = %d events on page %d / total %d, want %d / %d / %d",
					len(page.Events), page.Page, page.TotalCount, tc.wantItems, tc.wantPage, tc.wantTotal)
			}
		})
	}

}

func TestAuditListOnAnEmptyTrail(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := pg.NewAuditStore(pool)
	org, err := store.DefaultOrganizationID(ctx)
	if err != nil {
		t.Fatalf("DefaultOrganizationID: %v", err)
	}
	page, err := store.List(ctx, org, audit.ListParams{Page: 5, PageSize: 10, SortDescending: true})
	if err != nil {
		t.Fatalf("List on an empty trail: %v", err)
	}
	if len(page.Events) != 0 || page.Page != 1 || page.TotalCount != 0 {
		t.Errorf("empty trail = %d events on page %d / total %d, want 0 / 1 / 0",
			len(page.Events), page.Page, page.TotalCount)
	}
}

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
	// An actor-less failure (unknown email) must also persist: no actor AND no target (A3) — this is the exact shape the auth service emits for a failed login with no resolved user, so the empty target_type is exercised against the real NOT NULL column, not just the in-memory fake.
	if err := store.Record(ctx, audit.Event{
		OrganizationID: org,
		ActorType:      audit.ActorUser,
		Action:         audit.ActionAuthLogin,
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

	var failedActor, failedTargetID *string
	var failedTargetType string
	if err := pool.QueryRow(ctx,
		`select actor_user_id::text, target_type, target_id from audit_events where outcome = 'FAILED'`,
	).Scan(&failedActor, &failedTargetType, &failedTargetID); err != nil {
		t.Fatalf("query actor-less event: %v", err)
	}
	if failedActor != nil {
		t.Errorf("actor-less event stored actor %v, want NULL", *failedActor)
	}

	if failedTargetType != "" {
		t.Errorf("actor-less event target_type = %q, want empty", failedTargetType)
	}
	if failedTargetID != nil {
		t.Errorf("actor-less event target_id = %v, want NULL", *failedTargetID)
	}

	// Append-only: the trigger must reject mutation.
	if _, err := pool.Exec(ctx, `update audit_events set outcome = 'FAILED' where outcome = 'SUCCEEDED'`); err == nil {
		t.Error("UPDATE on audit_events succeeded, want append-only rejection")
	}
}

func TestAuditStoreOccurredAtDefaultAndExplicit(t *testing.T) {
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
	store := pg.NewAuditStore(pool)

	// The derived shape: an instant the CALLER supplies (as the TTL observation does, from the DB's own clock) must be stored verbatim, to microsecond precision — the timestamptz column's resolution.
	var observed time.Time
	if err := pool.QueryRow(ctx, `select clock_timestamp() - interval '90 seconds'`).Scan(&observed); err != nil {
		t.Fatalf("db clock: %v", err)
	}
	observed = observed.UTC().Truncate(time.Microsecond)
	derived := audit.Event{
		OrganizationID: org, ActorType: audit.ActorSystem, ActorService: "ttl_expiry",
		Action: audit.ActionAccessRequestExpired, TargetType: audit.TargetTypeAccessRequest,
		TargetID: "11111111-2222-3333-4444-555555555555", Outcome: audit.OutcomeSucceeded,
		OccurredAt: observed,
	}
	if err := store.Record(ctx, derived); err != nil {
		t.Fatalf("Record derived: %v", err)
	}

	// The ordinary shape: no instant supplied, so the DB stamps it. The window is measured with the DB's OWN clock — comparing against the host's would make the assertion hostage to container clock skew.
	dbNow := func() time.Time {
		t.Helper()
		var at time.Time
		if err := pool.QueryRow(ctx, `select clock_timestamp()`).Scan(&at); err != nil {
			t.Fatalf("db clock: %v", err)
		}
		return at.UTC()
	}
	before := dbNow()
	ordinary := audit.Event{
		OrganizationID: org, ActorType: audit.ActorSystem, ActorService: "test",
		Action: audit.ActionAccessRequestCancelled, TargetType: audit.TargetTypeAccessRequest,
		TargetID: "22222222-3333-4444-5555-666666666666", Outcome: audit.OutcomeSucceeded,
	}
	if err := store.Record(ctx, ordinary); err != nil {
		t.Fatalf("Record ordinary: %v", err)
	}

	read := func(target string) time.Time {
		t.Helper()
		var at time.Time
		if err := pool.QueryRow(ctx, `select occurred_at from audit_events where target_id = $1`, target).Scan(&at); err != nil {
			t.Fatalf("read %s: %v", target, err)
		}
		return at.UTC()
	}
	if got := read(derived.TargetID); !got.Equal(observed) {
		t.Errorf("derived occurred_at = %s, want the supplied %s", got, observed)
	}
	if got := read(ordinary.TargetID); got.Before(before) || got.After(dbNow()) {
		t.Errorf("ordinary occurred_at = %s, want between %s and now on the DB clock", got, before)
	}
	// And the two must be ordered as their instants were, so a mixed timeline reads chronologically rather than by insert order.
	if !read(derived.TargetID).Before(read(ordinary.TargetID)) {
		t.Error("the derived event must sort before the later ordinary one")
	}
}

func TestAuditStoreIgnoresTemporaryAuditShadow(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	org, err := pg.NewIdentityStore(pool).DefaultOrganizationID(ctx)
	if err != nil {
		t.Fatalf("organization: %v", err)
	}

	cfg := pool.Config().Copy()
	cfg.MaxConns = 1
	one, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("single-connection pool: %v", err)
	}
	t.Cleanup(one.Close)
	if _, err := one.Exec(ctx, `create temporary table audit_events (like public.audit_events including defaults)`); err != nil {
		t.Fatalf("create shadow: %v", err)
	}
	if err := pg.NewAuditStore(one).Record(ctx, audit.Event{
		OrganizationID: org,
		ActorType:      audit.ActorUser,
		Action:         audit.ActionAuthLogin,
		TargetType:     audit.TargetTypeUser,
		Outcome:        audit.OutcomeFailed,
		RequestID:      "qualified-write",
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	var permanent, shadow int
	if err := pool.QueryRow(ctx, `select count(*) from public.audit_events where request_id = 'qualified-write'`).Scan(&permanent); err != nil {
		t.Fatalf("permanent count: %v", err)
	}
	if err := one.QueryRow(ctx, `select count(*) from pg_temp.audit_events`).Scan(&shadow); err != nil {
		t.Fatalf("shadow count: %v", err)
	}
	if permanent != 1 || shadow != 0 {
		t.Errorf("audit destination permanent=%d shadow=%d, want 1/0", permanent, shadow)
	}
}

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

	// A rotation whose event cannot be mapped must roll back the WHOLE tx: no new session, no event.
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

func TestAuditStoreRefusesHalfSetDigest(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	org, err := pg.NewIdentityStore(pool).DefaultOrganizationID(ctx)
	if err != nil {
		t.Fatalf("DefaultOrganizationID: %v", err)
	}
	store := pg.NewAuditStore(pool)

	base := func() audit.Event {
		return audit.Event{
			OrganizationID: org,
			ActorType:      audit.ActorSystem,
			Action:         audit.ActionAccessRequestExpired,
			TargetType:     audit.TargetTypeAccessRequest,
			Outcome:        audit.OutcomeSucceeded,
		}
	}

	digestOnly := base()
	digestOnly.PayloadDigest = []byte("digest-without-a-version")
	if err := store.Record(ctx, digestOnly); err == nil {
		t.Error("a digest without its key version must be refused, not silently dropped")
	}

	versionOnly := base()
	versionOnly.PayloadDigestKeyVersion = 7
	if err := store.Record(ctx, versionOnly); err == nil {
		t.Error("a key version without its digest must be refused, not silently dropped")
	}

	var n int
	if err := pool.QueryRow(ctx, `select count(*) from audit_events`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("refused events wrote %d rows, want 0", n)
	}
}
