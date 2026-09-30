package access

import (
	"time"

	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// Read models describe request details and list results.

// RequestableConnection contains the target fields needed by the request form.
type RequestableConnection struct {
	ID          connection.ConnectionID
	DisplayName string
	DBType      string
	Environment string
}

// SubmitTarget contains the current policy, configuration version, and target fingerprint.
type SubmitTarget struct {
	Policy        connection.Policy
	ConfigVersion int64
	Fingerprint   string
	DisplayName   string
	DBType        string
}

// ListQuery specifies pagination, sorting, and optional requester scope.
type ListQuery struct {
	Page           int
	PageSize       int
	SortDescending bool
	// State filters to one lifecycle state; empty = all.
	State State
	// RequesterID, when set, restricts rows to that requester.
	RequesterID identity.UserID
}

// ApprovalView contains a decision, display fields, and current approval validity.
type ApprovalView struct {
	Approval            Approval
	ApproverEmail       string
	ApproverDisplayName string
	Valid               bool
}

// RequestView contains display data, effective state, and optional decrypted details.
type RequestView struct {
	Request              Request
	RequesterEmail       string
	RequesterDisplayName string
	ConnectionName       string
	Approvals            []ApprovalView
	ValidApprovals       int
	EffectiveState       State
	EffectiveReason      Reason
	Payload              *Payload
}

// StampEffective fills the derived state fields at read time.
func (v *RequestView) StampEffective(now time.Time) {
	v.EffectiveState, v.EffectiveReason = v.Request.EffectiveState(now)
}

// RequestPage is one §7.1 page of request views.
type RequestPage struct {
	Items      []RequestView
	Page       int
	PageSize   int
	TotalCount int64
	TotalPages int
}
