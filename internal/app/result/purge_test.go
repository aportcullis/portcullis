package result_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/aportcullis/portcullis/internal/app/result"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

type expiredResultStore struct {
	organizations []identity.OrganizationID
	listErr       error
	failing       map[identity.OrganizationID]error
	purged        []identity.OrganizationID
	cancelAfter   int
	cancel        context.CancelFunc
}

func (s *expiredResultStore) ListOrganizationIDs(context.Context) ([]identity.OrganizationID, error) {
	return s.organizations, s.listErr
}

func (s *expiredResultStore) PurgeExpired(_ context.Context, org identity.OrganizationID) error {
	s.purged = append(s.purged, org)
	if s.cancel != nil && len(s.purged) == s.cancelAfter {
		s.cancel()
	}
	return s.failing[org]
}

func TestPurgeExpiredVisitsEveryOrganization(t *testing.T) {
	ctx := context.Background()
	for _, organizations := range [][]identity.OrganizationID{
		{"org-default"},
		{"org-default", "org-b"},
		{"org-a", "org-b", "org-c"},
		{},
	} {
		store := &expiredResultStore{organizations: organizations}
		if err := result.PurgeExpiredInEveryOrganization(ctx, store); err != nil {
			t.Fatalf("purge %v: %v", organizations, err)
		}
		if !slices.Equal(store.purged, organizations) && (len(store.purged) != 0 || len(organizations) != 0) {
			t.Errorf("purged %v, want %v", store.purged, organizations)
		}
	}
}

func TestPurgeExpiredFailuresDoNotStarveOtherOrganizations(t *testing.T) {
	ctx := context.Background()
	brokenOrg := errors.New("org-b purge failed")
	store := &expiredResultStore{
		organizations: []identity.OrganizationID{"org-a", "org-b", "org-c"},
		failing:       map[identity.OrganizationID]error{"org-b": brokenOrg},
	}
	err := result.PurgeExpiredInEveryOrganization(ctx, store)
	if !errors.Is(err, brokenOrg) {
		t.Errorf("purge error = %v, want the org-b failure", err)
	}
	if !slices.Equal(store.purged, []identity.OrganizationID{"org-a", "org-b", "org-c"}) {
		t.Errorf("purged %v, want every organization despite org-b failing", store.purged)
	}

	listFailure := errors.New("organization listing failed")
	unlisted := &expiredResultStore{organizations: []identity.OrganizationID{"org-a"}, listErr: listFailure}
	if err := result.PurgeExpiredInEveryOrganization(ctx, unlisted); !errors.Is(err, listFailure) || len(unlisted.purged) != 0 {
		t.Errorf("list failure = %v after purging %v, want the list error and no purge", err, unlisted.purged)
	}

	cancelled, cancel := context.WithCancel(ctx)
	stopping := &expiredResultStore{organizations: []identity.OrganizationID{"org-a", "org-b", "org-c"}, cancelAfter: 1, cancel: cancel}
	if err := result.PurgeExpiredInEveryOrganization(cancelled, stopping); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled purge = %v, want context.Canceled", err)
	}
	if len(stopping.purged) != 1 {
		t.Errorf("cancelled purge visited %v, want it to stop after the cancellation", stopping.purged)
	}

	if err := result.PurgeExpiredInEveryOrganization(ctx, nil); err == nil {
		t.Error("purge with a nil store succeeded, want refusal")
	}
}
