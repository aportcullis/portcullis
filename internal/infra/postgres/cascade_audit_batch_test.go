package postgres_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

// auditRoundTripCounter counts single-statement audit inserts and batch sends on a pool.
type auditRoundTripCounter struct {
	singleInserts atomic.Int64
	batches       atomic.Int64
}

func (c *auditRoundTripCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "insert into public.audit_events") {
		c.singleInserts.Add(1)
	}
	return ctx
}

func (*auditRoundTripCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (c *auditRoundTripCounter) TraceBatchStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceBatchStartData) context.Context {
	c.batches.Add(1)
	return ctx
}

func (*auditRoundTripCounter) TraceBatchQuery(context.Context, *pgx.Conn, pgx.TraceBatchQueryData) {}

func (*auditRoundTripCounter) TraceBatchEnd(context.Context, *pgx.Conn, pgx.TraceBatchEndData) {}

func (c *auditRoundTripCounter) reset() {
	c.singleInserts.Store(0)
	c.batches.Store(0)
}

func countedPool(t *testing.T, base *pgxpool.Pool, counter *auditRoundTripCounter) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(base.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// liveRequests creates drafts, pending and approved requests on a connection.
func (f reqFixture) liveRequests(t *testing.T, connID connection.ConnectionID, drafts, pending int) []access.RequestID {
	t.Helper()
	ids := make([]access.RequestID, 0, drafts+pending)
	for range drafts {
		ids = append(ids, f.draft(t, connID).ID)
	}
	for range pending {
		ids = append(ids, f.submitted(t, connID, 1).ID)
	}
	return ids
}

func TestCascadeAuditEventsAreWrittenInBatches(t *testing.T) {
	owner := newReqFixtureFresh(t)
	counter := &auditRoundTripCounter{}
	counted := countedPool(t, owner.pool, counter)
	f := reqFixtureOver(t, connFixture{pool: counted, store: pg.NewConnectionStore(counted), org: owner.org, user: owner.user})
	ctx := context.Background()
	countEvents := func(t *testing.T, ids []access.RequestID, action audit.Action) int {
		t.Helper()
		total := 0
		for _, id := range ids {
			total += f.countEvents(t, action, id)
		}
		return total
	}

	for _, tc := range []struct {
		name            string
		drafts, pending int
	}{
		{"one pending request", 0, 1},
		{"a draft and five pending requests", 1, 5},
		{"three drafts and twelve pending requests", 3, 12},
	} {
		t.Run("archive with "+tc.name, func(t *testing.T) {
			connID := f.liveConn(t, 1)
			ids := f.liveRequests(t, connID, tc.drafts, tc.pending)
			counter.reset()
			if _, err := f.store.Archive(ctx, f.org, connID, connEvent(audit.ActionConnectionArchived, connID)); err != nil {
				t.Fatalf("Archive: %v", err)
			}
			if got := counter.singleInserts.Load(); got != 0 {
				t.Errorf("archive cascade issued %d single audit inserts, want them batched", got)
			}
			if got := counter.batches.Load(); got > 2 {
				t.Errorf("archive cascade sent %d batches, want at most 2 regardless of request count", got)
			}
			if got := countEvents(t, ids, audit.ActionAccessRequestCancelled); got != tc.drafts {
				t.Errorf("CANCELLED events = %d, want %d", got, tc.drafts)
			}
			if got := countEvents(t, ids, audit.ActionAccessRequestExpired); got != tc.pending {
				t.Errorf("EXPIRED events = %d, want %d", got, tc.pending)
			}
		})
	}

	t.Run("policy change with four pending requests", func(t *testing.T) {
		connID := f.liveConn(t, 1)
		ids := f.liveRequests(t, connID, 0, 4)
		counter.reset()
		f.liveConnPolicyBump(t, connID)
		if got := counter.singleInserts.Load(); got != 0 {
			t.Errorf("policy cascade issued %d single audit inserts", got)
		}
		if got := countEvents(t, ids, audit.ActionAccessRequestExpired); got != 4 {
			t.Errorf("EXPIRED events = %d, want 4", got)
		}
	})

	t.Run("a foreign-organization event rolls the whole cascade back", func(t *testing.T) {
		connID := f.liveConn(t, 1)
		ids := f.liveRequests(t, connID, 1, 3)
		forged := connEvent(audit.ActionConnectionArchived, connID)
		forged.OrganizationID = identity.OrganizationID(uuid.NewString())
		if _, err := f.store.Archive(ctx, f.org, connID, forged); !errors.Is(err, audit.ErrOrganizationMismatch) {
			t.Fatalf("Archive with a foreign event = %v, want ErrOrganizationMismatch", err)
		}
		if got := countEvents(t, ids, audit.ActionAccessRequestExpired) + countEvents(t, ids, audit.ActionAccessRequestCancelled); got != 0 {
			t.Errorf("a refused archive left %d cascade events", got)
		}
		for _, id := range ids {
			got, _, err := f.requests.GetSealed(ctx, f.org, id)
			if err != nil || got.State.Terminal() {
				t.Errorf("request %s after a refused archive = %s, %v; want it still live", id, got.State, err)
			}
		}
	})

	t.Run("an invalid event in the batch rolls the cascade back", func(t *testing.T) {
		connID := f.liveConn(t, 1)
		ids := f.liveRequests(t, connID, 0, 2)
		broken := connEvent(audit.ActionConnectionArchived, connID)
		broken.PayloadDigest = []byte("digest-without-a-key-version")
		if _, err := f.store.Archive(ctx, f.org, connID, broken); err == nil {
			t.Fatal("Archive with a half-set digest event succeeded")
		}
		if got := countEvents(t, ids, audit.ActionAccessRequestExpired); got != 0 {
			t.Errorf("a refused archive left %d expiry events", got)
		}
		if _, err := f.store.GetByID(ctx, f.org, connID); err != nil {
			t.Fatal(err)
		}
	})
}
