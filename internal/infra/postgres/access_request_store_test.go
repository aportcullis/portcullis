package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

type reqFixture struct {
	connFixture
	requests *pg.AccessRequestStore
	policies *pg.ConnectionPolicyStore

	requester identity.UserID
}

func newReqFixture(t *testing.T) reqFixture {
	t.Helper()
	return reqFixtureOver(t, newConnFixture(t))
}

func newReqFixtureFresh(t *testing.T) reqFixture {
	t.Helper()
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := pg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ids := pg.NewIdentityStore(pool)
	org, err := ids.DefaultOrganizationID(ctx)
	if err != nil {
		t.Fatalf("org: %v", err)
	}
	u, err := ids.CreateUser(ctx, unique("conn-admin")+"@example.com", "Conn Admin")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return reqFixtureOver(t, connFixture{pool: pool, store: pg.NewConnectionStore(pool), org: org, user: u.ID})
}

func reqFixtureOver(t *testing.T, f connFixture) reqFixture {
	t.Helper()
	fx := reqFixture{
		connFixture: f,
		requests:    pg.NewAccessRequestStore(f.pool),
		policies:    pg.NewConnectionPolicyStore(f.pool),
	}
	fx.requester = fx.userWithRole(t, "requester")
	return fx
}

func (f reqFixture) userWithRole(t *testing.T, role string) identity.UserID {
	t.Helper()
	ids := pg.NewIdentityStore(f.pool)
	u, err := ids.CreateUser(context.Background(), unique(role)+"@example.com", "User "+role)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := f.pool.Exec(context.Background(),
		`insert into organization_memberships (organization_id, user_id, role_id)
		 select $1, $2, r.id from roles r where r.organization_id = $1 and r.name = $3`,
		string(f.org), string(u.ID), role); err != nil {
		t.Fatalf("membership: %v", err)
	}
	return u.ID
}

// userWithCustomRole creates an active user holding a role built for the test with exactly the given permissions — the stand-in for the role-management API (M4). ADR-0008 lets a custom role hold any combination, so the store's checks must be exercised against combinations the seeded roles never produce.
func (f reqFixture) userWithCustomRole(t *testing.T, role string, permissions ...string) identity.UserID {
	t.Helper()
	ctx := context.Background()
	name := unique(role)
	ids := pg.NewIdentityStore(f.pool)
	u, err := ids.CreateUser(ctx, name+"@example.com", "User "+role)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	var roleID string
	if err := f.pool.QueryRow(ctx,
		`insert into roles (organization_id, name) values ($1, $2) returning id`,
		string(f.org), name).Scan(&roleID); err != nil {
		t.Fatalf("create role: %v", err)
	}
	for _, p := range permissions {
		if _, err := f.pool.Exec(ctx,
			`insert into role_permissions (role_id, permission_key) values ($1, $2)`, roleID, p); err != nil {
			t.Fatalf("grant %s: %v", p, err)
		}
	}
	if _, err := f.pool.Exec(ctx,
		`insert into organization_memberships (organization_id, user_id, role_id) values ($1, $2, $3)`,
		string(f.org), string(u.ID), roleID); err != nil {
		t.Fatalf("membership: %v", err)
	}
	return u.ID
}

func (f reqFixture) liveConn(t *testing.T, readQuorum int) connection.ConnectionID {
	t.Helper()
	ctx := context.Background()
	c := f.newConn(t, unique("ReqTarget"))
	if err := f.store.Create(ctx, c, sealedStub(1), connEvent(audit.ActionConnectionCreated, c.ID)); err != nil {
		t.Fatalf("Create connection: %v", err)
	}
	if readQuorum != 1 {
		cur, err := f.policies.GetCurrent(ctx, f.org, c.ID)
		if err != nil {
			t.Fatalf("GetCurrent: %v", err)
		}
		next, err := connection.NewPolicy(
			c.ID, f.org, cur.Version+1,
			connection.ClassRule{Allowed: true, RequiredApprovals: readQuorum},
			cur.Write, cur.DDL, cur.Limits, f.user, time.Now().UTC(),
		)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.policies.UpdatePolicy(ctx, next, cur.Version, connEvent(audit.ActionConnectionPolicyUpdated, c.ID)); err != nil {
			t.Fatalf("UpdatePolicy: %v", err)
		}
	}
	return c.ID
}

func (f reqFixture) draft(t *testing.T, connID connection.ConnectionID) access.Request {
	t.Helper()
	r, err := access.NewDraft(access.RequestID(uuid.NewString()), f.org, connID, f.requester, time.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.requests.CreateDraft(context.Background(), r, sealedPayloadStub(), reqEvent(audit.ActionAccessRequestCreated, r.ID)); err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	return r
}

func (f reqFixture) pin(t *testing.T, connID connection.ConnectionID) access.SubmitTarget {
	t.Helper()
	target, err := f.requests.CurrentTarget(context.Background(), f.org, connID)
	if err != nil {
		t.Fatalf("CurrentTarget: %v", err)
	}
	return target
}

// submitSnapshot is the snapshot the service would build for that target; tests that care about one field override it.
func submitSnapshot(target access.SubmitTarget, quorum int) access.Snapshot {
	return access.Snapshot{
		Class:                   connection.ClassRead,
		PolicyVersion:           target.Policy.Version,
		ConnectionConfigVersion: target.ConfigVersion,
		ConnectionFingerprint:   target.Fingerprint,
		ConnectionDisplayName:   target.DisplayName,
		ConnectionDBType:        target.DBType,
		RequiredApprovals:       quorum,
		Digest:                  []byte("digest-1"),
		DigestKeyVersion:        1,
		RedactedSQL:             "select <integer>",
	}
}

// submitted drives a draft to pending with the snapshot the service would pin.
func (f reqFixture) submitted(t *testing.T, connID connection.ConnectionID, quorum int) access.Request {
	t.Helper()
	r := f.draft(t, connID)
	sub, err := r.Submitted(submitSnapshot(f.pin(t, connID), quorum), time.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.requests.Submit(context.Background(), sub, 1, time.Hour, reqEvent(audit.ActionAccessRequestSubmitted, r.ID))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return got.Request
}

func sealedPayloadStub() access.SealedPayload {
	return access.SealedPayload{KeyVersion: 1, WrappedDEK: []byte("dek"), Nonce: []byte("nonce-abcdef"), Ciphertext: []byte("ciphertext")}
}

func reqEvent(action audit.Action, target access.RequestID) audit.Event {
	return audit.Event{
		ActorType: audit.ActorUser, Action: action,
		TargetType: audit.TargetTypeAccessRequest, TargetID: string(target),
		Outcome: audit.OutcomeSucceeded,
	}
}

func (f reqFixture) countEvents(t *testing.T, action audit.Action, target access.RequestID) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`select count(*) from audit_events where action = $1 and target_id = $2`,
		string(action), string(target)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAccessRequestLifecycle(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 2)
	approverA := f.userWithRole(t, "approver")
	approverB := f.userWithRole(t, "approver")

	r := f.draft(t, connID)
	got, sealed, err := f.requests.GetSealed(ctx, f.org, r.ID)
	if err != nil {
		t.Fatalf("GetSealed: %v", err)
	}
	if got.State != access.StateDraft || got.Version != 1 || string(sealed.Ciphertext) != "ciphertext" {
		t.Errorf("draft round trip: %+v / %+v", got, sealed)
	}

	updated, err := f.requests.UpdateDraft(ctx, r, sealedPayloadStub(), 1, reqEvent(audit.ActionAccessRequestUpdated, r.ID))
	if err != nil || updated.Request.Version != 2 {
		t.Fatalf("UpdateDraft = %+v, %v", updated.Request, err)
	}
	if updated.ConnectionName == "" || updated.RequesterEmail == "" {
		t.Errorf("mutation view missing display fields: %+v", updated)
	}
	got = updated.Request
	if _, err := f.requests.UpdateDraft(ctx, r, sealedPayloadStub(), 1); !errors.Is(err, access.ErrConflict) {
		t.Errorf("stale edit = %v, want ErrConflict", err)
	}

	pinned := f.pin(t, connID)
	sub, err := got.Submitted(submitSnapshot(pinned, 2), time.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		t.Fatal(err)
	}
	pending, err := f.requests.Submit(ctx, sub, 2, time.Hour, reqEvent(audit.ActionAccessRequestSubmitted, r.ID))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if pending.Request.State != access.StatePending || pending.Request.PolicyVersion != pinned.Policy.Version ||
		pending.Request.RequiredApprovals != 2 {
		t.Errorf("pending shape: %+v", pending.Request)
	}

	if pending.Request.ConnectionConfigVersion != pinned.ConfigVersion ||
		pending.Request.ConnectionFingerprint != pinned.Fingerprint {
		t.Errorf("target pin = config v%d / %q, want v%d / %q", pending.Request.ConnectionConfigVersion,
			pending.Request.ConnectionFingerprint, pinned.ConfigVersion, pinned.Fingerprint)
	}
	if pending.ConnectionName == "" {
		t.Error("submit view missing connection name")
	}
	if _, err := f.requests.UpdateDraft(ctx, r, sealedPayloadStub(), 3); !errors.Is(err, access.ErrNotDraft) {
		t.Errorf("post-submit edit = %v, want ErrNotDraft", err)
	}

	view, err := f.requests.Approve(ctx, f.org, r.ID, approverA, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, r.ID))
	if err != nil {
		t.Fatalf("first approve: %v", err)
	}
	if view.Request.State != access.StatePending || view.ValidApprovals != 1 {
		t.Errorf("after 1/2: %+v", view.Request)
	}
	before := time.Now().UTC()
	view, err = f.requests.Approve(ctx, f.org, r.ID, approverB, "lgtm", time.Hour, reqEvent(audit.ActionAccessRequestApproved, r.ID))
	if err != nil {
		t.Fatalf("second approve: %v", err)
	}
	if view.Request.State != access.StateApproved || view.ValidApprovals != 2 {
		t.Errorf("after 2/2: %+v", view.Request)
	}
	if view.Request.ExpiresAt == nil || view.Request.ExpiresAt.Before(before.Add(time.Hour).Add(-time.Minute)) {
		t.Errorf("ExpiresAt = %v, want ≈ now+1h", view.Request.ExpiresAt)
	}

	if view.RequesterDisplayName == "" || view.ConnectionName == "" || len(view.Approvals) != 2 {
		t.Errorf("view fields: %+v", view)
	}
	for _, a := range view.Approvals {
		if !a.Valid || a.ApproverDisplayName == "" {
			t.Errorf("approval view: %+v", a)
		}
	}
	if n := f.countEvents(t, audit.ActionAccessRequestApproved, r.ID); n != 2 {
		t.Errorf("APPROVED events = %d, want 2", n)
	}
}

func TestDecisionRechecksApproverEligibility(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 1)
	r := f.submitted(t, connID, 1)

	plain := f.userWithRole(t, "requester")
	if _, err := f.requests.Reject(ctx, f.org, r.ID, plain, "nope", reqEvent(audit.ActionAccessRequestRejected, r.ID)); !errors.Is(err, access.ErrApproverIneligible) {
		t.Errorf("reject by non-rejecter = %v, want ErrApproverIneligible", err)
	}
	if _, err := f.requests.Approve(ctx, f.org, r.ID, plain, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, r.ID)); !errors.Is(err, access.ErrApproverIneligible) {
		t.Errorf("approve by non-approver = %v, want ErrApproverIneligible", err)
	}

	// A deactivated approver — role intact — cannot reject either.
	gone := f.userWithRole(t, "approver")
	if _, err := f.pool.Exec(ctx, `update users set status = 'disabled' where id = $1`, string(gone)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.requests.Reject(ctx, f.org, r.ID, gone, "wrong table", reqEvent(audit.ActionAccessRequestRejected, r.ID)); !errors.Is(err, access.ErrApproverIneligible) {
		t.Errorf("reject by disabled approver = %v, want ErrApproverIneligible", err)
	}

	approver := f.userWithRole(t, "approver")
	view, err := f.requests.Reject(ctx, f.org, r.ID, approver, "wrong table", reqEvent(audit.ActionAccessRequestRejected, r.ID))
	if err != nil || view.Request.State != access.StateRejected {
		t.Fatalf("eligible reject = %+v, %v", view.Request, err)
	}
}

func TestAccessRequestApprovalGuards(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 2)
	approverA := f.userWithRole(t, "approver")
	r := f.submitted(t, connID, 2)

	if _, err := f.requests.Approve(ctx, f.org, r.ID, f.requester, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, r.ID)); !errors.Is(err, access.ErrSelfApproval) {
		t.Errorf("self-approval = %v, want ErrSelfApproval", err)
	}
	if _, err := f.requests.Approve(ctx, f.org, r.ID, approverA, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, r.ID)); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := f.requests.Approve(ctx, f.org, r.ID, approverA, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, r.ID)); !errors.Is(err, access.ErrAlreadyDecided) {
		t.Errorf("duplicate = %v, want ErrAlreadyDecided", err)
	}
	if _, err := f.requests.Reject(ctx, f.org, r.ID, approverA, "", reqEvent(audit.ActionAccessRequestRejected, r.ID)); !errors.Is(err, access.ErrReasonRequired) && !errors.Is(err, access.ErrAlreadyDecided) {
		t.Errorf("reasonless reject = %v", err)
	}
}

func TestAccessRequestApprovalRace(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 2)
	approverA := f.userWithRole(t, "approver")
	approverB := f.userWithRole(t, "approver")
	approverC := f.userWithRole(t, "approver")
	r := f.submitted(t, connID, 2)
	if _, err := f.requests.Approve(ctx, f.org, r.ID, approverA, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, r.ID)); err != nil {
		t.Fatalf("seed approve: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for idx, ap := range []identity.UserID{approverB, approverC} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[idx] = f.requests.Approve(ctx, f.org, r.ID, ap, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, r.ID))
		}()
	}
	wg.Wait()

	winners, losers := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, access.ErrNotPending):
			losers++
		default:
			t.Fatalf("unexpected race error: %v", err)
		}
	}
	if winners != 1 || losers != 1 {
		t.Errorf("race outcome = %d winners / %d losers, want exactly 1/1", winners, losers)
	}
	view, sealed, err := f.requests.Get(ctx, f.org, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sealed.Ciphertext) == 0 {
		t.Error("Get must return the sealed payload alongside the view")
	}
	if view.Request.State != access.StateApproved {
		t.Errorf("final state = %q", view.Request.State)
	}
}

func TestAccessRequestInvalidatedApprover(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 2)
	approverA := f.userWithRole(t, "approver")
	approverB := f.userWithRole(t, "approver")
	r := f.submitted(t, connID, 2)

	if _, err := f.requests.Approve(ctx, f.org, r.ID, approverA, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, r.ID)); err != nil {
		t.Fatalf("approve A: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `update users set status = 'disabled' where id = $1`, string(approverA)); err != nil {
		t.Fatal(err)
	}

	view, err := f.requests.Approve(ctx, f.org, r.ID, approverB, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, r.ID))
	if err != nil {
		t.Fatalf("approve B: %v", err)
	}
	if view.Request.State != access.StatePending || view.ValidApprovals != 1 {
		t.Errorf("after invalidation: state=%q valid=%d, want pending/1", view.Request.State, view.ValidApprovals)
	}
	for _, a := range view.Approvals {
		if a.Approval.ApproverID == approverA && a.Valid {
			t.Error("disabled approver's decision must not read as valid")
		}
	}
}

func TestSoftDeletedRolePermissionStopsGrantingApproval(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	identities := pg.NewIdentityStore(f.pool)
	admin := f.userWithRole(t, "admin")
	roleEvent := audit.Event{ActorType: audit.ActorUser, ActorUserID: &admin, Action: audit.ActionRoleUpdated, TargetType: audit.TargetTypeRole, Outcome: audit.OutcomeSucceeded}
	reviewerRole, err := identities.CreateRole(ctx, f.org, identity.Delegation{Actor: admin, Gate: identity.PermissionRolesCreate}, identity.RoleDefinition{Name: unique("reviewer"), Permissions: []identity.Permission{identity.PermissionRequestsApprove, identity.PermissionRequestsReject}}, roleEvent)
	if err != nil {
		t.Fatal(err)
	}
	reviewer, _, err := identities.CreateMember(ctx, f.org, identity.Delegation{Actor: admin, Gate: identity.PermissionUsersCreate}, identity.NewMember{Email: unique("reviewer") + "@example.com", DisplayName: "Reviewer", RoleID: reviewerRole.ID}, identity.PasswordSetupIssue{TokenHash: make([]byte, 32), Validity: time.Hour}, roleEvent)
	if err != nil {
		t.Fatal(err)
	}
	demoted := reviewer.User.ID
	updateReviewerRole := func(version int64, permissions ...identity.Permission) {
		t.Helper()
		if _, err := identities.UpdateRole(ctx, f.org, identity.Delegation{Actor: admin, Gate: identity.PermissionRolesUpdate}, reviewerRole.ID, version, identity.RoleDefinition{Name: reviewerRole.Name, Permissions: permissions}, roleEvent); err != nil {
			t.Fatalf("UpdateRole(version %d): %v", version, err)
		}
	}
	connID := f.liveConn(t, 2)
	steady := f.userWithRole(t, "approver")
	pending := f.submitted(t, connID, 2)
	if _, err := f.requests.Approve(ctx, f.org, pending.ID, demoted, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, pending.ID)); err != nil {
		t.Fatalf("approve before demotion: %v", err)
	}
	updateReviewerRole(1, identity.PermissionRequestsReject)

	other := f.submitted(t, connID, 2)
	if _, err := f.requests.Approve(ctx, f.org, other.ID, demoted, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, other.ID)); !errors.Is(err, access.ErrApproverIneligible) {
		t.Errorf("approve after soft-deleted permission = %v, want ErrApproverIneligible", err)
	}
	view, err := f.requests.Approve(ctx, f.org, pending.ID, steady, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, pending.ID))
	if err != nil || view.Request.State != access.StatePending || view.ValidApprovals != 1 {
		t.Fatalf("second approval = state %q valid %d (%v), want pending with 1 valid approval", view.Request.State, view.ValidApprovals, err)
	}
	for _, decision := range view.Approvals {
		if decision.Approval.ApproverID == demoted && decision.Valid {
			t.Error("an approval whose permission was soft-deleted must not read as valid")
		}
	}
	fetched, _, err := f.requests.Get(ctx, f.org, pending.ID)
	if err != nil || fetched.ValidApprovals != 1 {
		t.Errorf("Get valid approvals = %d (%v), want 1", fetched.ValidApprovals, err)
	}
	for _, descending := range []bool{true, false} {
		page, err := f.requests.List(ctx, f.org, access.ListQuery{Page: 1, PageSize: 100, SortDescending: descending})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		for _, item := range page.Items {
			if item.Request.ID == pending.ID && item.ValidApprovals != 1 {
				t.Errorf("List(descending=%t) valid approvals = %d, want 1", descending, item.ValidApprovals)
			}
		}
	}
	permissions, err := pg.NewIdentityStore(f.pool).PermissionsForUser(ctx, f.org, demoted)
	if err != nil || slices.Contains(permissions, identity.PermissionRequestsApprove) || !slices.Contains(permissions, identity.Permission("requests.reject")) {
		t.Errorf("permissions after soft delete = %v (%v), want requests.reject without requests.approve", permissions, err)
	}

	updateReviewerRole(2, identity.PermissionRequestsApprove, identity.PermissionRequestsReject)
	revived, err := f.requests.Approve(ctx, f.org, other.ID, demoted, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, other.ID))
	if err != nil || revived.ValidApprovals != 1 {
		t.Errorf("approve after revival = valid %d (%v), want 1", revived.ValidApprovals, err)
	}
	restored, _, err := f.requests.Get(ctx, f.org, pending.ID)
	if err != nil || restored.ValidApprovals != 2 {
		t.Errorf("revived approval valid count = %d (%v), want 2", restored.ValidApprovals, err)
	}
}

func TestPolicyChangeExpiresInFlightRequests(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 1)
	pending := f.submitted(t, connID, 1)
	still := f.draft(t, connID)

	cur, err := f.policies.GetCurrent(ctx, f.org, connID)
	if err != nil {
		t.Fatal(err)
	}
	next := validNextPolicy(t, cur, f.user)
	if _, err := f.policies.UpdatePolicy(ctx, next, cur.Version, connEvent(audit.ActionConnectionPolicyUpdated, connID)); err != nil {
		t.Fatalf("UpdatePolicy: %v", err)
	}

	got, _, err := f.requests.GetSealed(ctx, f.org, pending.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != access.StateExpired || got.Reason != access.ReasonPolicyChanged {
		t.Errorf("in-flight after policy change = %q/%q, want expired/policy_changed", got.State, got.Reason)
	}
	if n := f.countEvents(t, audit.ActionAccessRequestExpired, pending.ID); n != 1 {
		t.Errorf("EXPIRED events = %d, want 1", n)
	}
	if got, _, err = f.requests.GetSealed(ctx, f.org, still.ID); err != nil || got.State != access.StateDraft {
		t.Errorf("draft after policy change = %+v, %v; want untouched draft", got, err)
	}
}

func TestRequestViewShowsTheTargetAsItWasAtSubmit(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 1)
	before, err := f.store.GetByID(ctx, f.org, connID)
	if err != nil {
		t.Fatal(err)
	}

	pending := f.submitted(t, connID, 1)
	draft := f.draft(t, connID)

	renamed := unique("Relabelled")
	if _, err := f.store.UpdateDescriptor(ctx, f.org, connID, renamed, before.Environment,
		before.Description, before.Version, connEvent(audit.ActionConnectionUpdated, connID)); err != nil {
		t.Fatalf("UpdateDescriptor: %v", err)
	}

	submittedView, _, err := f.requests.Get(ctx, f.org, pending.ID)
	if err != nil {
		t.Fatal(err)
	}
	if submittedView.ConnectionName != before.DisplayName {
		t.Errorf("submitted request names %q, want the name it was submitted against (%q)",
			submittedView.ConnectionName, before.DisplayName)
	}
	if submittedView.Request.ConnectionDBType != string(before.DBType) ||
		submittedView.Request.ConnectionFingerprint != before.Fingerprint {
		t.Errorf("target snapshot = %q/%q, want %q/%q", submittedView.Request.ConnectionDBType,
			submittedView.Request.ConnectionFingerprint, before.DBType, before.Fingerprint)
	}

	draftView, _, err := f.requests.Get(ctx, f.org, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if draftView.ConnectionName != renamed {
		t.Errorf("draft names %q, want the CURRENT name %q — a draft has no snapshot yet",
			draftView.ConnectionName, renamed)
	}

	// The list is the same view, so it must answer the same way. The fixture's database is shared with the rest of the package, so only the two rows this test created are asserted on.
	page, err := f.requests.List(ctx, f.org, access.ListQuery{Page: 1, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	names := map[access.RequestID]string{}
	for _, item := range page.Items {
		names[item.Request.ID] = item.ConnectionName
	}
	if got := names[pending.ID]; got != before.DisplayName {
		t.Errorf("list row for the submitted request names %q, want %q", got, before.DisplayName)
	}
	if got := names[draft.ID]; got != renamed {
		t.Errorf("list row for the draft names %q, want the current %q", got, renamed)
	}
}

func TestSubmitSnapshotIsAllOrNothingAtTheSchema(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 1)
	submitted := f.submitted(t, connID, 1)
	draft := f.draft(t, connID)

	for _, column := range []string{
		"payload_digest", "payload_digest_key_version", "redacted_sql", "statement_class",
		"policy_version", "connection_config_version", "connection_fingerprint",
		"connection_display_name", "connection_db_type", "required_approvals",
	} {
		_, err := f.pool.Exec(ctx,
			`update access_requests set `+column+` = null where id = $1`, string(submitted.ID))
		if err == nil {
			t.Errorf("nulling %s on a submitted request succeeded — the snapshot must be all-or-nothing", column)
		}
	}

	if _, err := f.pool.Exec(ctx,
		`update access_requests set state = 'cancelled' where id = $1`, string(submitted.ID)); err != nil {
		t.Fatalf("cancel a submitted request: %v", err)
	}
	if _, err := f.pool.Exec(ctx,
		`update access_requests set payload_digest = null, redacted_sql = null where id = $1`,
		string(submitted.ID)); err == nil {
		t.Error("a cancelled SUBMISSION let its evidence be erased")
	}

	if _, err := f.pool.Exec(ctx,
		`update access_requests set statement_class = 'read' where id = $1`, string(draft.ID)); err == nil {
		t.Error("a draft accepted a partial snapshot")
	}

	if _, err := f.pool.Exec(ctx,
		`update access_requests set state = 'cancelled' where id = $1`, string(draft.ID)); err != nil {
		t.Errorf("cancelling a draft with no snapshot = %v, want it allowed", err)
	}
}

func TestConfigReplacementExpiresLiveRequests(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 1)

	draft := f.draft(t, connID)
	pending := f.submitted(t, connID, 1)
	approver := f.userWithRole(t, "approver")
	approved := f.submitted(t, connID, 1)
	if _, err := f.requests.Approve(ctx, f.org, approved.ID, approver, "", time.Hour,
		reqEvent(audit.ActionAccessRequestApproved, approved.ID)); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	before, err := f.store.GetByID(ctx, f.org, connID)
	if err != nil {
		t.Fatal(err)
	}
	target, err := connection.NewTarget("replica.example.com", 5433, "otherdb")
	if err != nil {
		t.Fatal(err)
	}
	replaced := before
	replaced.Target = target
	replaced.Fingerprint = target.Fingerprint(replaced.DBType)
	after, err := f.store.ReplaceConfig(ctx, replaced, before.Version, sealedStub(2),
		connEvent(audit.ActionConnectionUpdated, connID))
	if err != nil {
		t.Fatalf("ReplaceConfig: %v", err)
	}

	for _, tc := range []struct {
		name string
		id   access.RequestID
	}{{"pending", pending.ID}, {"approved", approved.ID}} {
		got, _, err := f.requests.GetSealed(ctx, f.org, tc.id)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got.State != access.StateExpired || got.Reason != access.ReasonConnectionChanged {
			t.Errorf("%s after the config replacement = %s/%s, want expired/connection_changed",
				tc.name, got.State, got.Reason)
		}
		if n := f.countEvents(t, audit.ActionAccessRequestExpired, tc.id); n != 1 {
			t.Errorf("%s EXPIRED events = %d, want 1", tc.name, n)
		}
	}

	stillDraft, _, err := f.requests.GetSealed(ctx, f.org, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stillDraft.State != access.StateDraft {
		t.Errorf("draft after the config replacement = %s, want an untouched draft", stillDraft.State)
	}

	if after.ConfigVersion != before.ConfigVersion+1 {
		t.Errorf("config_version = %d, want %d", after.ConfigVersion, before.ConfigVersion+1)
	}
	stalePin := f.pin(t, connID)
	stalePin.ConfigVersion = before.ConfigVersion
	stalePin.Fingerprint = before.Fingerprint
	stale, err := stillDraft.Submitted(submitSnapshot(stalePin, 1), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.requests.Submit(ctx, stale, stillDraft.Version, time.Hour,
		reqEvent(audit.ActionAccessRequestSubmitted, stillDraft.ID)); !errors.Is(err, access.ErrConnectionChanged) {
		t.Errorf("submit pinning the replaced config = %v, want ErrConnectionChanged", err)
	}
}

func TestRenameAndArchiveLeaveTheConfigTokenAlone(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 1)
	approver := f.userWithRole(t, "approver")
	approved := f.submitted(t, connID, 1)
	if _, err := f.requests.Approve(ctx, f.org, approved.ID, approver, "", time.Hour,
		reqEvent(audit.ActionAccessRequestApproved, approved.ID)); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	before, err := f.store.GetByID(ctx, f.org, connID)
	if err != nil {
		t.Fatal(err)
	}

	renamed, err := f.store.UpdateDescriptor(ctx, f.org, connID, unique("Relabelled"),
		before.Environment, before.Description, before.Version, connEvent(audit.ActionConnectionUpdated, connID))
	if err != nil {
		t.Fatalf("UpdateDescriptor: %v", err)
	}
	if renamed.ConfigVersion != before.ConfigVersion {
		t.Errorf("rename moved config_version %d → %d", before.ConfigVersion, renamed.ConfigVersion)
	}
	got, _, err := f.requests.GetSealed(ctx, f.org, approved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != access.StateApproved {
		t.Errorf("approved request after a rename = %s, want it untouched", got.State)
	}

	// Archive has its own reason and its own cascade; it must not be confused with a config change either.
	if _, err := f.store.Archive(ctx, f.org, connID, connEvent(audit.ActionConnectionArchived, connID)); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	swept, _, err := f.requests.GetSealed(ctx, f.org, approved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if swept.Reason != access.ReasonConnectionArchived {
		t.Errorf("archived cascade reason = %s, want connection_archived", swept.Reason)
	}
}

func TestArchiveCascadesRequests(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 1)
	d := f.draft(t, connID)
	p := f.submitted(t, connID, 1)

	exec := f.submitted(t, connID, 1)
	if _, err := f.pool.Exec(ctx, `update access_requests set state = 'executing' where id = $1`, string(exec.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Archive(ctx, f.org, connID, connEvent(audit.ActionConnectionArchived, connID)); !errors.Is(err, connection.ErrExecutionInFlight) {
		t.Fatalf("archive with executing = %v, want ErrExecutionInFlight", err)
	}
	if _, err := f.pool.Exec(ctx, `update access_requests set state = 'cancelled', state_reason = null where id = $1`, string(exec.ID)); err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.Archive(ctx, f.org, connID, connEvent(audit.ActionConnectionArchived, connID)); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	got, _, err := f.requests.GetSealed(ctx, f.org, d.ID)
	if err != nil || got.State != access.StateCancelled || got.Reason != access.ReasonConnectionArchived {
		t.Errorf("draft after archive = %+v, %v; want cancelled/connection_archived", got, err)
	}
	if got, _, err = f.requests.GetSealed(ctx, f.org, p.ID); err != nil || got.State != access.StateExpired || got.Reason != access.ReasonConnectionArchived {
		t.Errorf("pending after archive = %+v, %v; want expired/connection_archived", got, err)
	}
	if n := f.countEvents(t, audit.ActionAccessRequestCancelled, d.ID); n != 1 {
		t.Errorf("CANCELLED events = %d, want 1", n)
	}
}

func holdConnection(t *testing.T, pool *pgxpool.Pool, id connection.ConnectionID) pgx.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	if _, err := tx.Exec(ctx, `select id from connections where id = $1 for share`, string(id)); err != nil {
		t.Fatalf("hold connection: %v", err)
	}
	return tx
}

// stampBlockerEvent writes, inside the blocking transaction, the audit event a real write would record at that moment — with the actual current time, the way every stamped path in the store does.
func stampBlockerEvent(t *testing.T, tx pgx.Tx, org identity.OrganizationID, action audit.Action, targetType, targetID string) time.Time {
	t.Helper()
	var at time.Time
	if err := tx.QueryRow(context.Background(),
		`insert into audit_events (organization_id, occurred_at, actor_type, actor_service, action, target_type, target_id, outcome)
		 values ($1, clock_timestamp(), 'system', 'test.blocker', $2, $3, $4, 'succeeded')
		 returning occurred_at`,
		string(org), string(action), targetType, targetID).Scan(&at); err != nil {
		t.Fatalf("stamp blocker event: %v", err)
	}
	return at
}

func stampWhileHolding(t *testing.T, tx pgx.Tx, org identity.OrganizationID, id access.RequestID) (time.Time, time.Time) {
	t.Helper()
	var rowAt time.Time
	if err := tx.QueryRow(context.Background(),
		`update access_requests set updated_at = clock_timestamp() where id = $1 returning updated_at`,
		string(id)).Scan(&rowAt); err != nil {
		t.Fatalf("stamp row: %v", err)
	}
	return rowAt, stampBlockerEvent(t, tx, org, audit.ActionAccessRequestSubmitted, "access_request", string(id))
}

func eventInstant(t *testing.T, pool *pgxpool.Pool, action audit.Action, targetID string) time.Time {
	t.Helper()
	var at time.Time
	if err := pool.QueryRow(context.Background(),
		`select occurred_at from audit_events where action = $1 and target_id = $2 and actor_service is distinct from 'test.blocker'`,
		string(action), targetID).Scan(&at); err != nil {
		t.Fatalf("read %s event: %v", action, err)
	}
	return at
}

func cascadeStamps(t *testing.T, pool *pgxpool.Pool, id access.RequestID, action audit.Action) (time.Time, time.Time) {
	t.Helper()
	var rowAt, eventAt time.Time
	if err := pool.QueryRow(context.Background(),
		`select r.updated_at, e.occurred_at
		   from access_requests r
		   join audit_events e on e.target_id = r.id::text and e.action = $2
		  where r.id = $1`,
		string(id), string(action)).Scan(&rowAt, &eventAt); err != nil {
		t.Fatalf("read cascade stamps: %v", err)
	}
	return rowAt, eventAt
}

func TestArchiveCascadeStampsAfterTheLockWait(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 1)
	d := f.draft(t, connID)

	holder := holdConnection(t, f.pool, connID)

	done := make(chan error, 1)
	go func() {
		_, err := f.store.Archive(ctx, f.org, connID, connEvent(audit.ActionConnectionArchived, connID))
		done <- err
	}()

	time.Sleep(300 * time.Millisecond)

	wroteRowAt, wroteEventAt := stampWhileHolding(t, holder, f.org, d.ID)
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("release connection: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Archive: %v", err)
	}

	sweptAt, sweepEventAt := cascadeStamps(t, f.pool, d.ID, audit.ActionAccessRequestCancelled)
	if sweptAt.Before(wroteRowAt) {
		t.Errorf("swept updated_at %s precedes the write it waited for (%s) — the row's history runs backwards", sweptAt, wroteRowAt)
	}
	if sweepEventAt.Before(wroteEventAt) {
		t.Errorf("cascade event %s precedes the request event it waited for (%s) — the trail lists the sweep above its own cause", sweepEventAt, wroteEventAt)
	}
	if !sweepEventAt.Equal(sweptAt) {
		t.Errorf("event %s != row %s — the sweep must be stamped from ONE observed instant", sweepEventAt, sweptAt)
	}
}

func TestPolicyCascadeStampsAfterTheLockWait(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 1)
	p := f.submitted(t, connID, 1)

	pinned, err := f.policies.GetCurrent(ctx, f.org, connID)
	if err != nil {
		t.Fatalf("current policy: %v", err)
	}
	next, err := connection.NewPolicy(connID, f.org, pinned.Version+1,
		connection.ClassRule{Allowed: true, RequiredApprovals: 2}, pinned.Write, pinned.DDL,
		pinned.Limits, f.requester, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}

	holder := holdConnection(t, f.pool, connID)

	done := make(chan error, 1)
	go func() {
		_, err := f.policies.UpdatePolicy(ctx, next, pinned.Version, connEvent(audit.ActionConnectionPolicyUpdated, connID))
		done <- err
	}()
	time.Sleep(300 * time.Millisecond)

	wroteRowAt, wroteEventAt := stampWhileHolding(t, holder, f.org, p.ID)
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("release connection: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("UpdatePolicy: %v", err)
	}

	expiredAt, expiredEventAt := cascadeStamps(t, f.pool, p.ID, audit.ActionAccessRequestExpired)
	if expiredAt.Before(wroteRowAt) {
		t.Errorf("expired updated_at %s precedes the write it waited for (%s)", expiredAt, wroteRowAt)
	}
	if expiredEventAt.Before(wroteEventAt) {
		t.Errorf("EXPIRED event %s precedes the request event it waited for (%s)", expiredEventAt, wroteEventAt)
	}
}

func TestCascadeStampsAfterWaitingOnARequestRow(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 1)
	p := f.submitted(t, connID, 1)

	holder, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}

	if _, err := holder.Exec(ctx, `select id from access_requests where id = $1 for update`, string(p.ID)); err != nil {
		t.Fatalf("hold request row: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := f.store.Archive(ctx, f.org, connID, connEvent(audit.ActionConnectionArchived, connID))
		done <- err
	}()
	time.Sleep(300 * time.Millisecond)

	wroteRowAt, wroteEventAt := stampWhileHolding(t, holder, f.org, p.ID)
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("release request row: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Archive: %v", err)
	}

	expiredAt, expiredEventAt := cascadeStamps(t, f.pool, p.ID, audit.ActionAccessRequestExpired)
	if expiredAt.Before(wroteRowAt) {
		t.Errorf("expired updated_at %s precedes the decision-shaped write it waited for (%s)", expiredAt, wroteRowAt)
	}
	if expiredEventAt.Before(wroteEventAt) {
		t.Errorf("EXPIRED event %s precedes the event it waited for (%s)", expiredEventAt, wroteEventAt)
	}
}

func TestSubmittedAtComesFromTheDatabase(t *testing.T) {
	ctx := context.Background()

	t.Run("later than the write it waited for, equal to updated_at", func(t *testing.T) {
		f := newReqFixture(t)
		connID := f.liveConn(t, 1)
		d := f.draft(t, connID)
		sub, err := d.Submitted(submitSnapshot(f.pin(t, connID), 1), time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}

		blockedAt := blockOn(t, f.pool, `select id from connections where id = $1 for update`, string(connID), func(tx pgx.Tx) time.Time {
			return stampBlockerEvent(t, tx, f.org, audit.ActionConnectionPolicyUpdated, "connection", string(connID))
		}, func() error {
			_, err := f.requests.Submit(ctx, sub, 1, time.Hour, reqEvent(audit.ActionAccessRequestSubmitted, d.ID))
			return err
		})

		var submittedAt, updatedAt time.Time
		if err := f.pool.QueryRow(ctx,
			`select submitted_at, updated_at from access_requests where id = $1`,
			string(d.ID)).Scan(&submittedAt, &updatedAt); err != nil {
			t.Fatalf("read stamps: %v", err)
		}
		if submittedAt.Before(blockedAt) {
			t.Errorf("submitted_at %s precedes the write it waited for (%s)", submittedAt, blockedAt)
		}
		if !submittedAt.Equal(updatedAt) {
			t.Errorf("submitted_at %s != updated_at %s — one statement, one moment", submittedAt, updatedAt)
		}
	})

	t.Run("adversarial: a skewed application clock does not move it", func(t *testing.T) {
		f := newReqFixture(t)
		connID := f.liveConn(t, 1)
		for _, skew := range []time.Duration{72 * time.Hour, -72 * time.Hour} {
			d := f.draft(t, connID)
			sub, err := d.Submitted(submitSnapshot(f.pin(t, connID), 1), time.Now().UTC().Add(skew))
			if err != nil {
				t.Fatal(err)
			}
			view, err := f.requests.Submit(ctx, sub, 1, time.Hour, reqEvent(audit.ActionAccessRequestSubmitted, d.ID))
			if err != nil {
				t.Fatalf("Submit with %s skew: %v", skew, err)
			}
			var submittedAt time.Time
			if err := f.pool.QueryRow(ctx,
				`select submitted_at from access_requests where id = $1`, string(d.ID)).Scan(&submittedAt); err != nil {
				t.Fatal(err)
			}
			if drift := submittedAt.Sub(time.Now().UTC()); drift > time.Minute || drift < -time.Minute {
				t.Errorf("submitted_at %s took the caller's %s skew instead of the database clock", submittedAt, skew)
			}
			if view.Request.SubmittedAt == nil || !view.Request.SubmittedAt.Equal(submittedAt) {
				t.Errorf("returned submitted_at = %v, want the stored %s", view.Request.SubmittedAt, submittedAt)
			}
		}
	})

	t.Run("an auto-approval is approved at the instant it was submitted", func(t *testing.T) {
		f := newReqFixture(t)
		connID := f.liveConn(t, 0)
		d := f.draft(t, connID)
		sub, err := d.Submitted(submitSnapshot(f.pin(t, connID), 0), time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		approved, err := sub.Approved(time.Now().UTC(), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		autoApproved := reqEvent(audit.ActionAccessRequestApproved, d.ID)
		autoApproved.ActorType = audit.ActorSystem
		autoApproved.ActorService = access.ActorAutoApproval
		if _, err := f.requests.Submit(ctx, approved, 1, time.Hour,
			reqEvent(audit.ActionAccessRequestSubmitted, d.ID), autoApproved); err != nil {
			t.Fatalf("Submit: %v", err)
		}

		var submittedAt, expiresAt time.Time
		if err := f.pool.QueryRow(ctx,
			`select submitted_at, expires_at from access_requests where id = $1`,
			string(d.ID)).Scan(&submittedAt, &expiresAt); err != nil {
			t.Fatal(err)
		}
		if !expiresAt.Add(-time.Hour).Equal(submittedAt) {
			t.Errorf("expires_at - validity = %s, want the submission instant %s — the auto-approval happens AT the submit",
				expiresAt.Add(-time.Hour), submittedAt)
		}
	})
}

func TestRequestWritesStampAfterTheLockWait(t *testing.T) {
	ctx := context.Background()

	t.Run("create draft parked on the connection", func(t *testing.T) {
		f := newReqFixture(t)
		connID := f.liveConn(t, 1)
		r, err := access.NewDraft(access.RequestID(uuid.NewString()), f.org, connID, f.requester, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}

		blockedAt := blockOn(t, f.pool, `select id from connections where id = $1 for update`, string(connID), func(tx pgx.Tx) time.Time {
			return stampBlockerEvent(t, tx, f.org, audit.ActionConnectionArchived, "connection", string(connID))
		}, func() error {
			_, err := f.requests.CreateDraft(ctx, r, sealedPayloadStub(), reqEvent(audit.ActionAccessRequestCreated, r.ID))
			return err
		})

		if got := eventInstant(t, f.pool, audit.ActionAccessRequestCreated, string(r.ID)); got.Before(blockedAt) {
			t.Errorf("CREATED event %s precedes the write it waited for (%s)", got, blockedAt)
		}
	})

	t.Run("submit parked on the connection", func(t *testing.T) {
		f := newReqFixture(t)
		connID := f.liveConn(t, 1)
		d := f.draft(t, connID)
		sub, err := d.Submitted(submitSnapshot(f.pin(t, connID), 1), time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}

		blockedAt := blockOn(t, f.pool, `select id from connections where id = $1 for update`, string(connID), func(tx pgx.Tx) time.Time {
			return stampBlockerEvent(t, tx, f.org, audit.ActionConnectionPolicyUpdated, "connection", string(connID))
		}, func() error {
			_, err := f.requests.Submit(ctx, sub, 1, time.Hour, reqEvent(audit.ActionAccessRequestSubmitted, d.ID))
			return err
		})

		if got := eventInstant(t, f.pool, audit.ActionAccessRequestSubmitted, string(d.ID)); got.Before(blockedAt) {
			t.Errorf("SUBMITTED event %s precedes the write it waited for (%s)", got, blockedAt)
		}
	})

	t.Run("update draft parked on its own row", func(t *testing.T) {
		f := newReqFixture(t)
		connID := f.liveConn(t, 1)
		d := f.draft(t, connID)

		blockedAt := blockOn(t, f.pool, `select id from access_requests where id = $1 for update`, string(d.ID), func(tx pgx.Tx) time.Time {
			return stampBlockerEvent(t, tx, f.org, audit.ActionAccessRequestApproved, "access_request", string(d.ID))
		}, func() error {
			_, err := f.requests.UpdateDraft(ctx, d, sealedPayloadStub(), 1, reqEvent(audit.ActionAccessRequestUpdated, d.ID))
			return err
		})

		if got := eventInstant(t, f.pool, audit.ActionAccessRequestUpdated, string(d.ID)); got.Before(blockedAt) {
			t.Errorf("UPDATED event %s precedes the write it waited for (%s)", got, blockedAt)
		}
	})

	t.Run("cancel parked on its own row", func(t *testing.T) {
		f := newReqFixture(t)
		connID := f.liveConn(t, 1)
		d := f.draft(t, connID)

		blockedAt := blockOn(t, f.pool, `select id from access_requests where id = $1 for update`, string(d.ID), func(tx pgx.Tx) time.Time {
			return stampBlockerEvent(t, tx, f.org, audit.ActionAccessRequestApproved, "access_request", string(d.ID))
		}, func() error {
			_, err := f.requests.Cancel(ctx, f.org, d.ID, f.requester, reqEvent(audit.ActionAccessRequestCancelled, d.ID))
			return err
		})

		if got := eventInstant(t, f.pool, audit.ActionAccessRequestCancelled, string(d.ID)); got.Before(blockedAt) {
			t.Errorf("CANCELLED event %s precedes the write it waited for (%s)", got, blockedAt)
		}
	})
}

// blockOn holds `lock` in its own transaction, runs `call` (which parks on that lock), lets the blocker write while the caller waits, then releases. It returns the blocker's instant — everything the parked call stamps must be at or after it.
func blockOn(t *testing.T, pool *pgxpool.Pool, lock, id string, write func(pgx.Tx) time.Time, call func() error) time.Time {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin blocker: %v", err)
	}
	if _, err := tx.Exec(ctx, lock, id); err != nil {
		t.Fatalf("take blocking lock: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- call() }()

	time.Sleep(300 * time.Millisecond)

	at := write(tx)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("release blocking lock: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("blocked call: %v", err)
	}
	return at
}

func TestTTLExpiryObservedOnTouch(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 1)
	approverA := f.userWithRole(t, "approver")
	r := f.submitted(t, connID, 1)
	if _, err := f.requests.Approve(ctx, f.org, r.ID, approverA, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, r.ID)); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `update access_requests set expires_at = now() - interval '1 minute' where id = $1`, string(r.ID)); err != nil {
		t.Fatal(err)
	}

	if _, err := f.requests.Cancel(ctx, f.org, r.ID, f.requester, reqEvent(audit.ActionAccessRequestCancelled, r.ID)); !errors.Is(err, access.ErrNotCancellable) {
		t.Errorf("cancel after ttl = %v, want ErrNotCancellable", err)
	}
	got, _, err := f.requests.GetSealed(ctx, f.org, r.ID)
	if err != nil || got.State != access.StateExpired || got.Reason != access.ReasonTTLExpired {
		t.Errorf("after observation = %+v, %v; want expired/ttl_expired", got, err)
	}
	if n := f.countEvents(t, audit.ActionAccessRequestExpired, r.ID); n != 1 {
		t.Errorf("EXPIRED events = %d, want 1", n)
	}
}

func TestTTLExpiryObservedUnderLock(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 1)
	approverA := f.userWithRole(t, "approver")
	r := f.submitted(t, connID, 1)
	if _, err := f.requests.Approve(ctx, f.org, r.ID, approverA, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, r.ID)); err != nil {
		t.Fatalf("approve: %v", err)
	}

	if _, err := f.pool.Exec(ctx, `update access_requests set expires_at = now() + interval '300 milliseconds' where id = $1`, string(r.ID)); err != nil {
		t.Fatal(err)
	}

	before := time.Now().UTC()

	holder, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	if _, err := holder.Exec(ctx, `select id from access_requests where id = $1 for update`, string(r.ID)); err != nil {
		t.Fatalf("hold row: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := f.requests.Cancel(ctx, f.org, r.ID, f.requester, reqEvent(audit.ActionAccessRequestCancelled, r.ID))
		done <- err
	}()
	time.Sleep(600 * time.Millisecond)
	if err := holder.Rollback(ctx); err != nil {
		t.Fatalf("release row: %v", err)
	}

	if err := <-done; !errors.Is(err, access.ErrNotCancellable) {
		t.Errorf("cancel racing the TTL = %v, want ErrNotCancellable", err)
	}
	got, _, err := f.requests.GetSealed(ctx, f.org, r.ID)
	if err != nil || got.State != access.StateExpired || got.Reason != access.ReasonTTLExpired {
		t.Errorf("after the race = %+v, %v; want expired/ttl_expired", got, err)
	}
	if n := f.countEvents(t, audit.ActionAccessRequestExpired, r.ID); n != 1 {
		t.Errorf("EXPIRED events = %d, want 1", n)
	}
	if n := f.countEvents(t, audit.ActionAccessRequestCancelled, r.ID); n != 0 {
		t.Errorf("CANCELLED events = %d, want 0 — a request the TTL took must not read as withdrawn", n)
	}
	// The observation's own timestamps must sit on the same axis as the deadline it observed. now() is the TRANSACTION's start time, which in this race precedes expires_at, so a row or event stamped with it would tell the timeline the request expired BEFORE its deadline.
	var expiresAt, updatedAt, occurredAt time.Time
	if err := f.pool.QueryRow(ctx,
		`select r.expires_at, r.updated_at, e.occurred_at
		   from access_requests r
		   join audit_events e on e.target_id = r.id::text and e.action = $2
		  where r.id = $1`,
		string(r.ID), string(audit.ActionAccessRequestExpired)).Scan(&expiresAt, &updatedAt, &occurredAt); err != nil {
		t.Fatalf("read timestamps: %v", err)
	}
	if updatedAt.Before(expiresAt) {
		t.Errorf("updated_at %s precedes expires_at %s — the row says it expired before its deadline", updatedAt, expiresAt)
	}
	if occurredAt.Before(expiresAt) {
		t.Errorf("occurred_at %s precedes expires_at %s — the audit event predates the expiry it reports", occurredAt, expiresAt)
	}
	// ONE instant, not two clock reads: the row and its event must agree exactly. Two separate clock_timestamp() calls would drift by microseconds and leave the timeline unable to say which came first.
	if !occurredAt.Equal(updatedAt) {
		t.Errorf("occurred_at %s != updated_at %s — the observation must be stamped from a single instant", occurredAt, updatedAt)
	}

	// The explicit stamp is for DERIVED events only: an ordinary event still takes the column default, so a caller cannot dictate audit time on the normal path (ADR-0009). The cancelled-draft path below is such an event.
	other := f.draft(t, connID)
	if _, err := f.requests.Cancel(ctx, f.org, other.ID, f.requester, reqEvent(audit.ActionAccessRequestCancelled, other.ID)); err != nil {
		t.Fatalf("cancel a draft: %v", err)
	}
	var cancelledAt time.Time
	if err := f.pool.QueryRow(ctx,
		`select occurred_at from audit_events where target_id = $1 and action = $2`,
		string(other.ID), string(audit.ActionAccessRequestCancelled)).Scan(&cancelledAt); err != nil {
		t.Fatalf("read cancel event: %v", err)
	}
	if cancelledAt.Before(before) || cancelledAt.After(time.Now().UTC().Add(time.Minute)) {
		t.Errorf("ordinary event occurred_at = %s, want the server's own clock around now", cancelledAt)
	}
}

func TestDecisionRequiresTheActionsOwnPermission(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	rejectOnly := f.userWithCustomRole(t, "reject-only", "requests.reject")

	rejectable := f.submitted(t, f.liveConn(t, 1), 1)
	if _, err := f.requests.Reject(ctx, f.org, rejectable.ID, rejectOnly, "not this one", reqEvent(audit.ActionAccessRequestRejected, rejectable.ID)); err != nil {
		t.Fatalf("reject by a reject-only role: %v", err)
	}

	approvable := f.submitted(t, f.liveConn(t, 1), 1)
	if _, err := f.requests.Approve(ctx, f.org, approvable.ID, rejectOnly, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, approvable.ID)); !errors.Is(err, access.ErrApproverIneligible) {
		t.Errorf("approve by a reject-only role = %v, want ErrApproverIneligible", err)
	}
	got, _, err := f.requests.GetSealed(ctx, f.org, approvable.ID)
	if err != nil || got.State != access.StatePending {
		t.Errorf("state after the refused approve = %v, %v; want still pending", got.State, err)
	}
	var approvals int
	if err := f.pool.QueryRow(ctx, `select count(*) from approvals where request_id = $1`, string(approvable.ID)).Scan(&approvals); err != nil {
		t.Fatal(err)
	}
	if approvals != 0 {
		t.Errorf("refused approve left %d approval rows, want 0", approvals)
	}
}

func TestDecisionOnAForeignOrganizationIsNotFound(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	approver := f.userWithRole(t, "approver")
	r := f.submitted(t, f.liveConn(t, 1), 1)
	elsewhere := identity.OrganizationID(uuid.NewString())

	if _, err := f.requests.Approve(ctx, elsewhere, r.ID, approver, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, r.ID)); !errors.Is(err, access.ErrNotFound) {
		t.Errorf("approve across organizations = %v, want ErrNotFound", err)
	}
	if _, err := f.requests.Reject(ctx, elsewhere, r.ID, approver, "no", reqEvent(audit.ActionAccessRequestRejected, r.ID)); !errors.Is(err, access.ErrNotFound) {
		t.Errorf("reject across organizations = %v, want ErrNotFound", err)
	}
	if got, _, err := f.requests.GetSealed(ctx, f.org, r.ID); err != nil || got.State != access.StatePending {
		t.Errorf("state after the foreign decisions = %v, %v; want untouched pending", got.State, err)
	}
}

func TestManualDecisionTimestampsAgree(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 1)
	approverA := f.userWithRole(t, "approver")
	r := f.submitted(t, connID, 1)
	const validity = time.Hour

	holder, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	if _, err := holder.Exec(ctx, `select id from access_requests where id = $1 for update`, string(r.ID)); err != nil {
		t.Fatalf("hold row: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := f.requests.Approve(ctx, f.org, r.ID, approverA, "ok", validity, reqEvent(audit.ActionAccessRequestApproved, r.ID))
		done <- err
	}()
	time.Sleep(500 * time.Millisecond)
	txStart := time.Now().UTC()
	if err := holder.Rollback(ctx); err != nil {
		t.Fatalf("release row: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("approve: %v", err)
	}

	var decidedAt, updatedAt, occurredAt, expiresAt time.Time
	if err := f.pool.QueryRow(ctx,
		`select a.decided_at, r.updated_at, e.occurred_at, r.expires_at
		   from access_requests r
		   join approvals a on a.request_id = r.id
		   join audit_events e on e.target_id = r.id::text and e.action = $2
		  where r.id = $1`,
		string(r.ID), string(audit.ActionAccessRequestApproved)).Scan(&decidedAt, &updatedAt, &occurredAt, &expiresAt); err != nil {
		t.Fatalf("read decision timestamps: %v", err)
	}
	if !decidedAt.Equal(updatedAt) || !decidedAt.Equal(occurredAt) {
		t.Errorf("decision instants disagree: decided=%s updated=%s occurred=%s", decidedAt, updatedAt, occurredAt)
	}
	if diff := expiresAt.Sub(decidedAt); diff < validity-time.Second || diff > validity+time.Second {
		t.Errorf("expires_at - decided_at = %s, want ~%s", diff, validity)
	}

	if decidedAt.UTC().Before(txStart.Add(-100 * time.Millisecond)) {
		t.Errorf("decided_at %s looks like the transaction start, not the moment after the %s lock wait", decidedAt, 500*time.Millisecond)
	}
}

func TestRejectionAndSubQuorumTimestamps(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	approverA := f.userWithRole(t, "approver")
	approverB := f.userWithRole(t, "approver")

	rejected := f.submitted(t, f.liveConn(t, 1), 1)
	if _, err := f.requests.Reject(ctx, f.org, rejected.ID, approverA, "no", reqEvent(audit.ActionAccessRequestRejected, rejected.ID)); err != nil {
		t.Fatalf("reject: %v", err)
	}
	var decidedAt, occurredAt time.Time
	var expires *time.Time
	if err := f.pool.QueryRow(ctx,
		`select a.decided_at, e.occurred_at, r.expires_at
		   from access_requests r
		   join approvals a on a.request_id = r.id
		   join audit_events e on e.target_id = r.id::text and e.action = $2
		  where r.id = $1`,
		string(rejected.ID), string(audit.ActionAccessRequestRejected)).Scan(&decidedAt, &occurredAt, &expires); err != nil {
		t.Fatalf("read rejection timestamps: %v", err)
	}
	if !decidedAt.Equal(occurredAt) {
		t.Errorf("rejection instants disagree: decided=%s occurred=%s", decidedAt, occurredAt)
	}
	if expires != nil {
		t.Errorf("a rejected request carries expires_at = %v, want none", expires)
	}

	partial := f.submitted(t, f.liveConn(t, 2), 2)
	view, err := f.requests.Approve(ctx, f.org, partial.ID, approverA, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, partial.ID))
	if err != nil {
		t.Fatalf("first approve: %v", err)
	}
	if view.Request.State != access.StatePending || view.Request.ExpiresAt != nil {
		t.Errorf("after 1 of 2 approvals: state=%s expires=%v; want pending with no window", view.Request.State, view.Request.ExpiresAt)
	}

	if _, err := f.requests.Approve(ctx, f.org, partial.ID, approverA, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, partial.ID)); !errors.Is(err, access.ErrAlreadyDecided) {
		t.Errorf("duplicate approval = %v, want ErrAlreadyDecided", err)
	}
	if got, _, err := f.requests.GetSealed(ctx, f.org, partial.ID); err != nil || got.State != access.StatePending {
		t.Errorf("state after the duplicate = %v, %v; want still pending", got.State, err)
	}

	final, err := f.requests.Approve(ctx, f.org, partial.ID, approverB, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, partial.ID))
	if err != nil {
		t.Fatalf("second approve: %v", err)
	}
	if final.Request.State != access.StateApproved || final.Request.ExpiresAt == nil {
		t.Errorf("after 2 of 2: state=%s expires=%v; want approved with a window", final.Request.State, final.Request.ExpiresAt)
	}

	if _, err := f.requests.Reject(ctx, f.org, rejected.ID, approverB, "again", reqEvent(audit.ActionAccessRequestRejected, rejected.ID)); !errors.Is(err, access.ErrNotPending) {
		t.Errorf("rejecting a settled request = %v, want ErrNotPending", err)
	}
}

func TestTTLNotYetDueIsNotObserved(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 1)
	approverA := f.userWithRole(t, "approver")
	r := f.submitted(t, connID, 1)
	if _, err := f.requests.Approve(ctx, f.org, r.ID, approverA, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, r.ID)); err != nil {
		t.Fatalf("approve: %v", err)
	}

	view, err := f.requests.Cancel(ctx, f.org, r.ID, f.requester, reqEvent(audit.ActionAccessRequestCancelled, r.ID))
	if err != nil {
		t.Fatalf("cancel inside the window: %v", err)
	}
	if view.Request.State != access.StateCancelled {
		t.Errorf("state = %s, want cancelled — the window had not passed", view.Request.State)
	}
	if n := f.countEvents(t, audit.ActionAccessRequestExpired, r.ID); n != 0 {
		t.Errorf("EXPIRED events = %d, want 0 for a request inside its window", n)
	}
	if n := f.countEvents(t, audit.ActionAccessRequestCancelled, r.ID); n != 1 {
		t.Errorf("CANCELLED events = %d, want 1", n)
	}
}

func TestTTLExpiryOnDecisionPathIsRecordedOnce(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 1)
	approverA := f.userWithRole(t, "approver")
	approverB := f.userWithRole(t, "approver")
	r := f.submitted(t, connID, 1)
	if _, err := f.requests.Approve(ctx, f.org, r.ID, approverA, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, r.ID)); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `update access_requests set expires_at = now() + interval '300 milliseconds' where id = $1`, string(r.ID)); err != nil {
		t.Fatal(err)
	}

	holder, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	if _, err := holder.Exec(ctx, `select id from access_requests where id = $1 for update`, string(r.ID)); err != nil {
		t.Fatalf("hold row: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := f.requests.Approve(ctx, f.org, r.ID, approverB, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, r.ID))
		done <- err
	}()
	time.Sleep(600 * time.Millisecond)
	if err := holder.Rollback(ctx); err != nil {
		t.Fatalf("release row: %v", err)
	}
	if err := <-done; !errors.Is(err, access.ErrNotPending) {
		t.Errorf("approve racing the TTL = %v, want ErrNotPending", err)
	}

	var expiresAt, updatedAt, occurredAt time.Time
	var prev, next string
	var actorService *string
	if err := f.pool.QueryRow(ctx,
		`select r.expires_at, r.updated_at, e.occurred_at, e.previous_state, e.next_state, e.actor_service
		   from access_requests r
		   join audit_events e on e.target_id = r.id::text and e.action = $2
		  where r.id = $1`,
		string(r.ID), string(audit.ActionAccessRequestExpired)).Scan(&expiresAt, &updatedAt, &occurredAt, &prev, &next, &actorService); err != nil {
		t.Fatalf("read expiry row/event: %v", err)
	}
	if occurredAt.Before(expiresAt) || updatedAt.Before(expiresAt) || !occurredAt.Equal(updatedAt) {
		t.Errorf("timestamps off the deadline axis: expires=%s updated=%s occurred=%s", expiresAt, updatedAt, occurredAt)
	}
	// Setting occurred_at explicitly must not cost the derived event its identity.
	if prev != string(access.StateApproved) || next != string(access.StateExpired) {
		t.Errorf("expiry event transition = %s→%s, want approved→expired", prev, next)
	}
	if actorService == nil || *actorService != access.ActorTTLExpiry {
		t.Errorf("expiry event actor_service = %v, want %q", actorService, access.ActorTTLExpiry)
	}

	// Observing again must be a no-op: the UPDATE is guarded on state='approved', so a second touch cannot append a duplicate expiry to the trail.
	if _, err := f.requests.Approve(ctx, f.org, r.ID, approverB, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, r.ID)); !errors.Is(err, access.ErrNotPending) {
		t.Errorf("second approve = %v, want ErrNotPending", err)
	}
	if n := f.countEvents(t, audit.ActionAccessRequestExpired, r.ID); n != 1 {
		t.Errorf("EXPIRED events after a second touch = %d, want 1", n)
	}
}

func TestAutoApprovalEventAgreesWithTheRow(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 0)
	r := f.draft(t, connID)
	submittedAt := time.Now().UTC().Truncate(time.Microsecond)
	sub, err := r.Submitted(submitSnapshot(f.pin(t, connID), 0), submittedAt)
	if err != nil {
		t.Fatal(err)
	}
	const validity = time.Hour

	stale, err := sub.Approved(time.Now().UTC().Add(-2*validity), validity)
	if err != nil {
		t.Fatal(err)
	}
	approvedEvt := reqEvent(audit.ActionAccessRequestApproved, r.ID)
	approvedEvt.ActorType = audit.ActorSystem
	approvedEvt.ActorService = access.ActorAutoApproval
	approvedEvt.PreviousState = string(access.StatePending)
	approvedEvt.NextState = string(access.StateApproved)
	approvedEvt.Metadata = map[string]any{
		"connection_id":      string(connID),
		"required_approvals": 0,
		"expires_at":         stale.ExpiresAt.UTC().Format(time.RFC3339),
		"redacted_sql":       stale.RedactedSQL,
	}

	holder, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	if _, err := holder.Exec(ctx, `select id from connections where id = $1 for update`, string(connID)); err != nil {
		t.Fatalf("hold connection: %v", err)
	}
	type result struct {
		view access.RequestView
		err  error
	}
	done := make(chan result, 1)
	go func() {
		v, err := f.requests.Submit(ctx, stale, 1, validity,
			reqEvent(audit.ActionAccessRequestSubmitted, r.ID), approvedEvt)
		done <- result{v, err}
	}()
	time.Sleep(600 * time.Millisecond)
	if err := holder.Rollback(ctx); err != nil {
		t.Fatalf("release connection: %v", err)
	}
	got := <-done
	if got.err != nil {
		t.Fatalf("Submit: %v", got.err)
	}
	if got.view.Request.State != access.StateApproved || got.view.Request.ExpiresAt == nil {
		t.Fatalf("auto-approval landed as %+v", got.view.Request)
	}
	rowExpires := got.view.Request.ExpiresAt.UTC()

	var occurredAt time.Time
	var metaRaw []byte
	if err := f.pool.QueryRow(ctx,
		`select occurred_at, metadata from audit_events where target_id = $1 and action = $2`,
		string(r.ID), string(audit.ActionAccessRequestApproved)).Scan(&occurredAt, &metaRaw); err != nil {
		t.Fatalf("read approval event: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		t.Fatalf("metadata json: %v", err)
	}
	metaExpires, ok := meta["expires_at"].(string)
	if !ok {
		t.Fatalf("metadata expires_at missing: %v", meta)
	}
	parsed, err := time.Parse(time.RFC3339, metaExpires)
	if err != nil {
		t.Fatalf("metadata expires_at %q is not RFC3339: %v", metaExpires, err)
	}

	if !parsed.UTC().Truncate(time.Second).Equal(rowExpires.Truncate(time.Second)) {
		t.Errorf("metadata expires_at %s disagrees with the row's %s", parsed.UTC(), rowExpires)
	}

	if diff := rowExpires.Sub(occurredAt.UTC()); diff < validity-time.Second || diff > validity+time.Second {
		t.Errorf("expires_at - occurred_at = %s, want ~%s (one instant for both)", diff, validity)
	}

	var rowUpdated time.Time
	if err := f.pool.QueryRow(ctx,
		`select updated_at from access_requests where id = $1`, string(r.ID)).Scan(&rowUpdated); err != nil {
		t.Fatalf("read updated_at: %v", err)
	}
	if !rowUpdated.UTC().Equal(occurredAt.UTC()) {
		t.Errorf("updated_at %s != the approval instant %s — the row and its event were stamped by different clocks",
			rowUpdated.UTC(), occurredAt.UTC())
	}
	// And that instant is after the lock wait, not the transaction's start: the gap to submitted_at must cover the wait (tolerant of container clock skew).
	if gap := occurredAt.UTC().Sub(submittedAt); gap < 300*time.Millisecond {
		t.Errorf("occurred_at is only %s after submitted_at — it looks like the pre-lock instant", gap)
	}
}

func TestAutoApprovalTTLStampedUnderLock(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 0)
	r := f.draft(t, connID)
	sub, err := r.Submitted(submitSnapshot(f.pin(t, connID), 0), time.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		t.Fatal(err)
	}
	const validity = time.Hour

	stale, err := sub.Approved(time.Now().UTC().Add(-2*validity), validity)
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC()

	view, err := f.requests.Submit(ctx, stale, 1, validity, reqEvent(audit.ActionAccessRequestSubmitted, r.ID))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if view.Request.State != access.StateApproved {
		t.Fatalf("state = %s, want approved", view.Request.State)
	}
	if view.Request.ExpiresAt == nil || !view.Request.ExpiresAt.After(before.Add(validity-time.Minute)) {
		t.Errorf("expires_at = %v, want ~%s of validity left from the commit, not the stale instant", view.Request.ExpiresAt, validity)
	}
	// The window opens at the approval, so it cannot start before the request was even submitted, and it must not run past the validity from this moment.
	if view.Request.SubmittedAt == nil || !view.Request.ExpiresAt.After(*view.Request.SubmittedAt) {
		t.Errorf("expires_at %v is not after submitted_at %v", view.Request.ExpiresAt, view.Request.SubmittedAt)
	}
	if view.Request.ExpiresAt.After(time.Now().UTC().Add(validity)) {
		t.Errorf("expires_at = %v, want no later than now+%s — the window may not be extended", view.Request.ExpiresAt, validity)
	}
}

func TestPendingSubmitCarriesNoExpiry(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 1)
	r := f.draft(t, connID)
	sub, err := r.Submitted(submitSnapshot(f.pin(t, connID), 1), time.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		t.Fatal(err)
	}
	bogus := time.Now().UTC().Add(72 * time.Hour)
	sub.ExpiresAt = &bogus

	view, err := f.requests.Submit(ctx, sub, 1, time.Hour, reqEvent(audit.ActionAccessRequestSubmitted, r.ID))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if view.Request.State != access.StatePending {
		t.Fatalf("state = %s, want pending", view.Request.State)
	}
	if view.Request.ExpiresAt != nil {
		t.Errorf("pending expires_at = %v, want none", view.Request.ExpiresAt)
	}
	// And the effective state must not read as expired off a stray timestamp.
	if got, _ := view.Request.EffectiveState(time.Now().UTC().Add(96 * time.Hour)); got != access.StatePending {
		t.Errorf("effective state far in the future = %s, want pending", got)
	}
}

func TestApprovalReadsAreOrgScoped(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 1)
	approverA := f.userWithRole(t, "approver")
	r := f.submitted(t, connID, 1)
	if _, err := f.requests.Approve(ctx, f.org, r.ID, approverA, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, r.ID)); err != nil {
		t.Fatalf("approve: %v", err)
	}

	uid := func(s string) pgtype.UUID {
		t.Helper()
		var u pgtype.UUID
		if err := u.Scan(s); err != nil {
			t.Fatalf("uuid %q: %v", s, err)
		}
		return u
	}
	q := db.New(f.pool)
	rid, requester := uid(string(r.ID)), uid(string(f.requester))
	elsewhere := uid(uuid.NewString())

	for _, tc := range []struct {
		name string
		org  pgtype.UUID
		want int64
	}{
		{name: "own organization", org: uid(string(f.org)), want: 1},
		{name: "another organization", org: elsewhere, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count, err := q.CountValidApprovals(ctx, db.CountValidApprovalsParams{
				RequestID: rid, OrganizationID: tc.org, RequesterID: requester,
			})
			if err != nil || count != tc.want {
				t.Errorf("CountValidApprovals = %d, %v; want %d", count, err, tc.want)
			}
			rows, err := q.ListApprovalsForRequest(ctx, db.ListApprovalsForRequestParams{
				RequestID: rid, OrganizationID: tc.org, RequesterID: requester,
			})
			if err != nil || int64(len(rows)) != tc.want {
				t.Errorf("ListApprovalsForRequest = %d rows, %v; want %d", len(rows), err, tc.want)
			}
		})
	}
}

func TestListClampsInsideOneSnapshot(t *testing.T) {
	f := newReqFixtureFresh(t)
	ctx := context.Background()
	connID := f.liveConn(t, 1)
	for range 5 {
		f.draft(t, connID)
	}
	pending := f.submitted(t, connID, 1)

	for _, tc := range []struct {
		name      string
		query     access.ListQuery
		wantItems int
		wantPage  int
		wantTotal int64
	}{
		{"far past the end lands on the last page", access.ListQuery{Page: 9, PageSize: 2}, 2, 3, 6},
		{"one past the end lands on the last page", access.ListQuery{Page: 4, PageSize: 2}, 2, 3, 6},
		{"a partial last page keeps its rows", access.ListQuery{Page: 7, PageSize: 4}, 2, 2, 6},
		{"in range is untouched", access.ListQuery{Page: 2, PageSize: 2}, 2, 2, 6},

		{"filtered set clamps to ITS last page", access.ListQuery{Page: 5, PageSize: 1, State: access.StatePending}, 1, 1, 1},

		{"empty result reports page 1", access.ListQuery{Page: 5, PageSize: 2, State: access.StateRejected}, 0, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := f.requests.List(ctx, f.org, tc.query)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(page.Items) != tc.wantItems || page.Page != tc.wantPage || page.TotalCount != tc.wantTotal {
				t.Errorf("page = %d items on page %d / total %d, want %d / %d / %d",
					len(page.Items), page.Page, page.TotalCount, tc.wantItems, tc.wantPage, tc.wantTotal)
			}
		})
	}
	_ = pending
}

func TestGetReadsTheRequestAndItsApprovalsTogether(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	approver := f.userWithRole(t, "approver")
	r := f.submitted(t, f.liveConn(t, 2), 2)

	var once sync.Once
	traced := f.tracedPool(t, func(sql string) {
		// Anchor on a column only ListApprovalsForRequest selects: the view query also reads public.approvals (its count subquery), and firing there would insert BEFORE the first read — both statements would then agree and the race would go untested.
		if !strings.Contains(sql, "u.email as approver_email") {
			return
		}
		once.Do(func() {
			if _, err := f.pool.Exec(ctx,
				`insert into approvals (request_id, organization_id, approver_id, decision, reason)
				 values ($1, $2, $3, 'approved', '')`,
				string(r.ID), string(f.org), string(approver)); err != nil {
				t.Errorf("concurrent approval: %v", err)
			}
		})
	})

	view, _, err := pg.NewAccessRequestStore(traced).Get(ctx, f.org, r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if view.ValidApprovals != len(view.Approvals) {
		t.Errorf("valid-approval count %d disagrees with %d listed approvals — the two reads saw different states",
			view.ValidApprovals, len(view.Approvals))
	}

	settled, _, err := f.requests.Get(ctx, f.org, r.ID)
	if err != nil {
		t.Fatalf("Get again: %v", err)
	}
	if settled.ValidApprovals != 1 || len(settled.Approvals) != 1 {
		t.Errorf("settled view = %d valid / %d listed, want 1/1", settled.ValidApprovals, len(settled.Approvals))
	}
}

// tracedPool clones the fixture's pool with a query tracer attached, so a test can act at a precise point between a store method's statements without the store knowing anything about it (pgx's public tracing API).
func (f reqFixture) tracedPool(t *testing.T, beforeQuery func(sql string)) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(f.pool.Config().ConnString())
	if err != nil {
		t.Fatalf("parse pool config: %v", err)
	}
	cfg.ConnConfig.Tracer = queryTracer{before: beforeQuery}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("traced pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type queryTracer struct{ before func(sql string) }

func (q queryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	q.before(data.SQL)
	return ctx
}

func (queryTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestViewRowsCarryTheEffectiveState(t *testing.T) {
	f := newReqFixtureFresh(t)
	ctx := context.Background()
	connID := f.liveConn(t, 1)
	approver := f.userWithRole(t, "approver")

	live := f.submitted(t, connID, 1)
	if _, err := f.requests.Approve(ctx, f.org, live.ID, approver, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, live.ID)); err != nil {
		t.Fatalf("approve live: %v", err)
	}
	overdue := f.submitted(t, connID, 1)
	if _, err := f.requests.Approve(ctx, f.org, overdue.ID, approver, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, overdue.ID)); err != nil {
		t.Fatalf("approve overdue: %v", err)
	}
	// Past its window, but nothing has touched it — the stored state still says approved (lazy expiry), which is exactly when the badge must not.
	if _, err := f.pool.Exec(ctx, `update access_requests set expires_at = now() - interval '1 minute' where id = $1`, string(overdue.ID)); err != nil {
		t.Fatal(err)
	}

	byID := func(page access.RequestPage) map[access.RequestID]access.RequestView {
		out := map[access.RequestID]access.RequestView{}
		for _, v := range page.Items {
			out[v.Request.ID] = v
		}
		return out
	}

	all, err := f.requests.List(ctx, f.org, access.ListQuery{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	views := byID(all)
	if got := views[overdue.ID].EffectiveState; got != access.StateExpired {
		t.Errorf("overdue badge = %q, want expired", got)
	}
	if got := views[overdue.ID].EffectiveReason; got != access.ReasonTTLExpired {
		t.Errorf("overdue badge reason = %q, want ttl_expired", got)
	}
	if got := views[live.ID].EffectiveState; got != access.StateApproved {
		t.Errorf("live badge = %q, want approved — the window is still open", got)
	}

	if stored, _, err := f.requests.GetSealed(ctx, f.org, overdue.ID); err != nil || stored.State != access.StateApproved {
		t.Errorf("stored state = %v, %v; want still approved (lazy expiry)", stored.State, err)
	}

	expiredPage, err := f.requests.List(ctx, f.org, access.ListQuery{Page: 1, PageSize: 50, State: access.StateExpired})
	if err != nil {
		t.Fatalf("List expired: %v", err)
	}
	if _, ok := byID(expiredPage)[overdue.ID]; !ok || expiredPage.TotalCount != 1 {
		t.Errorf("expired filter = %d rows / total %d, want the overdue one", len(expiredPage.Items), expiredPage.TotalCount)
	}
	approvedPage, err := f.requests.List(ctx, f.org, access.ListQuery{Page: 1, PageSize: 50, State: access.StateApproved})
	if err != nil {
		t.Fatalf("List approved: %v", err)
	}
	if _, ok := byID(approvedPage)[overdue.ID]; ok {
		t.Error("the approved filter returned the overdue request")
	}
	if got := byID(approvedPage)[live.ID].EffectiveState; got != access.StateApproved {
		t.Errorf("approved filter badge = %q, want approved", got)
	}

	detail, _, err := f.requests.Get(ctx, f.org, overdue.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if detail.EffectiveState != access.StateExpired {
		t.Errorf("detail badge = %q, want expired (the list says expired)", detail.EffectiveState)
	}
}

func TestAccessRequestListTotalsAreOneSnapshot(t *testing.T) {
	f := newReqFixtureFresh(t)
	ctx := context.Background()
	connID := f.liveConn(t, 1)
	for range 5 {
		f.draft(t, connID)
	}

	submitted := f.submitted(t, connID, 1)

	for _, tc := range []struct {
		name      string
		query     access.ListQuery
		wantItems int
		wantTotal int64
	}{
		{"first page of several", access.ListQuery{Page: 1, PageSize: 2}, 2, 6},
		{"middle page", access.ListQuery{Page: 2, PageSize: 2}, 2, 6},
		{"last partial page", access.ListQuery{Page: 2, PageSize: 4}, 2, 6},
		{"whole set on one page", access.ListQuery{Page: 1, PageSize: 50}, 6, 6},
		{"filtered", access.ListQuery{Page: 1, PageSize: 50, State: access.StatePending}, 1, 1},
		{"filter matching nothing", access.ListQuery{Page: 1, PageSize: 50, State: access.StateRejected}, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := f.requests.List(ctx, f.org, tc.query)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(page.Items) != tc.wantItems || page.TotalCount != tc.wantTotal {
				t.Errorf("page = %d items / total %d, want %d / %d",
					len(page.Items), page.TotalCount, tc.wantItems, tc.wantTotal)
			}
		})
	}
	_ = submitted
}

func TestAccessRequestListPagination(t *testing.T) {

	f := newReqFixtureFresh(t)
	ctx := context.Background()
	connID := f.liveConn(t, 1)
	other := f.userWithRole(t, "requester")

	for range 3 {
		f.draft(t, connID)
	}
	foreign, err := access.NewDraft(access.RequestID(uuid.NewString()), f.org, connID, other, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.requests.CreateDraft(ctx, foreign, sealedPayloadStub(), reqEvent(audit.ActionAccessRequestCreated, foreign.ID)); err != nil {
		t.Fatal(err)
	}

	page, err := f.requests.List(ctx, f.org, access.ListQuery{Page: 1, PageSize: 2, SortDescending: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.TotalCount != 4 || len(page.Items) != 2 {
		t.Errorf("page = total %d items %d, want 4/2", page.TotalCount, len(page.Items))
	}
	mine, err := f.requests.List(ctx, f.org, access.ListQuery{Page: 1, PageSize: 10, SortDescending: true, RequesterID: f.requester})
	if err != nil {
		t.Fatal(err)
	}
	if mine.TotalCount != 3 {
		t.Errorf("own scope total = %d, want 3", mine.TotalCount)
	}
	drafts, err := f.requests.List(ctx, f.org, access.ListQuery{Page: 1, PageSize: 10, State: access.StateDraft})
	if err != nil {
		t.Fatal(err)
	}
	if drafts.TotalCount != 4 {
		t.Errorf("state filter total = %d, want 4", drafts.TotalCount)
	}
}

func TestListEffectiveStateAndValidCount(t *testing.T) {
	f := newReqFixtureFresh(t)
	ctx := context.Background()
	connID := f.liveConn(t, 2)
	approverA := f.userWithRole(t, "approver")

	pending := f.submitted(t, connID, 2)
	if _, err := f.requests.Approve(ctx, f.org, pending.ID, approverA, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, pending.ID)); err != nil {
		t.Fatalf("approve: %v", err)
	}
	page, err := f.requests.List(ctx, f.org, access.ListQuery{Page: 1, PageSize: 10, State: access.StatePending})
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalCount != 1 || len(page.Items) != 1 || page.Items[0].ValidApprovals != 1 {
		t.Errorf("pending list = total %d, valid %d; want 1 request with 1 valid approval", page.TotalCount, listValid(page))
	}

	// A second request auto-approved (quorum 0) then forced overdue must read as expired, not approved.
	auto := f.submitted(t, f.liveConn(t, 0), 0)
	if _, err := f.pool.Exec(ctx, `update access_requests set state='approved', expires_at = now() - interval '1 minute' where id = $1`, string(auto.ID)); err != nil {
		t.Fatal(err)
	}
	approved, err := f.requests.List(ctx, f.org, access.ListQuery{Page: 1, PageSize: 10, State: access.StateApproved})
	if err != nil {
		t.Fatal(err)
	}
	if approved.TotalCount != 0 {
		t.Errorf("overdue-approved appears under Approved filter: total %d, want 0", approved.TotalCount)
	}
	expired, err := f.requests.List(ctx, f.org, access.ListQuery{Page: 1, PageSize: 10, State: access.StateExpired})
	if err != nil {
		t.Fatal(err)
	}
	if expired.TotalCount != 1 {
		t.Errorf("overdue-approved missing from Expired filter: total %d, want 1", expired.TotalCount)
	}
}

func listValid(p access.RequestPage) int {
	if len(p.Items) == 0 {
		return -1
	}
	return p.Items[0].ValidApprovals
}

func TestCreateDraftRefusesArchivedConnection(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 1)

	if _, err := f.store.Archive(ctx, f.org, connID, connEvent(audit.ActionConnectionArchived, connID)); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	r, err := access.NewDraft(access.RequestID(uuid.NewString()), f.org, connID, f.requester, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.requests.CreateDraft(ctx, r, sealedPayloadStub(), reqEvent(audit.ActionAccessRequestCreated, r.ID)); !errors.Is(err, access.ErrConnectionArchived) {
		t.Errorf("CreateDraft on an archived connection = %v, want ErrConnectionArchived", err)
	}
	if _, _, err := f.requests.GetSealed(ctx, f.org, r.ID); !errors.Is(err, access.ErrNotFound) {
		t.Errorf("a refused draft must not persist: %v", err)
	}
}

func TestSubmitRefusesSupersededPolicyPin(t *testing.T) {
	f := newReqFixture(t)
	ctx := context.Background()
	connID := f.liveConn(t, 1)
	r := f.draft(t, connID)

	pinned := f.pin(t, connID)

	next := validNextPolicy(t, pinned.Policy, f.user)
	if _, err := f.policies.UpdatePolicy(ctx, next, pinned.Policy.Version, connEvent(audit.ActionConnectionPolicyUpdated, connID)); err != nil {
		t.Fatalf("UpdatePolicy: %v", err)
	}

	sub, err := r.Submitted(submitSnapshot(pinned, 1), time.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.requests.Submit(ctx, sub, 1, time.Hour, reqEvent(audit.ActionAccessRequestSubmitted, r.ID)); !errors.Is(err, connection.ErrPolicyConflict) {
		t.Errorf("Submit with a superseded pin = %v, want connection.ErrPolicyConflict", err)
	}
	if got, _, err := f.requests.GetSealed(ctx, f.org, r.ID); err != nil || got.State != access.StateDraft {
		t.Errorf("state after a refused submit = %v, %v; want draft (nothing landed)", got.State, err)
	}
}
