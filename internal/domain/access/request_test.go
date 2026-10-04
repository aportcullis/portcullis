package access_test

import (
	"errors"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

var t0 = time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

func draft(t *testing.T) access.Request {
	t.Helper()
	r, err := access.NewDraft("11111111-2222-3333-4444-555555555555", "org-1", "conn-1", "user-req", t0)
	if err != nil {
		t.Fatalf("NewDraft: %v", err)
	}
	return r
}

func snapshot(quorum int) access.Snapshot {
	return access.Snapshot{
		Class:                   connection.ClassRead,
		PolicyVersion:           3,
		ConnectionConfigVersion: 1,
		ConnectionFingerprint:   "v1|sha256:abc",
		ConnectionDisplayName:   "Payroll Production",
		ConnectionDBType:        "postgresql",
		RequiredApprovals:       quorum,
		Digest:                  []byte{0xd1},
		DigestKeyVersion:        1,
		RedactedSQL:             "select <integer>",
	}
}

func submitted(t *testing.T, quorum int) access.Request {
	t.Helper()
	r, err := draft(t).Submitted(snapshot(quorum), t0.Add(time.Minute))
	if err != nil {
		t.Fatalf("Submitted: %v", err)
	}
	return r
}

func TestNewDraft(t *testing.T) {
	t.Parallel()
	r := draft(t)
	if r.State != access.StateDraft || r.Version != 1 || r.SubmittedAt != nil || r.ExpiresAt != nil {
		t.Errorf("draft shape wrong: %+v", r)
	}
	for _, bad := range [][4]string{
		{"", "org-1", "conn-1", "u"},
		{"id", "", "conn-1", "u"},
		{"id", "org-1", "", "u"},
		{"id", "org-1", "conn-1", ""},
	} {
		if _, err := access.NewDraft(access.RequestID(bad[0]), identity.OrganizationID(bad[1]), connection.ConnectionID(bad[2]), identity.UserID(bad[3]), t0); err == nil {
			t.Errorf("NewDraft(%v) must fail", bad)
		}
	}
}

func TestSubmittedPinsTheSnapshot(t *testing.T) {
	t.Parallel()
	r := submitted(t, 2)
	if r.State != access.StatePending {
		t.Fatalf("state = %q, want pending", r.State)
	}
	if r.Class != connection.ClassRead || r.PolicyVersion != 3 || r.RequiredApprovals != 2 ||
		r.RedactedSQL != "select <integer>" || len(r.Digest) == 0 || r.DigestKeyVersion != 1 {
		t.Errorf("snapshot not pinned: %+v", r)
	}
	if r.SubmittedAt == nil || !r.SubmittedAt.Equal(t0.Add(time.Minute)) {
		t.Errorf("SubmittedAt = %v", r.SubmittedAt)
	}

	if r.ConnectionConfigVersion != 1 || r.ConnectionFingerprint != "v1|sha256:abc" {
		t.Errorf("target pin not kept: config v%d fingerprint %q",
			r.ConnectionConfigVersion, r.ConnectionFingerprint)
	}

	if _, err := r.Submitted(snapshot(2), t0); !errors.Is(err, access.ErrNotDraft) {
		t.Errorf("re-submit = %v, want ErrNotDraft", err)
	}

	// An incomplete snapshot must refuse (the DB CHECK would reject it anyway). Every field is exercised: a snapshot missing any one of them describes an approval unit nobody could verify later.
	incomplete := map[string]func(access.Snapshot) access.Snapshot{
		"policy version 0":        func(s access.Snapshot) access.Snapshot { s.PolicyVersion = 0; return s },
		"negative quorum":         func(s access.Snapshot) access.Snapshot { s.RequiredApprovals = -1; return s },
		"missing digest":          func(s access.Snapshot) access.Snapshot { s.Digest = nil; return s },
		"missing digest key":      func(s access.Snapshot) access.Snapshot { s.DigestKeyVersion = 0; return s },
		"missing class":           func(s access.Snapshot) access.Snapshot { s.Class = ""; return s },
		"missing redacted SQL":    func(s access.Snapshot) access.Snapshot { s.RedactedSQL = ""; return s },
		"missing config version":  func(s access.Snapshot) access.Snapshot { s.ConnectionConfigVersion = 0; return s },
		"missing fingerprint":     func(s access.Snapshot) access.Snapshot { s.ConnectionFingerprint = ""; return s },
		"negative config version": func(s access.Snapshot) access.Snapshot { s.ConnectionConfigVersion = -1; return s },
	}
	for name, omit := range incomplete {
		if _, err := draft(t).Submitted(omit(snapshot(2)), t0); !errors.Is(err, access.ErrInvalidRequest) {
			t.Errorf("%s = %v, want ErrInvalidRequest", name, err)
		}
	}
}

func TestApproved(t *testing.T) {
	t.Parallel()
	r, err := submitted(t, 1).Approved(t0.Add(2*time.Minute), 24*time.Hour)
	if err != nil {
		t.Fatalf("Approved: %v", err)
	}
	if r.State != access.StateApproved {
		t.Errorf("state = %q", r.State)
	}
	if r.ExpiresAt == nil || !r.ExpiresAt.Equal(t0.Add(2*time.Minute).Add(24*time.Hour)) {
		t.Errorf("ExpiresAt = %v, want submit+24h", r.ExpiresAt)
	}
	// Only pending can approve; validity must be positive.
	if _, err := draft(t).Approved(t0, 24*time.Hour); !errors.Is(err, access.ErrNotPending) {
		t.Errorf("draft approve = %v, want ErrNotPending", err)
	}
	if _, err := submitted(t, 1).Approved(t0, 0); err == nil {
		t.Error("zero validity must fail")
	}
}

func TestCancellable(t *testing.T) {
	t.Parallel()
	if err := draft(t).ValidateCancellation(); err != nil {
		t.Errorf("draft: %v", err)
	}
	if err := submitted(t, 1).ValidateCancellation(); err != nil {
		t.Errorf("pending: %v", err)
	}
	appr, _ := submitted(t, 1).Approved(t0, time.Hour)
	if err := appr.ValidateCancellation(); err != nil {
		t.Errorf("approved: %v", err)
	}
	rejected := submitted(t, 1)
	rejected.State = access.StateRejected
	if err := rejected.ValidateCancellation(); !errors.Is(err, access.ErrNotCancellable) {
		t.Errorf("rejected cancel = %v, want ErrNotCancellable", err)
	}
}

func TestApprovalExpiresExactlyAtItsDeadline(t *testing.T) {
	t.Parallel()
	approved, err := submitted(t, 1).Approved(t0, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	deadline := t0.Add(time.Hour)
	for _, tc := range []struct {
		name    string
		at      time.Time
		expired bool
	}{
		{"one nanosecond before the deadline", deadline.Add(-time.Nanosecond), false},
		{"halfway through the window", t0.Add(30 * time.Minute), false},
		{"at the approval instant", t0, false},
		{"exactly at the deadline", deadline, true},
		{"one nanosecond after the deadline", deadline.Add(time.Nanosecond), true},
		{"a day after the deadline", deadline.Add(24 * time.Hour), true},
	} {
		if got := approved.ApprovalExpiredAt(tc.at); got != tc.expired {
			t.Errorf("ApprovalExpiredAt(%s) = %t, want %t", tc.name, got, tc.expired)
		}
		state, reason := approved.EffectiveState(tc.at)
		if tc.expired && (state != access.StateExpired || reason != access.ReasonTTLExpired) {
			t.Errorf("EffectiveState(%s) = %s/%s, want expired/ttl_expired", tc.name, state, reason)
		}
		if !tc.expired && (state != access.StateApproved || reason != "") {
			t.Errorf("EffectiveState(%s) = %s/%s, want approved", tc.name, state, reason)
		}
	}

	unbounded := approved
	unbounded.ExpiresAt = nil
	if !unbounded.ApprovalExpiredAt(t0) {
		t.Error("an approval without a deadline is treated as open; it must be treated as expired")
	}
	if state, _ := unbounded.EffectiveState(t0); state != access.StateExpired {
		t.Errorf("EffectiveState of an approval without a deadline = %s, want expired", state)
	}
	pending := submitted(t, 2)
	pending.ExpiresAt = &deadline
	if state, _ := pending.EffectiveState(deadline.Add(time.Hour)); state != access.StatePending {
		t.Errorf("a pending request with a stray deadline became %s", state)
	}
}

func TestEffectiveState(t *testing.T) {
	t.Parallel()
	appr, _ := submitted(t, 1).Approved(t0, time.Hour)
	if s, reason := appr.EffectiveState(t0.Add(30 * time.Minute)); s != access.StateApproved || reason != "" {
		t.Errorf("fresh = %q/%q", s, reason)
	}
	if s, reason := appr.EffectiveState(t0.Add(2 * time.Hour)); s != access.StateExpired || reason != access.ReasonTTLExpired {
		t.Errorf("overdue = %q/%q, want expired/ttl_expired", s, reason)
	}

	p := submitted(t, 2)
	if s, reason := p.EffectiveState(t0.Add(100 * time.Hour)); s != access.StatePending || reason != "" {
		t.Errorf("pending passthrough = %q/%q", s, reason)
	}
}
