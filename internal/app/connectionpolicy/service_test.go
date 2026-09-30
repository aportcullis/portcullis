package connectionpolicy_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	appolicy "github.com/aportcullis/portcullis/internal/app/connectionpolicy"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// fakeRepo is the in-memory Repository: one current policy per connection id, archived ids fail updates, and every mutation records the events it was asked to commit (they would ride the same transaction — ADR-0009).
type fakeRepo struct {
	org         identity.OrganizationID
	policies    map[connection.ConnectionID]connection.Policy
	archived    map[connection.ConnectionID]bool
	updateCalls int
	txEvents    [][]audit.Event
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		org:      "org1",
		policies: map[connection.ConnectionID]connection.Policy{},
		archived: map[connection.ConnectionID]bool{},
	}
}

func (r *fakeRepo) DefaultOrganizationID(context.Context) (identity.OrganizationID, error) {
	return r.org, nil
}

func (r *fakeRepo) GetCurrent(_ context.Context, org identity.OrganizationID, id connection.ConnectionID) (connection.Policy, error) {
	p, ok := r.policies[id]
	if !ok || org != r.org {
		return connection.Policy{}, connection.ErrNotFound
	}
	return p, nil
}

func (r *fakeRepo) UpdatePolicy(_ context.Context, next connection.Policy, expectedVersion int64, events ...audit.Event) (connection.Policy, error) {
	r.updateCalls++
	current, ok := r.policies[next.ConnectionID]
	if !ok {
		return connection.Policy{}, connection.ErrNotFound
	}
	if r.archived[next.ConnectionID] {
		return connection.Policy{}, connection.ErrArchived
	}
	if current.Version != expectedVersion {
		return connection.Policy{}, connection.ErrPolicyConflict
	}
	r.policies[next.ConnectionID] = next
	r.txEvents = append(r.txEvents, events)
	return next, nil
}

func (r *fakeRepo) seed() connection.ConnectionID {
	const id connection.ConnectionID = "c1"
	p := connection.DefaultPolicy()
	p.ConnectionID = id
	p.OrganizationID = r.org
	p.CreatedBy = "creator1"
	p.CreatedAt = time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	r.policies[id] = p
	return id
}

type fixture struct {
	repo *fakeRepo
	svc  *appolicy.Service
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	repo := newFakeRepo()
	svc, err := appolicy.New(repo)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	svc.WithClock(func() time.Time { return time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC) })
	return fixture{repo: repo, svc: svc}
}

func defaultUpdate(expected int64) appolicy.UpdateParams {
	return appolicy.UpdateParams{
		ExpectedVersion:     expected,
		Read:                appolicy.ClassRuleInput{Allowed: true, RequiredApprovals: 1},
		Write:               appolicy.ClassRuleInput{Allowed: false, RequiredApprovals: 1},
		DDL:                 appolicy.ClassRuleInput{Allowed: false, RequiredApprovals: 1},
		QueryTimeoutSeconds: 30,
		MaxRows:             10_000,
		MaxResultBytes:      16 << 20,
	}
}

func TestGetReturnsCurrentPolicy(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.repo.seed()

	got, err := f.svc.Get(t.Context(), id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Version != 1 || !got.Read.Allowed || got.Write.Allowed {
		t.Errorf("policy = %+v, want the seeded v1 default", got)
	}

	if _, err := f.svc.Get(t.Context(), "missing"); !errors.Is(err, connection.ErrNotFound) {
		t.Errorf("Get(missing) = %v, want ErrNotFound", err)
	}
}

func TestUpdateAppendsNextVersionWithAudit(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.repo.seed()

	p := defaultUpdate(1)
	p.Read.RequiredApprovals = 0 // auto-approve read (§4.3 small-team deadlock)
	p.QueryTimeoutSeconds = 60
	got, err := f.svc.Update(t.Context(), "admin1", id, p)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Version != 2 {
		t.Errorf("Version = %d, want 2", got.Version)
	}
	if got.Read.RequiredApprovals != 0 || got.Limits.QueryTimeoutSeconds != 60 {
		t.Errorf("policy = %+v", got)
	}
	if got.CreatedBy != "admin1" {
		t.Errorf("CreatedBy = %q, want the updating admin", got.CreatedBy)
	}

	if len(f.repo.txEvents) != 1 || len(f.repo.txEvents[0]) != 1 {
		t.Fatalf("txEvents = %+v, want exactly one CONNECTION_POLICY_UPDATED riding the mutation", f.repo.txEvents)
	}
	evt := f.repo.txEvents[0][0]
	if evt.Action != audit.ActionConnectionPolicyUpdated || evt.TargetID != string(id) {
		t.Fatalf("event = %+v", evt)
	}
	if evt.Metadata["policy_version"] != int64(2) {
		t.Errorf("policy_version metadata = %v, want 2", evt.Metadata["policy_version"])
	}
	changed, _ := evt.Metadata["changed_fields"].([]string)
	if !slices.Contains(changed, "read.required_approvals") || !slices.Contains(changed, "query_timeout_seconds") {
		t.Errorf("changed_fields = %v", changed)
	}
	auto, _ := evt.Metadata["auto_approve_classes"].([]string)
	if !slices.Contains(auto, "read") {
		t.Errorf("auto_approve_classes = %v, want [read]", auto)
	}
}

func TestUpdateEnablingWriteEmitsCompanionEvent(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.repo.seed()

	p := defaultUpdate(1)
	p.Write = appolicy.ClassRuleInput{Allowed: true, RequiredApprovals: 2}
	if _, err := f.svc.Update(t.Context(), "admin1", id, p); err != nil {
		t.Fatalf("Update: %v", err)
	}

	events := f.repo.txEvents[0]
	if len(events) != 2 {
		t.Fatalf("events = %d, want UPDATED + one CLASS_ENABLED", len(events))
	}
	companion := events[1]
	if companion.Action != audit.ActionConnectionPolicyClassEnabled {
		t.Fatalf("companion action = %q", companion.Action)
	}
	if companion.Metadata["class"] != "write" || companion.Metadata["required_approvals"] != 2 {
		t.Errorf("companion metadata = %+v", companion.Metadata)
	}

	enabled, _ := events[0].Metadata["enabled_classes"].([]string)
	if !slices.Contains(enabled, "write") {
		t.Errorf("enabled_classes = %v", enabled)
	}
}

func TestUpdateEnablingWriteAndDDLEmitsTwoCompanions(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.repo.seed()

	p := defaultUpdate(1)
	p.Write = appolicy.ClassRuleInput{Allowed: true, RequiredApprovals: 2}
	p.DDL = appolicy.ClassRuleInput{Allowed: true, RequiredApprovals: 3}
	if _, err := f.svc.Update(t.Context(), "admin1", id, p); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got := len(f.repo.txEvents[0]); got != 3 {
		t.Fatalf("events = %d, want UPDATED + 2 CLASS_ENABLED", got)
	}
}

func TestUpdateDisablingEmitsNoCompanion(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.repo.seed()

	p := defaultUpdate(1)
	p.Write = appolicy.ClassRuleInput{Allowed: true, RequiredApprovals: 1}
	if _, err := f.svc.Update(t.Context(), "admin1", id, p); err != nil {
		t.Fatal(err)
	}
	p = defaultUpdate(2)
	if _, err := f.svc.Update(t.Context(), "admin1", id, p); err != nil {
		t.Fatal(err)
	}

	second := f.repo.txEvents[1]
	if len(second) != 1 {
		t.Fatalf("disable emitted %d events, want only UPDATED", len(second))
	}
	disabled, _ := second[0].Metadata["disabled_classes"].([]string)
	if !slices.Contains(disabled, "write") {
		t.Errorf("disabled_classes = %v", disabled)
	}
}

func TestUpdateKeepingWriteEnabledEmitsNoCompanion(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.repo.seed()

	p := defaultUpdate(1)
	p.Write = appolicy.ClassRuleInput{Allowed: true, RequiredApprovals: 1}
	if _, err := f.svc.Update(t.Context(), "admin1", id, p); err != nil {
		t.Fatal(err)
	}
	p = defaultUpdate(2)
	p.Write = appolicy.ClassRuleInput{Allowed: true, RequiredApprovals: 5}
	if _, err := f.svc.Update(t.Context(), "admin1", id, p); err != nil {
		t.Fatal(err)
	}
	if got := len(f.repo.txEvents[1]); got != 1 {
		t.Fatalf("still-enabled write emitted %d events, want only UPDATED", got)
	}
}

func TestUpdateStaleVersionFailsWithPolicyConflict(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.repo.seed()

	if _, err := f.svc.Update(t.Context(), "admin1", id, defaultUpdate(1)); err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.Update(t.Context(), "admin1", id, defaultUpdate(1)); !errors.Is(err, connection.ErrPolicyConflict) {
		t.Fatalf("stale update = %v, want ErrPolicyConflict", err)
	}
}

func TestUpdateArchivedConnectionFails(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.repo.seed()
	f.repo.archived[id] = true

	if _, err := f.svc.Update(t.Context(), "admin1", id, defaultUpdate(1)); !errors.Is(err, connection.ErrArchived) {
		t.Fatalf("archived update = %v, want ErrArchived", err)
	}

	if _, err := f.svc.Get(t.Context(), id); err != nil {
		t.Errorf("Get on archived = %v, want nil", err)
	}
}

func TestUpdateInvalidParamsFailBeforeMutation(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	id := f.repo.seed()

	bad := defaultUpdate(1)
	bad.QueryTimeoutSeconds = 0
	if _, err := f.svc.Update(t.Context(), "admin1", id, bad); !errors.Is(err, connection.ErrInvalidPolicy) {
		t.Fatalf("invalid update = %v, want ErrInvalidPolicy", err)
	}
	bad = defaultUpdate(1)
	bad.Read.RequiredApprovals = -1
	if _, err := f.svc.Update(t.Context(), "admin1", id, bad); !errors.Is(err, connection.ErrInvalidPolicy) {
		t.Fatalf("invalid update = %v, want ErrInvalidPolicy", err)
	}
	if f.repo.updateCalls != 0 {
		t.Errorf("updateCalls = %d, want 0 — validation must fail before the mutation", f.repo.updateCalls)
	}
}
