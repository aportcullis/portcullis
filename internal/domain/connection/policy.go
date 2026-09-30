package connection

import (
	"time"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// StatementClass is the coarse SQL classification a connection policy gates — a fixed domain enum (ADR-0002 classifies statements into exactly these; the dialect classifier and access_requests slices reuse it).
type StatementClass string

// Statement classes, ordered by escalating risk (ddl > write > read).
const (
	ClassRead  StatementClass = "read"
	ClassWrite StatementClass = "write"
	ClassDDL   StatementClass = "ddl"
)

// Classes returns the statement-class vocabulary in its canonical order.
func Classes() []StatementClass {
	return []StatementClass{ClassRead, ClassWrite, ClassDDL}
}

// Policy bounds (ADR-0015). The approvals cap is a schema sanity bound far above any real quorum; the limit bounds mirror PRD §8.2 (timeout ≤ 5m, rows ≤ 10k) and ADR-0011 (one result must fit the 64 MiB per-user quota).
const (
	maxRequiredApprovals = 100
	minQueryTimeoutSecs  = 1
	maxQueryTimeoutSecs  = 300
	maxMaxRows           = 10_000
	minMaxResultBytes    = 4096
	maxMaxResultBytes    = 64 << 20
)

// ClassRule is one statement class's gate: whether the class may run at all, and how many distinct approvers a request needs (0 = system auto-approval, audited the same — PRD §4.3). RequiredApprovals is kept even while the class is disallowed so re-enabling does not silently reset the quorum.
type ClassRule struct {
	Allowed           bool
	RequiredApprovals int
}

func (r ClassRule) validate() error {
	if r.RequiredApprovals < 0 || r.RequiredApprovals > maxRequiredApprovals {
		return ErrInvalidPolicy
	}
	return nil
}

// Limits are the per-policy execution bounds (one set per policy version, not per class — ADR-0015). They cap, never extend, the PRD §8.2 hard limits.
type Limits struct {
	QueryTimeoutSeconds int
	MaxRows             int
	// MaxResultBytes caps the stored result snapshot (ADR-0011 admission).
	MaxResultBytes int64
}

func (l Limits) validate() error {
	if l.QueryTimeoutSeconds < minQueryTimeoutSecs || l.QueryTimeoutSeconds > maxQueryTimeoutSecs {
		return ErrInvalidPolicy
	}
	if l.MaxRows < 1 || l.MaxRows > maxMaxRows {
		return ErrInvalidPolicy
	}
	if l.MaxResultBytes < minMaxResultBytes || l.MaxResultBytes > maxMaxResultBytes {
		return ErrInvalidPolicy
	}
	return nil
}

// Policy is one immutable version of a connection's execution policy (connection_policy_versions row). Access requests pin a Version; the pinned snapshot never changes — updates append the next version (ADR-0015).
type Policy struct {
	ConnectionID   ConnectionID
	OrganizationID identity.OrganizationID
	// Version is both the snapshot identity and the optimistic-concurrency token for updates (connections.current_policy_version points at it).
	Version int64
	Read    ClassRule
	Write   ClassRule
	DDL     ClassRule
	Limits  Limits
	// CreatedBy is the admin whose update produced this version; the backfilled v1 default rows carry the connection's creator.
	CreatedBy identity.UserID
	CreatedAt time.Time
}

// DefaultPolicy is the v1 policy every new connection starts with: read-only with one approval per class (PRD §4.3) and the ADR-0015 default limits. The identity fields are zero — the connection store binds them when it inserts the row alongside the connection.
func DefaultPolicy() Policy {
	return Policy{
		Version: 1,
		Read:    ClassRule{Allowed: true, RequiredApprovals: 1},
		Write:   ClassRule{Allowed: false, RequiredApprovals: 1},
		DDL:     ClassRule{Allowed: false, RequiredApprovals: 1},
		Limits:  Limits{QueryTimeoutSeconds: 30, MaxRows: 10_000, MaxResultBytes: 16 << 20},
	}
}

// NewPolicy validates and assembles a policy version.
func NewPolicy(
	id ConnectionID,
	org identity.OrganizationID,
	version int64,
	read, write, ddl ClassRule,
	limits Limits,
	createdBy identity.UserID,
	now time.Time,
) (Policy, error) {
	if id == "" || org == "" || createdBy == "" || version < 1 {
		return Policy{}, ErrInvalidPolicy
	}
	for _, r := range []ClassRule{read, write, ddl} {
		if err := r.validate(); err != nil {
			return Policy{}, err
		}
	}
	if err := limits.validate(); err != nil {
		return Policy{}, err
	}
	return Policy{
		ConnectionID:   id,
		OrganizationID: org,
		Version:        version,
		Read:           read,
		Write:          write,
		DDL:            ddl,
		Limits:         limits,
		CreatedBy:      createdBy,
		CreatedAt:      now,
	}, nil
}

// Rule returns the gate for one statement class (zero rule for an unknown class — callers pass the Classes() vocabulary).
func (p Policy) Rule(class StatementClass) ClassRule {
	switch class {
	case ClassRead:
		return p.Read
	case ClassWrite:
		return p.Write
	case ClassDDL:
		return p.DDL
	}
	return ClassRule{}
}
