package access

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// MaxApprovalReasonChars matches the database constraint and is served to clients so UI limits cannot drift.
const MaxApprovalReasonChars = 1000

// Decision is an approver's verdict (approvals.decision).
type Decision string

const (
	DecisionApproved Decision = "approved"
	DecisionRejected Decision = "rejected"
)

// Approval is one approver's immutable decision on a request (approvals row). Rows are never mutated: whether an approval still COUNTS is a predicate computed at count time (active approver + live requests.approve grant), never a flag stored here (ADR-0018).
type Approval struct {
	ID             string
	RequestID      RequestID
	OrganizationID identity.OrganizationID
	ApproverID     identity.UserID
	Decision       Decision
	// Reason is required for a rejection (PRD §4.4), optional for an approval.
	Reason    string
	DecidedAt time.Time
}

// NewApproval validates and assembles a decision. The requester is passed in so self-approval is structurally impossible to build (admin included, PRD §4.3); the store's re-validation is the second, race-free enforcement.
func NewApproval(
	requestID RequestID,
	org identity.OrganizationID,
	approver, requester identity.UserID,
	decision Decision,
	reason string,
	now time.Time,
) (Approval, error) {
	if requestID == "" || org == "" || approver == "" || requester == "" {
		return Approval{}, ErrInvalidApproval
	}
	if decision != DecisionApproved && decision != DecisionRejected {
		return Approval{}, ErrInvalidApproval
	}
	if approver == requester {
		return Approval{}, ErrSelfApproval
	}
	if utf8.RuneCountInString(reason) > MaxApprovalReasonChars {
		return Approval{}, ErrReasonTooLong
	}
	if decision == DecisionRejected && strings.TrimSpace(reason) == "" {
		return Approval{}, ErrReasonRequired
	}
	return Approval{
		RequestID:      requestID,
		OrganizationID: org,
		ApproverID:     approver,
		Decision:       decision,
		Reason:         reason,
		DecidedAt:      now,
	}, nil
}
