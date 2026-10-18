package postgres

import (
	"context"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

// authorizedConnection admits the current RPC before reading target metadata.
func (store *ConnectionStore) authorizedConnection(ctx context.Context, organization identity.OrganizationID, identifier connection.ConnectionID) (connection.Connection, error) {
	var result connection.Connection
	err := store.withAuthorizedRead(ctx, organization, func(ctx context.Context, scope *policyReadScope) error {
		if len(scope.decisions) == 0 || scope.decisions[0].decision.Resource.ConnectionID != string(identifier) {
			return authz.ErrPermissionDenied
		}
		copy := *store
		copy.policy, copy.q = nil, scope.queries
		var err error
		result, err = copy.GetByID(ctx, organization, identifier)
		return err
	})
	return result, err
}

// authorizedConnections filters the complete bounded candidate set before returning any rows.
func (store *ConnectionStore) authorizedConnections(ctx context.Context, organization identity.OrganizationID, archived bool) ([]connection.Connection, error) {
	var result []connection.Connection
	err := store.withAuthorizedRead(ctx, organization, func(ctx context.Context, scope *policyReadScope) error {
		organizationID, err := policyOrganizationUUID(organization)
		if err != nil {
			return err
		}
		rows, err := scope.queries.ListAuthorizationConnectionCandidates(ctx, db.ListAuthorizationConnectionCandidatesParams{OrganizationID: organizationID, IncludeArchived: archived, CandidateLimit: scope.policy.candidateQueryLimit()})
		if err != nil {
			return err
		}
		if len(rows) > scope.policy.readSettings.MaxCandidates {
			return authz.ErrBindingLimitExceeded
		}
		for _, row := range rows {
			allowed, err := scope.candidate(ctx, identity.ResourceConnection, uuidToString(row.ID), scope.operation.Permission)
			if err != nil {
				return err
			}
			if allowed {
				result = append(result, toConnection(row))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// authorizedTestMaterial refuses credential loading until current request or connection admission succeeds.
func (store *ConnectionStore) authorizedTestMaterial(ctx context.Context, organization identity.OrganizationID, identifier connection.ConnectionID) (connection.Connection, connection.SealedCredential, error) {
	var result connection.Connection
	var sealed connection.SealedCredential
	err := store.withAuthorizedRead(ctx, organization, func(ctx context.Context, scope *policyReadScope) error {
		if len(scope.decisions) == 0 {
			return authz.ErrPermissionDenied
		}
		reference := scope.decisions[0].decision.Resource
		if reference.ConnectionID != string(identifier) {
			return authz.ErrPermissionDenied
		}
		copy := *store
		copy.policy, copy.q = nil, scope.queries
		var err error
		result, sealed, err = copy.TestMaterial(ctx, organization, identifier)
		return err
	})
	return result, sealed, err
}
