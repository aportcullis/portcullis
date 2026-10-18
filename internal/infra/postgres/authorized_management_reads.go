package postgres

import (
	"context"
	"time"

	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
	"github.com/jackc/pgx/v5/pgtype"
)

type managementReadCheckpoint struct {
	actor      identity.UserID
	permission identity.Permission
	target     authz.ResourceTarget
	revision   int64
}

// withAuthorizedManagementRead coordinates metadata and registers fresh revision/condition checks before output.
func (store *ManagementReadStore) withAuthorizedManagementRead(ctx context.Context, organizationID pgtype.UUID, read func(context.Context, administration.ManagementReadSnapshot) error) error {
	var checkpoints []managementReadCheckpoint
	err := store.policy.withPolicyRead(ctx, store.pool, store.organization, func(ctx context.Context, queries *db.Queries, at time.Time) error {
		snapshot := managementReadSnapshot{queries: queries, organization: store.organization, organizationID: organizationID, at: at}
		if err := read(ctx, &snapshot); err != nil {
			return err
		}
		checkpoints = snapshot.checkpoints
		return nil
	})
	if err != nil {
		return err
	}
	authz.RegisterOutputCheck(ctx, func(checkContext context.Context) error {
		return store.policy.withPolicyRead(checkContext, store.pool, store.organization, func(ctx context.Context, queries *db.Queries, at time.Time) error {
			for _, checkpoint := range checkpoints {
				current, err := store.policy.authorize(ctx, queries, checkpoint.actor, checkpoint.permission, checkpoint.target, at)
				if err != nil {
					return err
				}
				if current.Revision != checkpoint.revision {
					return authz.ErrPermissionDenied
				}
			}
			return nil
		})
	})
	return nil
}
