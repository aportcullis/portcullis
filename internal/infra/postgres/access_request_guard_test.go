package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

// runtimeRolePool opens a pool whose sessions act as the runtime role, the principal the request guard bounds.
func runtimeRolePool(t *testing.T, owner *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(owner.Config().ConnString())
	if err != nil {
		t.Fatalf("parse pool config: %v", err)
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, `set role portcullis_runtime`)
		return err
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open runtime pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// requireGuardRefusal asserts the request guard refused a runtime write with insufficient_privilege.
func requireGuardRefusal(t *testing.T, err error, attempt string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Errorf("%s = %v, want the request guard to refuse with 42501", attempt, err)
	}
}

func TestAccessRequestGuardAllowsStorePathsAsRuntime(t *testing.T) {
	owner := newReqFixtureFresh(t)
	runtime := runtimeRolePool(t, owner.pool)
	f := reqFixtureOver(t, connFixture{pool: runtime, store: pg.NewConnectionStore(runtime), org: owner.org, user: owner.user})
	ctx := context.Background()

	t.Run("draft edit, submit, quorum approval, execution and completion", func(t *testing.T) {
		connID := f.liveConn(t, 1)
		draft := f.draft(t, connID)
		edited, err := f.requests.UpdateDraft(ctx, draft, sealedPayloadStub(), 1, reqEvent(audit.ActionAccessRequestUpdated, draft.ID))
		if err != nil {
			t.Fatalf("UpdateDraft as runtime: %v", err)
		}
		submitted, err := edited.Request.Submitted(submitSnapshot(f.pin(t, connID), 1), time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.requests.Submit(ctx, submitted, 2, time.Hour, reqEvent(audit.ActionAccessRequestSubmitted, draft.ID)); err != nil {
			t.Fatalf("Submit as runtime: %v", err)
		}
		approver := f.userWithRole(t, "approver")
		view, err := f.requests.Approve(ctx, f.org, draft.ID, approver, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, draft.ID))
		if err != nil || view.Request.State != access.StateApproved {
			t.Fatalf("Approve as runtime = %+v, %v", view.Request, err)
		}
		lease, err := f.requests.AcquireExecution(ctx, f.org, draft.ID, f.requester, "server-one", uuid.NewString(), view.Request.Digest, reqEvent(audit.ActionExecutionStarted, draft.ID))
		if err != nil {
			t.Fatalf("AcquireExecution as runtime: %v", err)
		}
		if err := f.requests.CompleteExecution(ctx, f.org, lease, access.ExecutionCompletion{State: access.StateSucceeded}, reqEvent(audit.ActionExecutionFinished, draft.ID)); err != nil {
			t.Fatalf("CompleteExecution as runtime: %v", err)
		}
	})

	t.Run("requester cancels a draft and an approver rejects a pending request", func(t *testing.T) {
		connID := f.liveConn(t, 1)
		draft := f.draft(t, connID)
		if view, err := f.requests.Cancel(ctx, f.org, draft.ID, f.requester, reqEvent(audit.ActionAccessRequestCancelled, draft.ID)); err != nil || view.Request.State != access.StateCancelled {
			t.Fatalf("Cancel draft as runtime = %+v, %v", view.Request, err)
		}
		pending := f.submitted(t, connID, 1)
		rejecter := f.userWithRole(t, "approver")
		if view, err := f.requests.Reject(ctx, f.org, pending.ID, rejecter, "wrong table", reqEvent(audit.ActionAccessRequestRejected, pending.ID)); err != nil || view.Request.State != access.StateRejected {
			t.Fatalf("Reject as runtime = %+v, %v", view.Request, err)
		}
	})

	t.Run("auto-approval then requester cancellation of the approved request", func(t *testing.T) {
		approved := approvedExecutionRequest(t, f)
		if view, err := f.requests.Cancel(ctx, f.org, approved.ID, f.requester, reqEvent(audit.ActionAccessRequestCancelled, approved.ID)); err != nil || view.Request.State != access.StateCancelled {
			t.Fatalf("Cancel approved as runtime = %+v, %v", view.Request, err)
		}
	})

	t.Run("policy cascade expires live requests and key rotation rewraps terminal envelopes", func(t *testing.T) {
		connID := f.liveConn(t, 1)
		pending := f.submitted(t, connID, 1)
		f.liveConnPolicyBump(t, connID)
		got, _, err := f.requests.GetSealed(ctx, f.org, pending.ID)
		if err != nil || got.State != access.StateExpired {
			t.Fatalf("pending after policy change = %+v, %v", got, err)
		}
		if _, err := runtime.Exec(ctx,
			`update public.access_requests set payload_key_version = payload_key_version + 1, payload_wrapped_dek = 'rewrapped', payload_nonce = 'nonce-rotated', payload_ciphertext = 'ciphertext-rotated' where id = $1`,
			string(pending.ID)); err != nil {
			t.Fatalf("rotation rewrap of a terminal envelope as runtime: %v", err)
		}
	})
}

// liveConnPolicyBump publishes the next policy version for a connection, cascading its live requests.
func (f reqFixture) liveConnPolicyBump(t *testing.T, connID connection.ConnectionID) {
	t.Helper()
	ctx := context.Background()
	cur, err := f.policies.GetCurrent(ctx, f.org, connID)
	if err != nil {
		t.Fatalf("GetCurrent: %v", err)
	}
	next := validNextPolicy(t, cur, f.user)
	if _, err := f.policies.UpdatePolicy(ctx, next, cur.Version, connEvent(audit.ActionConnectionPolicyUpdated, connID)); err != nil {
		t.Fatalf("UpdatePolicy: %v", err)
	}
}

func TestAccessRequestGuardEnforcesTheDomainGraphAsRuntime(t *testing.T) {
	// A fresh database keeps the matrix's rows out of the shared organization's list pages.
	f := newReqFixtureFresh(t)
	runtime := runtimeRolePool(t, f.pool)
	ctx := context.Background()
	connID := f.liveConn(t, 1)

	for _, from := range access.States() {
		for _, to := range access.States() {
			if from == access.StateDraft {
				continue
			}
			// The owner places a submitted row in the source state; the guard exempts principals that could disable it anyway.
			row := f.submitted(t, connID, 1)
			if _, err := f.pool.Exec(ctx, `update public.access_requests set state = $2, expires_at = case when $2 in ('approved', 'executing', 'succeeded', 'failed', 'outcome_unknown') then clock_timestamp() + interval '1 hour' end where id = $1`, string(row.ID), string(from)); err != nil {
				t.Fatalf("owner places %s: %v", from, err)
			}
			_, err := runtime.Exec(ctx, `update public.access_requests set state = $2, version = version + 1, updated_at = clock_timestamp() where id = $1`, string(row.ID), string(to))
			allowed := from.CanTransitionTo(to) || (from == to && !from.Terminal())
			if allowed && err != nil {
				t.Errorf("runtime %s → %s = %v, want allowed by the domain graph", from, to, err)
			}
			if !allowed {
				requireGuardRefusal(t, err, "runtime "+string(from)+" → "+string(to))
			}
		}
	}

	draft := f.draft(t, connID)
	for _, to := range []access.State{access.StateExecuting, access.StateSucceeded, access.StateRejected, access.StateExpired, access.StateFailed, access.StateOutcomeUnknown} {
		_, err := runtime.Exec(ctx, `update public.access_requests set state = $2 where id = $1`, string(draft.ID), string(to))
		requireGuardRefusal(t, err, "runtime draft → "+string(to))
	}
	if _, err := runtime.Exec(ctx, `update public.access_requests set state = 'cancelled' where id = $1`, string(draft.ID)); err != nil {
		t.Errorf("runtime draft → cancelled = %v, want allowed", err)
	}
}

func TestAccessRequestGuardFreezesEvidenceAsRuntime(t *testing.T) {
	f := newReqFixtureFresh(t)
	runtime := runtimeRolePool(t, f.pool)
	ctx := context.Background()
	connID := f.liveConn(t, 1)
	pending := f.submitted(t, connID, 1)
	approved := approvedExecutionRequest(t, f)
	other := f.userWithRole(t, "requester")

	for _, forged := range []struct {
		name, sql string
		target    access.RequestID
	}{
		{"rewrite the redacted SQL approvers saw", `update public.access_requests set redacted_sql = 'drop table users' where id = $1`, pending.ID},
		{"swap the payload digest", `update public.access_requests set payload_digest = 'forged' where id = $1`, pending.ID},
		{"lower the pinned quorum", `update public.access_requests set required_approvals = 0 where id = $1`, pending.ID},
		{"repin the policy version", `update public.access_requests set policy_version = policy_version + 1 where id = $1`, pending.ID},
		{"repoint the connection fingerprint", `update public.access_requests set connection_fingerprint = 'elsewhere' where id = $1`, approved.ID},
		{"retitle a submitted request", `update public.access_requests set title = 'innocent' where id = $1`, pending.ID},
		{"redate the submission", `update public.access_requests set submitted_at = submitted_at - interval '1 day' where id = $1`, pending.ID},
		{"extend an approval window", `update public.access_requests set expires_at = expires_at + interval '30 days' where id = $1`, approved.ID},
		{"invent an expiry on a pending request", `update public.access_requests set expires_at = clock_timestamp() + interval '1 day' where id = $1`, pending.ID},
		{"attach a reason without a transition", `update public.access_requests set state_reason = 'ttl_expired' where id = $1`, pending.ID},
	} {
		_, err := runtime.Exec(ctx, forged.sql, string(forged.target))
		requireGuardRefusal(t, err, forged.name)
	}

	for _, forged := range []struct{ name, sql string }{
		{"reassign the requester", `update public.access_requests set requester_id = '` + string(other) + `' where id = $1`},
		{"move the request to another organization", `update public.access_requests set organization_id = gen_random_uuid() where id = $1`},
		{"redate the creation", `update public.access_requests set created_at = created_at - interval '1 day' where id = $1`},
	} {
		draft := f.draft(t, connID)
		_, err := runtime.Exec(ctx, forged.sql, string(draft.ID))
		requireGuardRefusal(t, err, forged.name)
	}

	smuggled := f.draft(t, connID)
	_, err := runtime.Exec(ctx, `
		update public.access_requests d
		set (payload_digest, payload_digest_key_version, redacted_sql, statement_class, policy_version,
		     required_approvals, connection_config_version, connection_fingerprint, connection_display_name,
		     connection_db_type, submitted_at)
		  = (select s.payload_digest, s.payload_digest_key_version, s.redacted_sql, s.statement_class, s.policy_version,
		            s.required_approvals, s.connection_config_version, s.connection_fingerprint, s.connection_display_name,
		            s.connection_db_type, s.submitted_at
		     from public.access_requests s where s.id = $2)
		where d.id = $1`, string(smuggled.ID), string(pending.ID))
	requireGuardRefusal(t, err, "stamp a submit snapshot on a draft without submitting")

	if _, err := f.requests.Cancel(ctx, f.org, pending.ID, f.requester, reqEvent(audit.ActionAccessRequestCancelled, pending.ID)); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	for _, forged := range []struct{ name, sql string }{
		{"attach a system reason to a requester cancel", `update public.access_requests set state_reason = 'policy_changed' where id = $1`},
		{"redate a terminal row", `update public.access_requests set updated_at = updated_at + interval '1 hour' where id = $1`},
		{"bump a terminal version", `update public.access_requests set version = version + 1 where id = $1`},
		{"retitle a terminal row", `update public.access_requests set title = 'renamed' where id = $1`},
	} {
		_, err := runtime.Exec(ctx, forged.sql, string(pending.ID))
		requireGuardRefusal(t, err, forged.name)
	}

	if _, err := runtime.Exec(ctx, `update public.access_requests set title = 'still a draft' where id = $1`, string(smuggled.ID)); err != nil {
		t.Errorf("draft title edit as runtime = %v, want allowed", err)
	}
}
