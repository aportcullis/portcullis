package access_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/access"
)

func TestApplyApprovalDecisionPreservesSubQuorumRequest(t *testing.T) {
	t.Parallel()
	for _, validity := range []time.Duration{time.Hour, 0, -time.Hour} {
		request := pendingDecisionRequest(t, 2)
		approval := recordedDecision(t, request, access.DecisionApproved)
		before := cloneDecisionRequest(request)
		got, err := request.ApplyApprovalDecision(approval, 1, validity)
		if err != nil {
			t.Fatalf("sub-quorum approval with validity %s: %v", validity, err)
		}
		if !reflect.DeepEqual(got, before) {
			t.Errorf("sub-quorum decision changed the request: got %+v, want %+v", got, before)
		}
		assertDecisionRequestUnchanged(t, request, before)
	}
}

func TestApplyApprovalDecisionOpensWindowAtExactQuorum(t *testing.T) {
	t.Parallel()
	request := pendingDecisionRequest(t, 2)
	approval := recordedDecision(t, request, access.DecisionApproved)
	before := cloneDecisionRequest(request)
	got, err := request.ApplyApprovalDecision(approval, 2, 90*time.Minute)
	if err != nil {
		t.Fatalf("exact-quorum approval: %v", err)
	}
	expected := cloneDecisionRequest(before)
	expected.State = access.StateApproved
	expected.Reason = ""
	expected.UpdatedAt = approval.DecidedAt
	expires := approval.DecidedAt.Add(90 * time.Minute)
	expected.ExpiresAt = &expires
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("exact-quorum request: got %+v, want %+v", got, expected)
	}
	assertDecisionRequestUnchanged(t, request, before)
}

func TestApplyApprovalDecisionAcceptsCurrentCountAboveQuorum(t *testing.T) {
	t.Parallel()
	request := pendingDecisionRequest(t, 2)
	approval := recordedDecision(t, request, access.DecisionApproved)
	before := cloneDecisionRequest(request)
	got, err := request.ApplyApprovalDecision(approval, 3, time.Hour)
	if err != nil {
		t.Fatalf("above-quorum approval: %v", err)
	}
	expected := cloneDecisionRequest(before)
	expected.State = access.StateApproved
	expected.Reason = ""
	expected.UpdatedAt = approval.DecidedAt
	expires := approval.DecidedAt.Add(time.Hour)
	expected.ExpiresAt = &expires
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("above-quorum request: got %+v, want %+v", got, expected)
	}
	assertDecisionRequestUnchanged(t, request, before)
}

func TestApplyApprovalDecisionPreservesValidZeroQuorumThreshold(t *testing.T) {
	t.Parallel()
	request := pendingDecisionRequest(t, 0)
	approval := recordedDecision(t, request, access.DecisionApproved)
	before := cloneDecisionRequest(request)
	got, err := request.ApplyApprovalDecision(approval, 0, time.Hour)
	if err != nil {
		t.Fatalf("zero-quorum approval: %v", err)
	}
	expected := cloneDecisionRequest(before)
	expected.State = access.StateApproved
	expected.Reason = ""
	expected.UpdatedAt = approval.DecidedAt
	expires := approval.DecidedAt.Add(time.Hour)
	expected.ExpiresAt = &expires
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("zero-quorum request: got %+v, want %+v", got, expected)
	}
	assertDecisionRequestUnchanged(t, request, before)
}

func TestApplyApprovalDecisionRejectsWithoutOpeningOrExtendingWindow(t *testing.T) {
	t.Parallel()
	for _, existingExpiry := range []bool{false, true} {
		request := pendingDecisionRequest(t, 2)
		if !existingExpiry {
			request.ExpiresAt = nil
		}
		approval := recordedDecision(t, request, access.DecisionRejected)
		approval.Reason = strings.Repeat("거", access.MaxApprovalReasonChars)
		before := cloneDecisionRequest(request)
		got, err := request.ApplyApprovalDecision(approval, 0, -time.Hour)
		if err != nil {
			t.Fatalf("rejection with existing expiry %t: %v", existingExpiry, err)
		}
		expected := cloneDecisionRequest(before)
		expected.State = access.StateRejected
		expected.Reason = ""
		expected.UpdatedAt = approval.DecidedAt
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("rejected request: got %+v, want %+v", got, expected)
		}
		assertDecisionRequestUnchanged(t, request, before)
	}
}

func TestApplyApprovalDecisionRefusesSettledOrUnsubmittedRequests(t *testing.T) {
	t.Parallel()
	for _, state := range access.States() {
		if state == access.StatePending {
			continue
		}
		for _, decision := range []access.Decision{access.DecisionApproved, access.DecisionRejected} {
			request := pendingDecisionRequest(t, 2)
			request.State = state
			approval := recordedDecision(t, request, decision)
			assertDecisionRefused(t, request, approval, 2, time.Hour, access.ErrNotPending)
		}
	}
}

func TestApplyApprovalDecisionRefusesForeignDecision(t *testing.T) {
	t.Parallel()
	for _, association := range []string{"request", "organization"} {
		request := pendingDecisionRequest(t, 2)
		approval := recordedDecision(t, request, access.DecisionApproved)
		if association == "request" {
			approval.RequestID = "another-request"
		} else {
			approval.OrganizationID = "another-organization"
		}
		assertDecisionRefused(t, request, approval, 2, time.Hour, access.ErrInvalidApproval)
	}
}

func TestApplyApprovalDecisionRevalidatesPublicDecisionFields(t *testing.T) {
	t.Parallel()
	scenarios := []struct {
		name   string
		forge  func(*access.Approval, access.Request)
		refuse error
	}{
		{
			name: "self-approval",
			forge: func(approval *access.Approval, request access.Request) {
				approval.ApproverID = request.RequesterID
			},
			refuse: access.ErrSelfApproval,
		},
		{
			name: "missing approver",
			forge: func(approval *access.Approval, _ access.Request) {
				approval.ApproverID = ""
			},
			refuse: access.ErrInvalidApproval,
		},
		{
			name: "unknown decision",
			forge: func(approval *access.Approval, _ access.Request) {
				approval.Decision = "maybe"
			},
			refuse: access.ErrInvalidApproval,
		},
		{
			name: "blank rejection reason",
			forge: func(approval *access.Approval, _ access.Request) {
				approval.Decision = access.DecisionRejected
				approval.Reason = " \t\n"
			},
			refuse: access.ErrReasonRequired,
		},
		{
			name: "oversized approval reason",
			forge: func(approval *access.Approval, _ access.Request) {
				approval.Reason = strings.Repeat("거", access.MaxApprovalReasonChars+1)
			},
			refuse: access.ErrReasonTooLong,
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			request := pendingDecisionRequest(t, 2)
			approval := recordedDecision(t, request, access.DecisionApproved)
			scenario.forge(&approval, request)
			assertDecisionRefused(t, request, approval, 2, time.Hour, scenario.refuse)
		})
	}
}

func TestApplyApprovalDecisionRequiresRecordedDatabaseInstant(t *testing.T) {
	t.Parallel()
	request := pendingDecisionRequest(t, 2)
	approval := recordedDecision(t, request, access.DecisionApproved)
	approval.DecidedAt = time.Time{}
	assertDecisionRefused(t, request, approval, 2, time.Hour, access.ErrInvalidApproval)
}

func TestApplyApprovalDecisionRefusesImpossibleQuorumFacts(t *testing.T) {
	t.Parallel()
	request := pendingDecisionRequest(t, 2)
	approval := recordedDecision(t, request, access.DecisionApproved)
	assertDecisionRefused(t, request, approval, -1, time.Hour, access.ErrInvalidApproval)
	request.RequiredApprovals = -1
	assertDecisionRefused(t, request, approval, 2, time.Hour, access.ErrInvalidRequest)
}

func TestApplyApprovalDecisionRequiresPositiveValidityAtQuorum(t *testing.T) {
	t.Parallel()
	for _, validity := range []time.Duration{0, -time.Hour} {
		request := pendingDecisionRequest(t, 2)
		approval := recordedDecision(t, request, access.DecisionApproved)
		assertDecisionRefused(t, request, approval, 2, validity, access.ErrInvalidRequest)
	}
}

// pendingDecisionRequest builds a pinned pending request with existing reason, expiry and version fields.
func pendingDecisionRequest(t *testing.T, requiredApprovals int) access.Request {
	t.Helper()
	request := submitted(t, requiredApprovals)
	request.Title = "Review the pinned query"
	request.Version = 17
	request.Reason = access.ReasonPolicyChanged
	existingExpiry := t0.Add(-time.Minute)
	request.ExpiresAt = &existingExpiry
	return request
}

// recordedDecision builds a valid decision at a fixed supplied instant.
func recordedDecision(t *testing.T, request access.Request, decision access.Decision) access.Approval {
	t.Helper()
	approval, err := access.NewApproval(request.ID, request.OrganizationID, "user-approver", request.RequesterID, decision, "Reviewed query", t0.Add(12*time.Minute+345678*time.Microsecond))
	if err != nil {
		t.Fatalf("NewApproval: %v", err)
	}
	return approval
}

// assertDecisionRefused checks the refusal sentinel and the caller's unchanged request.
func assertDecisionRefused(t *testing.T, request access.Request, approval access.Approval, count int64, validity time.Duration, expectedError error) {
	t.Helper()
	before := cloneDecisionRequest(request)
	if _, err := request.ApplyApprovalDecision(approval, count, validity); !errors.Is(err, expectedError) {
		t.Errorf("decision %q for %q with count %d and validity %s: got %v, want %v", approval.Decision, request.State, count, validity, err, expectedError)
	}
	assertDecisionRequestUnchanged(t, request, before)
}

// assertDecisionRequestUnchanged verifies that the decision preserved caller-owned data.
func assertDecisionRequestUnchanged(t *testing.T, request, before access.Request) {
	t.Helper()
	if !reflect.DeepEqual(request, before) {
		t.Errorf("decision mutated the input request: got %+v, want %+v", request, before)
	}
}

// cloneDecisionRequest copies mutable request fields so decision tests can detect mutations.
func cloneDecisionRequest(request access.Request) access.Request {
	cloned := request
	cloned.Digest = append([]byte(nil), request.Digest...)
	if request.SubmittedAt != nil {
		submittedAt := *request.SubmittedAt
		cloned.SubmittedAt = &submittedAt
	}
	if request.ExpiresAt != nil {
		expiresAt := *request.ExpiresAt
		cloned.ExpiresAt = &expiresAt
	}
	return cloned
}
