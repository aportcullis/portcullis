package access_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/access"
)

func TestNewApproval(t *testing.T) {
	t.Parallel()
	a, err := access.NewApproval("req-1", "org-1", "user-approver", "user-req", access.DecisionApproved, "", t0)
	if err != nil {
		t.Fatalf("NewApproval: %v", err)
	}
	if a.Decision != access.DecisionApproved || a.ApproverID != "user-approver" {
		t.Errorf("approval shape: %+v", a)
	}

	if _, err := access.NewApproval("req-1", "org-1", "user-req", "user-req", access.DecisionApproved, "", t0); !errors.Is(err, access.ErrSelfApproval) {
		t.Errorf("self-approval = %v, want ErrSelfApproval", err)
	}
	// A rejection must carry a reason (PRD §4.4); either decision caps it.
	if _, err := access.NewApproval("req-1", "org-1", "a", "b", access.DecisionRejected, "", t0); !errors.Is(err, access.ErrReasonRequired) {
		t.Errorf("reasonless rejection = %v, want ErrReasonRequired", err)
	}
	if _, err := access.NewApproval("req-1", "org-1", "a", "b", access.DecisionRejected, "   ", t0); !errors.Is(err, access.ErrReasonRequired) {
		t.Errorf("whitespace rejection reason = %v, want ErrReasonRequired", err)
	}

	if _, err := access.NewApproval("req-1", "org-1", "a", "b", access.DecisionApproved, strings.Repeat("r", 1001), t0); !errors.Is(err, access.ErrReasonTooLong) {
		t.Errorf("oversized reason = %v, want ErrReasonTooLong", err)
	}
	if _, err := access.NewApproval("req-1", "org-1", "a", "b", access.DecisionApproved, strings.Repeat("r", 1000), t0); err != nil {
		t.Errorf("reason at the limit = %v, want accepted", err)
	}
	if _, err := access.NewApproval("req-1", "org-1", "a", "b", access.Decision("maybe"), "", t0); err == nil {
		t.Error("unknown decision must fail")
	}
	if _, err := access.NewApproval("", "org-1", "a", "b", access.DecisionApproved, "", t0); err == nil {
		t.Error("missing ids must fail")
	}
	if _, err := access.NewApproval("req-1", "org-1", "a", "b", access.DecisionRejected, "syntax looks off", t0); err != nil {
		t.Errorf("valid rejection: %v", err)
	}
}
