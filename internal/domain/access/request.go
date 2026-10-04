package access

import (
	"time"

	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// RequestID identifies an access request.
type RequestID string

// Request holds the access-request lifecycle and immutable submission snapshot.
type Request struct {
	Title          string
	ID             RequestID
	OrganizationID identity.OrganizationID
	ConnectionID   connection.ConnectionID
	RequesterID    identity.UserID
	State          State
	// Reason is set only on system-caused expired/cancelled transitions.
	Reason Reason

	// Submission fields are immutable; Digest authenticates the whole approval unit.
	Digest           []byte
	DigestKeyVersion uint32
	RedactedSQL      string
	Class            connection.StatementClass
	PolicyVersion    int64
	// Pin the submitted target’s config version and descriptor so later edits cannot rewrite approval history (PRD §4.3).
	ConnectionConfigVersion int64
	ConnectionFingerprint   string
	ConnectionDisplayName   string
	ConnectionDBType        string
	RequiredApprovals       int
	SubmittedAt             *time.Time

	// ExpiresAt is the approval time plus its configured validity window.
	ExpiresAt *time.Time

	// Version guards draft edits and submission against concurrent changes.
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewDraft assembles a fresh draft request.
func NewDraft(
	id RequestID,
	org identity.OrganizationID,
	connID connection.ConnectionID,
	requester identity.UserID,
	now time.Time,
) (Request, error) {
	if id == "" || org == "" || connID == "" || requester == "" {
		return Request{}, ErrInvalidRequest
	}
	return Request{
		ID:             id,
		OrganizationID: org,
		ConnectionID:   connID,
		RequesterID:    requester,
		State:          StateDraft,
		Version:        1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// Submitted returns a pending request with its approval snapshot pinned.
func (r Request) Submitted(s Snapshot, now time.Time) (Request, error) {
	if r.State != StateDraft {
		return Request{}, ErrNotDraft
	}
	if err := s.validate(); err != nil {
		return Request{}, err
	}
	r.State = StatePending
	r.Class = s.Class
	r.PolicyVersion = s.PolicyVersion
	r.ConnectionConfigVersion = s.ConnectionConfigVersion
	r.ConnectionFingerprint = s.ConnectionFingerprint
	r.ConnectionDisplayName = s.ConnectionDisplayName
	r.ConnectionDBType = s.ConnectionDBType
	r.RequiredApprovals = s.RequiredApprovals
	r.Digest = s.Digest
	r.DigestKeyVersion = s.DigestKeyVersion
	r.RedactedSQL = s.RedactedSQL
	// The store replaces this timestamp with the database time observed under lock.
	r.SubmittedAt = &now
	r.UpdatedAt = now
	return r, nil
}

// Snapshot holds the immutable target and policy fields fixed at submission.
type Snapshot struct {
	Class                   connection.StatementClass
	PolicyVersion           int64
	ConnectionConfigVersion int64
	ConnectionFingerprint   string
	ConnectionDisplayName   string
	ConnectionDBType        string
	RequiredApprovals       int
	Digest                  []byte
	DigestKeyVersion        uint32
	RedactedSQL             string
}

func (s Snapshot) validate() error {
	if s.Class == "" || s.PolicyVersion < 1 || s.ConnectionConfigVersion < 1 ||
		s.ConnectionFingerprint == "" || s.ConnectionDisplayName == "" || s.ConnectionDBType == "" ||
		s.RequiredApprovals < 0 ||
		len(s.Digest) == 0 || s.DigestKeyVersion == 0 || s.RedactedSQL == "" {
		return ErrInvalidRequest
	}
	return nil
}

// Approved returns an approved request with its validity window applied.
func (r Request) Approved(now time.Time, validity time.Duration) (Request, error) {
	if r.State != StatePending {
		return Request{}, ErrNotPending
	}
	if validity <= 0 {
		return Request{}, ErrInvalidRequest
	}
	expires := now.Add(validity)
	r.State = StateApproved
	r.ExpiresAt = &expires
	r.UpdatedAt = now
	return r, nil
}

// ValidateCancellation rejects requests outside draft, pending, or approved states.
func (r Request) ValidateCancellation() error {
	switch r.State {
	case StateDraft, StatePending, StateApproved:
		return nil
	}
	return ErrNotCancellable
}

// ApprovalExpiredAt reports whether the approval window has closed at the instant: it closes exactly at ExpiresAt, and a missing deadline counts as closed (ADR-0018).
func (r Request) ApprovalExpiredAt(at time.Time) bool {
	return r.ExpiresAt == nil || !at.Before(*r.ExpiresAt)
}

// EffectiveState derives approval expiry without changing the stored request.
func (r Request) EffectiveState(now time.Time) (State, Reason) {
	if r.State == StateApproved && r.ApprovalExpiredAt(now) {
		return StateExpired, ReasonTTLExpired
	}
	return r.State, r.Reason
}
