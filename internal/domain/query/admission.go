package query

import "time"

// CachedResult supplies ordered LRU metadata for snapshot admission.
type CachedResult struct {
	SnapshotMetadata
	LastAccessedAt time.Time
}

// ResultEviction records why one cached snapshot must be removed.
type ResultEviction struct {
	Result CachedResult
	Cause  string
}

// PlanResultAdmission plans expiry and LRU removal, preserving live results when admission fails.
func PlanResultAdmission(current []CachedResult, incoming SnapshotMetadata, at time.Time, userQuota, globalQuota int64) ([]ResultEviction, error) {
	if incoming.ByteCount < 0 || incoming.ByteCount > userQuota || incoming.ByteCount > globalQuota {
		return nil, ErrResultStoreFull
	}
	evictions := []ResultEviction{}
	live := make([]bool, len(current))
	counts := map[string]int{}
	var ownBytes, totalBytes int64
	for idx, row := range current {
		if !at.Before(row.ExpiresAt) {
			evictions = append(evictions, ResultEviction{Result: row, Cause: "expired"})
			continue
		}
		live[idx] = true
		counts[string(row.OwnerID)]++
		totalBytes += row.ByteCount
		if row.OwnerID == incoming.OwnerID {
			ownBytes += row.ByteCount
		}
	}
	expiredCount := len(evictions)
	evict := func(idx int, cause string) {
		row := current[idx]
		live[idx] = false
		counts[string(row.OwnerID)]--
		totalBytes -= row.ByteCount
		if row.OwnerID == incoming.OwnerID {
			ownBytes -= row.ByteCount
		}
		evictions = append(evictions, ResultEviction{Result: row, Cause: cause})
	}
	for idx, row := range current {
		if ownBytes+incoming.ByteCount <= userQuota {
			break
		}
		if live[idx] && row.OwnerID == incoming.OwnerID {
			evict(idx, "user_quota")
		}
	}
	for idx, row := range current {
		if totalBytes+incoming.ByteCount <= globalQuota {
			break
		}
		if live[idx] && (row.OwnerID == incoming.OwnerID || counts[string(row.OwnerID)] > 1) {
			evict(idx, "global_cap")
		}
	}
	if totalBytes+incoming.ByteCount > globalQuota {
		return evictions[:expiredCount], ErrResultStoreFull
	}
	return evictions, nil
}
