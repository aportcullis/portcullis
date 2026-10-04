package result

import (
	"context"
	"errors"
	"fmt"
)

// PurgeExpiredInEveryOrganization purges each organization's expired snapshots, continuing past a failing organization.
func PurgeExpiredInEveryOrganization(ctx context.Context, store ExpiredResultStore) error {
	if store == nil {
		return errors.New("result: nil expired-result store")
	}
	organizations, err := store.ListOrganizationIDs(ctx)
	if err != nil {
		return fmt.Errorf("result: list organizations: %w", err)
	}
	var failures []error
	for _, org := range organizations {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		if err := store.PurgeExpired(ctx, org); err != nil {
			failures = append(failures, fmt.Errorf("result: purge organization %s: %w", org, err))
		}
	}
	if err := ctx.Err(); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}
