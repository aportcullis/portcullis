package accessrequest_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/accessrequest"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// TestApprovalDecisionViews preserves request snapshots, decision views and audit actors through the application port.
func TestApprovalDecisionViews(t *testing.T) {
	t.Parallel()
	thirdApprover := identity.UserID("user-approver-3")
	scenarios := []struct {
		name              string
		previousApprovers []identity.UserID
		approver          identity.UserID
		decision          access.Decision
		reason            string
		wantState         access.State
		wantValidCount    int
	}{
		{
			name:     "below quorum stays pending with its original snapshot",
			approver: approver1, decision: access.DecisionApproved,
			wantState: access.StatePending, wantValidCount: 1,
		},
		{
			name:              "exact quorum starts validity at the deciding approval",
			previousApprovers: []identity.UserID{approver1},
			approver:          approver2, decision: access.DecisionApproved, reason: "reviewed",
			wantState: access.StateApproved, wantValidCount: 2,
		},
		{
			name:              "current valid count above quorum also approves",
			previousApprovers: []identity.UserID{approver1, approver2},
			approver:          thirdApprover, decision: access.DecisionApproved,
			wantState: access.StateApproved, wantValidCount: 3,
		},
		{
			name:              "one reasoned rejection terminates a partially approved request",
			previousApprovers: []identity.UserID{approver1},
			approver:          approver2, decision: access.DecisionRejected, reason: "  restrict the target rows  ",
			wantState: access.StateRejected, wantValidCount: 1,
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			repository := newRepo()
			service := newService(t, repository)
			pending := createPendingDecisionRequest(t, service)
			for _, previousApprover := range scenario.previousApprovers {
				previous, err := access.NewApproval(pending.ID, pending.OrganizationID, previousApprover, pending.RequesterID, access.DecisionApproved, "previous review", t0.Add(-time.Minute))
				if err != nil {
					t.Fatalf("seed previous approval: %v", err)
				}
				repository.approvals[pending.ID] = append(repository.approvals[pending.ID], previous)
			}
			repository.valid[thirdApprover] = true
			decidedAt := t0.Add(2 * time.Hour)
			repository.clock = func() time.Time { return decidedAt }
			eventCountBefore := len(repository.events)
			var view access.RequestView
			var err error
			wantAction := audit.ActionAccessRequestApproved
			if scenario.decision == access.DecisionRejected {
				view, err = service.Reject(context.Background(), scenario.approver, pending.ID, scenario.reason)
				wantAction = audit.ActionAccessRequestRejected
			} else {
				view, err = service.Approve(context.Background(), scenario.approver, pending.ID, scenario.reason)
			}
			if err != nil {
				t.Fatalf("record decision: %v", err)
			}
			wantRequest := pending
			wantRequest.State = scenario.wantState
			if scenario.wantState != access.StatePending {
				wantRequest.UpdatedAt = decidedAt
			}
			if scenario.wantState == access.StateApproved {
				expiresAt := decidedAt.Add(24 * time.Hour)
				wantRequest.ExpiresAt = &expiresAt
			}
			if !reflect.DeepEqual(view.Request, wantRequest) {
				t.Errorf("request snapshot = %+v, want %+v", view.Request, wantRequest)
			}
			if view.EffectiveState != scenario.wantState || view.ValidApprovals != scenario.wantValidCount {
				t.Errorf("decision view state=%s count=%d, want state=%s count=%d", view.EffectiveState, view.ValidApprovals, scenario.wantState, scenario.wantValidCount)
			}
			if len(view.Approvals) != len(scenario.previousApprovers)+1 {
				t.Fatalf("approval views = %d, want %d", len(view.Approvals), len(scenario.previousApprovers)+1)
			}
			latest := view.Approvals[len(view.Approvals)-1]
			decision := latest.Approval
			if decision.ApproverID != scenario.approver || decision.Decision != scenario.decision || decision.Reason != scenario.reason || !decision.DecidedAt.Equal(decidedAt) || !latest.Valid {
				t.Errorf("recorded decision = %+v", latest)
			}
			if len(repository.events) != eventCountBefore+1 {
				t.Fatalf("audit count = %d, want %d", len(repository.events), eventCountBefore+1)
			}
			latestEvent := repository.events[len(repository.events)-1]
			if latestEvent.Action != wantAction || latestEvent.ActorUserID == nil || *latestEvent.ActorUserID != scenario.approver || latestEvent.Outcome != audit.OutcomeSucceeded {
				t.Errorf("decision audit = %+v", latestEvent)
			}
		})
	}
}

// TestApprovalDecisionRefusals preserves stored decisions and decision audits when a use case refuses input.
func TestApprovalDecisionRefusals(t *testing.T) {
	t.Parallel()
	scenarios := []struct {
		name       string
		approver   identity.UserID
		decision   access.Decision
		reason     string
		prepare    func(*fakeRepo, access.Request) access.Request
		wantError  error
		wantExpiry bool
	}{
		{name: "requester cannot approve", approver: requester, decision: access.DecisionApproved, wantError: access.ErrSelfApproval},
		{name: "rejection requires a nonblank reason", approver: approver2, decision: access.DecisionRejected, reason: " \t ", wantError: access.ErrReasonRequired},
		{name: "approval reason observes the character bound", approver: approver2, decision: access.DecisionApproved, reason: strings.Repeat("界", access.MaxApprovalReasonChars+1), wantError: access.ErrReasonTooLong},
		{
			name: "ineligible fake approver cannot record a decision", approver: approver2, decision: access.DecisionApproved, wantError: access.ErrInvalidApproval,
			prepare: func(repository *fakeRepo, pending access.Request) access.Request {
				repository.valid[approver2] = false
				return pending
			},
		},
		{
			name: "one approver cannot decide twice", approver: approver1, decision: access.DecisionApproved, wantError: access.ErrAlreadyDecided,
			prepare: func(repository *fakeRepo, pending access.Request) access.Request {
				repository.approvals[pending.ID] = []access.Approval{{RequestID: pending.ID, OrganizationID: pending.OrganizationID, ApproverID: approver1, Decision: access.DecisionApproved, DecidedAt: t0}}
				return pending
			},
		},
		{
			name: "cancelled request cannot receive a decision", approver: approver2, decision: access.DecisionApproved, wantError: access.ErrNotPending,
			prepare: func(_ *fakeRepo, pending access.Request) access.Request {
				pending.State = access.StateCancelled
				return pending
			},
		},
		{
			name: "expired approved request cannot receive a decision", approver: approver2, decision: access.DecisionApproved, wantError: access.ErrNotPending, wantExpiry: true,
			prepare: func(_ *fakeRepo, pending access.Request) access.Request {
				expiresAt := t0.Add(-time.Nanosecond)
				pending.State = access.StateApproved
				pending.ExpiresAt = &expiresAt
				return pending
			},
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			repository := newRepo()
			service := newService(t, repository)
			request := createPendingDecisionRequest(t, service)
			if scenario.prepare != nil {
				request = scenario.prepare(repository, request)
				repository.requests[request.ID] = request
			}
			approvalCountBefore := len(repository.approvals[request.ID])
			eventCountBefore := len(repository.events)
			var err error
			if scenario.decision == access.DecisionRejected {
				_, err = service.Reject(context.Background(), scenario.approver, request.ID, scenario.reason)
			} else {
				_, err = service.Approve(context.Background(), scenario.approver, request.ID, scenario.reason)
			}
			if !errors.Is(err, scenario.wantError) {
				t.Fatalf("decision error = %v, want %v", err, scenario.wantError)
			}
			if len(repository.approvals[request.ID]) != approvalCountBefore || len(repository.events) != eventCountBefore {
				t.Errorf("refused decision changed approvals/audits: approvals=%d, audits=%d", len(repository.approvals[request.ID]), len(repository.events))
			}
			wantRequest := request
			if scenario.wantExpiry {
				wantRequest.State = access.StateExpired
				wantRequest.Reason = access.ReasonTTLExpired
			}
			if stored := repository.requests[request.ID]; !reflect.DeepEqual(stored, wantRequest) {
				t.Errorf("stored request = %+v, want %+v", stored, wantRequest)
			}
		})
	}
}

// createPendingDecisionRequest submits one request requiring two current approvals.
func createPendingDecisionRequest(t *testing.T, service *accessrequest.Service) access.Request {
	t.Helper()
	draft := createDraft(t, service, connRead, "select 1")
	view, err := service.Submit(context.Background(), requester, draft.ID, draft.Version)
	if err != nil {
		t.Fatalf("submit decision request: %v", err)
	}
	if view.Request.State != access.StatePending || view.Request.RequiredApprovals != 2 {
		t.Fatalf("pending request = %+v", view.Request)
	}
	return view.Request
}
