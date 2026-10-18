package main

import (
	"context"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/app/condition"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/celcondition"
	"github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// activeAuthorization builds the sole runtime grant source after migration and readiness validation.
func activeAuthorization(ctx context.Context, pool *pgxpool.Pool, store *postgres.IdentityStore, catalog []identity.Permission) (identity.OrganizationID, *postgres.BindingPolicyEvaluator, *authz.ActiveService, error) {
	organization, err := store.DefaultOrganizationID(ctx)
	if err != nil {
		return "", nil, nil, err
	}
	settings, err := authz.LoadResourceConfig()
	if err != nil {
		return "", nil, nil, err
	}
	conditionSettings, err := condition.LoadConfig()
	if err != nil {
		return "", nil, nil, err
	}
	adapter, err := celcondition.New()
	if err != nil {
		return "", nil, nil, err
	}
	conditions, err := condition.New(adapter, adapter, conditionSettings)
	if err != nil {
		return "", nil, nil, err
	}
	readSettings, err := authz.LoadReadConfig()
	if err != nil {
		return "", nil, nil, err
	}
	policy, err := postgres.NewBindingPolicyEvaluator(conditions, catalog, settings, readSettings)
	if err != nil {
		return "", nil, nil, err
	}
	repository, err := postgres.NewBindingPolicyStore(pool, policy, organization)
	if err != nil {
		return "", nil, nil, err
	}
	if err := repository.ValidateActivation(ctx); err != nil {
		return "", nil, nil, err
	}
	service, err := authz.NewActive(repository)
	return organization, policy, service, err
}
