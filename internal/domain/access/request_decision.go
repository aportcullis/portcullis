package access

import "time"

// ApplyApprovalDecision derives the pending request's next state from a recorded decision and current quorum facts.
func (r Request) ApplyApprovalDecision(approval Approval, currentEligibleApprovals int64, validity time.Duration) (Request, error) {
	if r.State != StatePending {
		return Request{}, ErrNotPending
	}
	if r.RequiredApprovals < 0 {
		return Request{}, ErrInvalidRequest
	}
	if approval.RequestID != r.ID || approval.OrganizationID != r.OrganizationID ||
		approval.DecidedAt.IsZero() || currentEligibleApprovals < 0 {
		return Request{}, ErrInvalidApproval
	}
	if _, err := NewApproval(approval.RequestID, approval.OrganizationID, approval.ApproverID, r.RequesterID, approval.Decision, approval.Reason, approval.DecidedAt); err != nil {
		return Request{}, err
	}
	if approval.Decision == DecisionRejected {
		r.State = StateRejected
		r.Reason = ""
		r.UpdatedAt = approval.DecidedAt
		return r, nil
	}
	if currentEligibleApprovals < int64(r.RequiredApprovals) {
		return r, nil
	}
	approved, err := r.Approved(approval.DecidedAt, validity)
	if err != nil {
		return Request{}, err
	}
	approved.Reason = ""
	return approved, nil
}
