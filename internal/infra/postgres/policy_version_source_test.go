package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

func TestPolicyVersionIsDerivedFromTheStoredPointer(t *testing.T) {
	f := newPolicyFixture(t)
	ctx := context.Background()
	newConnection := func(t *testing.T) connection.Policy {
		t.Helper()
		c := f.newConn(t, unique("PolicyVersion"))
		if err := f.store.Create(ctx, c, sealedStub(1), connEvent(audit.ActionConnectionCreated, c.ID)); err != nil {
			t.Fatal(err)
		}
		current, err := f.policies.GetCurrent(ctx, f.org, c.ID)
		if err != nil {
			t.Fatal(err)
		}
		return current
	}

	for _, tc := range []struct {
		name          string
		callerVersion func(current connection.Policy) int64
	}{
		{"the caller's version matches the next pointer", func(current connection.Policy) int64 { return current.Version + 1 }},
		{"the caller claims a version far ahead", func(current connection.Policy) int64 { return current.Version + 41 }},
		{"the caller repeats the current version", func(current connection.Policy) int64 { return current.Version }},
		{"the caller sends an impossible version", func(connection.Policy) int64 { return 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := newConnection(t)
			next := validNextPolicy(t, current, f.user)
			next.Version = tc.callerVersion(current)
			stored, err := f.policies.UpdatePolicy(ctx, next, current.Version, connEvent(audit.ActionConnectionPolicyUpdated, current.ConnectionID))
			if err != nil {
				t.Fatalf("UpdatePolicy: %v", err)
			}
			if stored.Version != current.Version+1 {
				t.Errorf("stored version = %d, want the bumped pointer %d", stored.Version, current.Version+1)
			}
			reread, err := f.policies.GetCurrent(ctx, f.org, current.ConnectionID)
			if err != nil || reread.Version != current.Version+1 || reread.Write.RequiredApprovals != 2 {
				t.Errorf("current policy after update = %+v, %v", reread, err)
			}
		})
	}

	t.Run("a stale expected version is a conflict", func(t *testing.T) {
		current := newConnection(t)
		if _, err := f.policies.UpdatePolicy(ctx, validNextPolicy(t, current, f.user), current.Version); err != nil {
			t.Fatal(err)
		}
		if _, err := f.policies.UpdatePolicy(ctx, validNextPolicy(t, current, f.user), current.Version); !errors.Is(err, connection.ErrPolicyConflict) {
			t.Errorf("stale UpdatePolicy = %v, want ErrPolicyConflict", err)
		}
	})

	t.Run("a forged expected version ahead of the pointer is a conflict", func(t *testing.T) {
		current := newConnection(t)
		if _, err := f.policies.UpdatePolicy(ctx, validNextPolicy(t, current, f.user), current.Version+5); !errors.Is(err, connection.ErrPolicyConflict) {
			t.Errorf("forged-ahead UpdatePolicy = %v, want ErrPolicyConflict", err)
		}
	})

	t.Run("an archived connection is refused", func(t *testing.T) {
		current := newConnection(t)
		if _, err := f.store.Archive(ctx, f.org, current.ConnectionID, connEvent(audit.ActionConnectionArchived, current.ConnectionID)); err != nil {
			t.Fatal(err)
		}
		if _, err := f.policies.UpdatePolicy(ctx, validNextPolicy(t, current, f.user), current.Version); !errors.Is(err, connection.ErrArchived) {
			t.Errorf("UpdatePolicy on an archived connection = %v, want ErrArchived", err)
		}
	})

	t.Run("a foreign organization is not found", func(t *testing.T) {
		current := newConnection(t)
		next := validNextPolicy(t, current, f.user)
		next.OrganizationID = identity.OrganizationID(uuid.NewString())
		if _, err := f.policies.UpdatePolicy(ctx, next, current.Version); !errors.Is(err, connection.ErrNotFound) {
			t.Errorf("cross-organization UpdatePolicy = %v, want ErrNotFound", err)
		}
	})
}
