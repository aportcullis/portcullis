package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// TestAccessRequestDecisionUUIDForms preserves parsed identities and refusal atomicity at the PostgreSQL port.
func TestAccessRequestDecisionUUIDForms(t *testing.T) {
	scenarios := []struct {
		name             string
		requestID        access.RequestID
		formatRequest    func(string) string
		formatPrincipals func(string) string
		reject           bool
		self             bool
		requestOverride  access.RequestID
		actorOverride    identity.UserID
		foreignOrg       identity.OrganizationID
		wantError        error
		wantErrorPrefix  string
	}{
		{name: "uppercase request approves", requestID: "abcdefab-cdef-4abc-8def-abcdefabc001", formatRequest: strings.ToUpper},
		{name: "compact request rejects", requestID: "abcdefab-cdef-4abc-8def-abcdefabc002", formatRequest: compactDecisionUUID, reject: true},
		{name: "uppercase organization and actor approve", requestID: "abcdefab-cdef-4abc-8def-abcdefabc003", formatPrincipals: strings.ToUpper},
		{name: "compact organization and actor reject", requestID: "abcdefab-cdef-4abc-8def-abcdefabc004", formatPrincipals: compactDecisionUUID, reject: true},
		{name: "compact requester cannot self approve", requestID: "abcdefab-cdef-4abc-8def-abcdefabc005", formatPrincipals: compactDecisionUUID, self: true, wantError: access.ErrSelfApproval},
		{name: "malformed request refuses", requestID: "abcdefab-cdef-4abc-8def-abcdefabc006", requestOverride: "not-a-request-uuid", wantErrorPrefix: "request id:"},
		{name: "malformed actor refuses", requestID: "abcdefab-cdef-4abc-8def-abcdefabc007", actorOverride: "not-an-actor-uuid", wantErrorPrefix: "approver id:"},
		{name: "foreign organization refuses", requestID: "abcdefab-cdef-4abc-8def-abcdefabc008", foreignOrg: "ffffffff-ffff-4fff-8fff-ffffffffffff", wantError: access.ErrNotFound},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newReqFixture(t)
			pending := fixture.submitIdentityDecisionRequest(t, scenario.requestID)
			canonicalActor := fixture.userWithRole(t, "approver")
			requestID, organization, actor := pending.ID, fixture.org, canonicalActor
			if scenario.self {
				actor = fixture.requester
			}
			if scenario.formatRequest != nil {
				requestID = access.RequestID(scenario.formatRequest(string(requestID)))
			}
			if scenario.formatPrincipals != nil {
				organization = identity.OrganizationID(scenario.formatPrincipals(string(organization)))
				actor = identity.UserID(scenario.formatPrincipals(string(actor)))
			}
			if scenario.requestOverride != "" {
				requestID = scenario.requestOverride
			}
			if scenario.actorOverride != "" {
				actor = scenario.actorOverride
			}
			if scenario.foreignOrg != "" {
				organization = scenario.foreignOrg
			}
			approvalsBefore, auditsBefore := fixture.countDecisionIdentityEvidence(t, pending.ID)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var view access.RequestView
			var err error
			wantState := access.StateApproved
			decisionAction := audit.ActionAccessRequestApproved
			if scenario.reject {
				wantState = access.StateRejected
				decisionAction = audit.ActionAccessRequestRejected
				view, err = fixture.requests.Reject(ctx, organization, requestID, actor, "reviewed rejection", reqEvent(decisionAction, pending.ID))
			} else {
				view, err = fixture.requests.Approve(ctx, organization, requestID, actor, "reviewed approval", time.Hour, reqEvent(decisionAction, pending.ID))
			}
			approvalsAfter, auditsAfter := fixture.countDecisionIdentityEvidence(t, pending.ID)
			if scenario.wantError != nil || scenario.wantErrorPrefix != "" {
				if err == nil || (scenario.wantError != nil && !errors.Is(err, scenario.wantError)) || (scenario.wantErrorPrefix != "" && !strings.HasPrefix(err.Error(), scenario.wantErrorPrefix)) {
					t.Errorf("decision error = %v, want %v or prefix %q", err, scenario.wantError, scenario.wantErrorPrefix)
				}
				if approvalsAfter != approvalsBefore || auditsAfter != auditsBefore {
					t.Errorf("refusal changed evidence: approvals %d→%d, audits %d→%d", approvalsBefore, approvalsAfter, auditsBefore, auditsAfter)
				}
				stored, _, readErr := fixture.requests.GetSealed(ctx, fixture.org, pending.ID)
				if readErr != nil || stored.State != access.StatePending {
					t.Errorf("state after refusal = %s, %v; want pending", stored.State, readErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("decision with accepted UUID forms: %v", err)
			}
			if view.Request.ID != pending.ID || view.Request.OrganizationID != fixture.org || view.Request.State != wantState || view.EffectiveState != wantState {
				t.Errorf("canonical decision view = %+v", view)
			}
			if len(view.Approvals) != 1 {
				t.Fatalf("approval views = %d, want 1", len(view.Approvals))
			}
			approval := view.Approvals[0].Approval
			if approval.RequestID != pending.ID || approval.OrganizationID != fixture.org || approval.ApproverID != canonicalActor {
				t.Errorf("persisted decision identities = %+v", approval)
			}
			if approvalsAfter != approvalsBefore+1 || auditsAfter != auditsBefore+1 || fixture.countEvents(t, decisionAction, pending.ID) != 1 {
				t.Errorf("successful decision evidence: approvals %d→%d, audits %d→%d", approvalsBefore, approvalsAfter, auditsBefore, auditsAfter)
			}
		})
	}
}

// compactDecisionUUID removes separators without changing UUID identity.
func compactDecisionUUID(identifier string) string {
	return strings.ReplaceAll(identifier, "-", "")
}

// submitIdentityDecisionRequest submits a fixed request identity requiring one approval.
func (f reqFixture) submitIdentityDecisionRequest(t *testing.T, requestID access.RequestID) access.Request {
	t.Helper()
	ctx := context.Background()
	connectionID := f.liveConn(t, 1)
	draft, err := access.NewDraft(requestID, f.org, connectionID, f.requester, time.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.requests.CreateDraft(ctx, draft, sealedPayloadStub(), reqEvent(audit.ActionAccessRequestCreated, draft.ID)); err != nil {
		t.Fatalf("create identity decision draft: %v", err)
	}
	submitted, err := draft.Submitted(submitSnapshot(f.pin(t, connectionID), 1), time.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		t.Fatal(err)
	}
	view, err := f.requests.Submit(ctx, submitted, draft.Version, time.Hour, reqEvent(audit.ActionAccessRequestSubmitted, draft.ID))
	if err != nil {
		t.Fatalf("submit identity decision request: %v", err)
	}
	return view.Request
}

// countDecisionIdentityEvidence reads request-scoped approval and audit row counts.
func (f reqFixture) countDecisionIdentityEvidence(t *testing.T, requestID access.RequestID) (int, int) {
	t.Helper()
	var approvalCount, auditCount int
	if err := f.pool.QueryRow(context.Background(), `select
		(select count(*) from approvals where request_id = $1::uuid),
		(select count(*) from audit_events where target_id = $1::text)`, string(requestID)).Scan(&approvalCount, &auditCount); err != nil {
		t.Fatalf("read identity decision evidence: %v", err)
	}
	return approvalCount, auditCount
}
