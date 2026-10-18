package postgres

import (
	"context"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuthorizedResultReader rechecks current ancestry and policy before any cached ciphertext is decrypted.
type AuthorizedResultReader struct {
	reader *ConnectionStore
}

// NewAuthorizedResultReader constructs a read-only result adapter for client-facing result services.
func NewAuthorizedResultReader(pool *pgxpool.Pool, policy *BindingPolicyEvaluator, organization identity.OrganizationID) (*AuthorizedResultReader, error) {
	reader, err := NewAuthorizedConnectionStore(pool, policy, organization)
	if err != nil {
		return nil, err
	}
	return &AuthorizedResultReader{reader: reader}, nil
}

// Put refuses client-facing writes; execution owns its separate encrypted-result writer.
func (store *AuthorizedResultReader) Put(context.Context, query.SealedResult) error {
	return authz.ErrPermissionDenied
}

// Get verifies the exact result identity and owner before loading its key envelope.
func (store *AuthorizedResultReader) Get(ctx context.Context, organization identity.OrganizationID, owner identity.UserID, identifier string) (query.SealedResult, error) {
	var result query.SealedResult
	err := store.reader.withAuthorizedRead(ctx, organization, func(ctx context.Context, scope *policyReadScope) error {
		if scope.operation.Principal != owner || len(scope.decisions) == 0 || scope.decisions[0].decision.Resource.ID != identifier {
			return authz.ErrPermissionDenied
		}
		copy := &ResultStore{connections: &ConnectionStore{pool: store.reader.pool, q: scope.queries}}
		var err error
		result, err = copy.Get(ctx, organization, owner, identifier)
		return err
	})
	if err != nil {
		return query.SealedResult{}, err
	}
	store.fenceResultLifetime(ctx, organization, owner, identifier)
	return result, nil
}

// GetChunk checks a fresh result decision before returning each encrypted chunk.
func (store *AuthorizedResultReader) GetChunk(ctx context.Context, organization identity.OrganizationID, owner identity.UserID, identifier string, index int) (query.SealedResultChunk, error) {
	var result query.SealedResultChunk
	err := store.reader.withAuthorizedRead(ctx, organization, func(ctx context.Context, scope *policyReadScope) error {
		if scope.operation.Principal != owner || len(scope.decisions) == 0 || scope.decisions[0].decision.Resource.ID != identifier {
			return authz.ErrPermissionDenied
		}
		copy := &ResultStore{connections: &ConnectionStore{pool: store.reader.pool, q: scope.queries}}
		var err error
		result, err = copy.GetChunk(ctx, organization, owner, identifier, index)
		return err
	})
	if err != nil {
		return query.SealedResultChunk{}, err
	}
	store.fenceResultLifetime(ctx, organization, owner, identifier)
	return result, nil
}

// fenceResultLifetime prevents a processed unary response from outliving its current owner and cache TTL.
func (store *AuthorizedResultReader) fenceResultLifetime(ctx context.Context, organization identity.OrganizationID, owner identity.UserID, identifier string) {
	authz.RegisterOutputCheck(ctx, func(checkContext context.Context) error {
		resultID, organizationID, userID, err := resultIDs(identifier, organization, owner)
		if err != nil {
			return query.ErrResultUnavailable
		}
		_, err = store.reader.q.GetResultSet(checkContext, db.GetResultSetParams{ID: resultID, OrganizationID: organizationID, OwnerUserID: userID})
		return notFound(err, query.ErrResultUnavailable)
	})
}
