package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

// BindingManagementStore provides locked policy transactions; production preparation remains read-only.
type BindingManagementStore struct{ pool *pgxpool.Pool }

// NewBindingManagementStore constructs the inactive management repository for constructor injection.
func NewBindingManagementStore(pool *pgxpool.Pool) *BindingManagementStore {
	return &BindingManagementStore{pool: pool}
}

// WithBindingTransaction coordinates current policy reads and atomic grant writes under one organization lock.
func (s *BindingManagementStore) WithBindingTransaction(ctx context.Context, organization identity.OrganizationID, mutate func(administration.BindingTransaction) error) error {
	return s.withPolicyTransaction(ctx, organization, func(transaction *bindingManagementTransaction) error { return mutate(transaction) })
}

// withPolicyTransaction shares policy lock, revision and commit ownership across management consumers.
func (s *BindingManagementStore) withPolicyTransaction(ctx context.Context, organization identity.OrganizationID, mutate func(*bindingManagementTransaction) error) error {
	if err := bindingPolicyContextError(ctx); err != nil {
		return err
	}
	organizationID, valid := parseBindingPolicyUUID(string(organization))
	if !valid {
		return administration.ErrBindingManagementInactive
	}
	transaction, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadWrite})
	if err != nil {
		return bindingManagementStorageError(ctx)
	}
	defer func() { _ = transaction.Rollback(ctx) }()
	queries := db.New(transaction)
	if err := queries.LockBindingOrganization(ctx, db.LockBindingOrganizationParams{LockClass: lockClassAdministration, LockObject: organizationLockObject(organization)}); err != nil {
		return bindingManagementStorageError(ctx)
	}
	revision, err := queries.LockBindingRevision(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return administration.ErrBindingManagementInactive
	}
	if err != nil {
		return bindingManagementStorageError(ctx)
	}
	if revision <= 0 {
		return administration.ErrBindingManagementInactive
	}
	policy := &bindingManagementTransaction{queries: queries, organization: organization, organizationID: organizationID, revision: revision}
	if err := mutate(policy); err != nil {
		return err
	}
	if err := bindingPolicyContextError(ctx); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return bindingManagementStorageError(ctx)
	}
	return nil
}

// bindingManagementTransaction pins all operations to one organization and locked revision.
type bindingManagementTransaction struct {
	queries        *db.Queries
	organization   identity.OrganizationID
	organizationID pgtype.UUID
	revision       int64
	at             time.Time
}

// bindingManagementStorageError keeps driver details outside the management boundary.
func bindingManagementStorageError(ctx context.Context) error {
	if err := bindingPolicyContextError(ctx); err != nil {
		return err
	}
	return administration.ErrBindingManagementUnavailable
}

// bindingPolicyContextError fences elapsed deadlines before committing policy state.
func bindingPolicyContextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, exists := ctx.Deadline(); exists && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

// parseBindingPolicyUUID rejects SQL-equivalent spellings that would split policy lock keys or identity comparisons.
func parseBindingPolicyUUID(value string) (pgtype.UUID, bool) {
	identifier, err := stringToUUID(value)
	return identifier, err == nil && identifier.Valid && uuidToString(identifier) == value
}
