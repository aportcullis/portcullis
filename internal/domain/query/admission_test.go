package query_test

import (
	"errors"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"testing"
	"time"
)

func TestResultAdmissionEvictsExpiredThenOwnLRUAndPreservesOtherOwners(t *testing.T) {
	at := time.Now()
	row := func(id, owner string, size int64, expired bool) query.CachedResult {
		metadata := query.SnapshotMetadata{ID: id, OwnerID: identity.UserID(owner), ByteCount: size, ExpiresAt: at.Add(time.Hour)}
		if expired {
			metadata.ExpiresAt = at.Add(-time.Second)
		}
		return query.CachedResult{SnapshotMetadata: metadata, LastAccessedAt: at}
	}
	current := []query.CachedResult{row("expired", "other", 20, true), row("own-old", "me", 20, false), row("other-only", "other", 40, false), row("own-new", "me", 20, false)}
	incoming := query.SnapshotMetadata{OwnerID: "me", ByteCount: 30}
	evicted, err := query.PlanResultAdmission(current, incoming, at, 45, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(evicted) != 3 || evicted[0].Cause != "expired" || evicted[1].Result.ID != "own-old" || evicted[2].Result.ID != "own-new" {
		t.Fatalf("evictions=%+v", evicted)
	}
	_, err = query.PlanResultAdmission([]query.CachedResult{row("only-a", "a", 40, false), row("only-b", "b", 40, false)}, incoming, at, 45, 100)
	if !errors.Is(err, query.ErrResultStoreFull) {
		t.Fatalf("last-result floors not enforced: %v", err)
	}
}
