package access

import "errors"

// Sentinel errors. The transport maps these onto Connect codes; messages are generic on purpose — nothing here may echo SQL or parameter values.
var (
	// ErrNotFound: no such request in this organization.
	ErrNotFound = errors.New("access: request not found")
	// ErrInvalidRequest: a constructor/transition received an impossible shape.
	ErrInvalidRequest = errors.New("access: invalid request")
	// ErrInvalidPayload: the SQL/parameter payload failed shape validation.
	ErrInvalidPayload = errors.New("access: invalid payload")
	// ErrInvalidApproval: an approval had an impossible shape — a missing id or an unknown decision. Those fields come from the handler, never from the user, so reaching this is a broken server invariant (the transport leaves it Internal); ErrReasonTooLong below is the user-input counterpart.
	ErrInvalidApproval = errors.New("access: invalid approval")
	// ErrReasonTooLong: the approval/rejection reason exceeded the character cap (PRD §4.4). The one shape a user can get wrong, so it maps to InvalidArgument rather than Internal.
	ErrReasonTooLong = errors.New("access: approval reason is too long")

	// ErrNotDraft: only a draft may be edited or submitted (PRD §4.4).
	ErrNotDraft = errors.New("access: request is not a draft")
	// ErrNotPending: only a pending request may be approved or rejected.
	ErrNotPending = errors.New("access: request is not pending")
	// ErrNotCancellable: only draft/pending/approved may be cancelled.
	ErrNotCancellable = errors.New("access: request is not cancellable")
	// ErrInvalidTransition: the requested state change is not an edge of the request state machine.
	ErrInvalidTransition = errors.New("access: state transition is not allowed")
	// ErrConflict: the optimistic version token did not match — reload and retry.
	ErrConflict = errors.New("access: request changed concurrently")

	// ErrSelfApproval: requesters never approve their own requests (admin included, PRD §4.3).
	ErrSelfApproval = errors.New("access: self-approval is not allowed")
	// ErrAlreadyDecided: this approver already decided on this request (UNIQUE(request_id, approver_id) → 23505).
	ErrAlreadyDecided = errors.New("access: approver already decided")
	// ErrReasonRequired: a rejection must say why (PRD §4.4).
	ErrReasonRequired = errors.New("access: rejection requires a reason")
	// ErrApproverIneligible: at decision time (under the row lock) the approver is no longer active or no longer holds the action's permission (requests.approve / requests.reject). Re-checked inside the transaction so a permission revoked after the handler's coarse check cannot still decide a request (PRD §4.4 "re-check at approval-record time").
	ErrApproverIneligible = errors.New("access: approver no longer eligible to decide")

	// ErrClassNotAllowed: the pinned policy does not allow the statement class.
	ErrClassNotAllowed = errors.New("access: statement class not allowed by policy")
	// ErrUnclassifiable: the dialect could not classify the statement — reject, never guess (PRD §4.3 fail-closed).
	ErrUnclassifiable = errors.New("access: statement could not be classified")
	// ErrConnectionArchived: the connection no longer accepts new requests.
	ErrConnectionArchived = errors.New("access: connection is archived")
	// ErrConnectionChanged: the connection's configuration was replaced after this request pinned it. The pipeline classified, gated and digested a target that no longer exists, so the submit is refused and the requester starts again against the new one (ADR-0014/0018).
	ErrConnectionChanged = errors.New("access: connection configuration changed")
)
