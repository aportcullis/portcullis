package main

import (
	"context"
	"net/http"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/app/condition"
	"github.com/aportcullis/portcullis/internal/infra/celcondition"
	"github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/aportcullis/portcullis/internal/transport/connectapi"
	"github.com/aportcullis/portcullis/internal/transport/server"
)

// activeAdministrationMounts exposes policy services under the shared active authorization boundary.
func activeAdministrationMounts(ctx context.Context, pool *pgxpool.Pool, store *postgres.IdentityStore, policy *postgres.BindingPolicyEvaluator, options []connect.HandlerOption) ([]server.Mount, error) {
	organization, err := store.DefaultOrganizationID(ctx)
	if err != nil {
		return nil, err
	}
	managementSettings, err := administration.LoadBindingConfig()
	if err != nil {
		return nil, err
	}
	roleSettings, err := administration.LoadRolePolicyConfig()
	if err != nil {
		return nil, err
	}
	conditionSettings, err := condition.LoadConfig()
	if err != nil {
		return nil, err
	}
	resourceSettings, err := authz.LoadResourceConfig()
	if err != nil {
		return nil, err
	}
	adapter, err := celcondition.New()
	if err != nil {
		return nil, err
	}
	conditions, err := condition.New(adapter, adapter, conditionSettings)
	if err != nil {
		return nil, err
	}
	repository, auditor := postgres.NewBindingManagementStore(pool), postgres.NewAuditStore(pool)
	accounts, err := administration.NewAccountPolicyService(repository, auditor, managementSettings)
	if err != nil {
		return nil, err
	}
	roles, err := administration.NewRolePolicyService(repository, auditor, roleSettings)
	if err != nil {
		return nil, err
	}
	bindings, err := administration.NewBindingService(repository, conditions, auditor, managementSettings)
	if err != nil {
		return nil, err
	}
	readStore, err := postgres.NewAuthorizedManagementReadStore(pool, policy, organization)
	if err != nil {
		return nil, err
	}
	reader, err := administration.NewManagementReader(readStore, conditions, managementSettings, resourceSettings)
	if err != nil {
		return nil, err
	}
	managementOptions := append([]connect.HandlerOption(nil), options...)
	routes := connectapi.NewAdministrationRoutes(connectapi.NewUsersService(organization, accounts, reader), connectapi.NewRolesService(organization, roles, reader), connectapi.NewBindingsService(organization, bindings, reader), managementOptions...)
	mounts := make([]server.Mount, 0, len(routes))
	for _, route := range routes {
		mounts = append(mounts, server.Mount{Pattern: route.Pattern, Handler: http.MaxBytesHandler(route.Handler, server.MaxRequestBytes)})
	}
	return mounts, nil
}
