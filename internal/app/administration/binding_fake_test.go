package administration_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/app/condition"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/celcondition"
)

// bindingPolicyState holds observable committed policy data for rollback scenarios.
type bindingPolicyState struct {
	members         map[identity.UserID]identity.UserStatus
	roles           map[identity.RoleID]identity.Role
	bindings        map[identity.BindingID]identity.RoleBinding
	history         []identity.RoleBinding
	events          []audit.Event
	revision        int64
	revokedSessions int
}

// cloneBindingPolicyState isolates tentative writes from committed test state.
func cloneBindingPolicyState(state bindingPolicyState) bindingPolicyState {
	state.members = maps.Clone(state.members)
	state.roles = maps.Clone(state.roles)
	state.bindings = maps.Clone(state.bindings)
	state.history = slices.Clone(state.history)
	state.events = slices.Clone(state.events)
	return state
}

// bindingPolicyFake models the transactional consumer port, without consulting legacy membership roles.
type bindingPolicyFake struct {
	mutex    sync.Mutex
	state    bindingPolicyState
	catalog  []identity.Permission
	refusals []audit.Event
	fail     string
	wait     bool
	at       time.Time
}

func (repository *bindingPolicyFake) WithBindingTransaction(ctx context.Context, organization identity.OrganizationID, mutate func(administration.BindingTransaction) error) error {
	if repository.wait {
		<-ctx.Done()
		return ctx.Err()
	}
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if organization != "org" || repository.state.revision <= 0 {
		return administration.ErrBindingManagementInactive
	}
	transaction := &bindingPolicyTransaction{repository: repository, state: cloneBindingPolicyState(repository.state)}
	if err := mutate(transaction); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	repository.state = transaction.state
	return nil
}

func (repository *bindingPolicyFake) Record(_ context.Context, event audit.Event) error {
	repository.refusals = append(repository.refusals, event)
	return nil
}

// bindingPolicyTransaction exposes a tentative copy that is discarded on any refusal.
type bindingPolicyTransaction struct {
	repository *bindingPolicyFake
	state      bindingPolicyState
}

func (transaction *bindingPolicyTransaction) ObserveBindingMutationTime(context.Context, identity.UserID) (time.Time, error) {
	return transaction.repository.at, nil
}
func (transaction *bindingPolicyTransaction) LoadPermissionCatalog(context.Context) ([]identity.Permission, error) {
	return slices.Clone(transaction.repository.catalog), nil
}
func (transaction *bindingPolicyTransaction) LockMemberStatus(_ context.Context, user identity.UserID) (identity.UserStatus, error) {
	status, exists := transaction.state.members[user]
	if !exists {
		return "", identity.ErrUserNotFound
	}
	return status, nil
}
func (transaction *bindingPolicyTransaction) LoadRole(_ context.Context, role identity.RoleID) (identity.Role, error) {
	value, exists := transaction.state.roles[role]
	if !exists {
		return identity.Role{}, identity.ErrRoleNotFound
	}
	return value, nil
}
func (transaction *bindingPolicyTransaction) LockBindingVersion(_ context.Context, binding identity.BindingID) (identity.RoleBinding, error) {
	value, exists := transaction.state.bindings[binding]
	if !exists {
		return identity.RoleBinding{}, identity.ErrBindingNotFound
	}
	return value, nil
}
func (transaction *bindingPolicyTransaction) LockBindingScope(_ context.Context, scope identity.ResourceScope) error {
	if scope.Validate() != nil || (scope.Type == identity.ScopeConnection && scope.ConnectionID != "connection") {
		return identity.ErrInvalidResourceScope
	}
	return nil
}
func (transaction *bindingPolicyTransaction) LoadUnconditionalPermissions(_ context.Context, user identity.UserID) ([]identity.Permission, error) {
	permissions := make([]identity.Permission, 0)
	for _, binding := range transaction.state.bindings {
		if binding.OrganizationID == "org" && binding.UserID == user && !binding.Revoked && !binding.HasCondition && binding.Scope.Type == identity.ScopeOrganization {
			permissions = append(permissions, transaction.state.roles[binding.RoleID].Permissions...)
		}
	}
	return permissions, nil
}
func (transaction *bindingPolicyTransaction) InsertBinding(_ context.Context, definition identity.BindingDefinition) (identity.RoleBinding, error) {
	binding := identity.RoleBinding{ID: identity.BindingID(fmt.Sprintf("created-%d", len(transaction.state.history)+1)), OrganizationID: "org", UserID: definition.UserID, RoleID: definition.RoleID, Scope: definition.Scope, Condition: definition.Condition, HasCondition: definition.HasCondition, Version: 1}
	transaction.state.bindings[binding.ID] = binding
	transaction.state.history = append(transaction.state.history, binding)
	return binding, nil
}
func (transaction *bindingPolicyTransaction) AppendBindingVersion(_ context.Context, binding identity.RoleBinding, expected int64) error {
	if transaction.state.bindings[binding.ID].Version != expected {
		return identity.ErrBindingConflict
	}
	transaction.state.bindings[binding.ID] = binding
	transaction.state.history = append(transaction.state.history, binding)
	return nil
}
func (transaction *bindingPolicyTransaction) HasRecoveryAdministrator(ctx context.Context, required []identity.Permission) (bool, error) {
	for user, status := range transaction.state.members {
		if status != identity.StatusActive {
			continue
		}
		held, _ := transaction.LoadUnconditionalPermissions(ctx, user)
		if identity.ValidateDelegation(held, required) == nil {
			return true, nil
		}
	}
	return false, nil
}
func (transaction *bindingPolicyTransaction) FinalizeBindingChange(_ context.Context, _ identity.UserID, event audit.Event) (int64, error) {
	transaction.state.revokedSessions++
	transaction.state.revision++
	if transaction.repository.fail != "" {
		return 0, errors.New("private storage failure")
	}
	event.Metadata["authorization_revision"] = transaction.state.revision
	transaction.state.events = append(transaction.state.events, event)
	return transaction.state.revision, nil
}

// bindingManagementScenario injects real bounded condition preparation into transactional use cases.
func bindingManagementScenario(t *testing.T) (*bindingPolicyFake, *administration.BindingService, identity.BindingDefinition) {
	t.Helper()
	catalog := append(identity.EnforcedPermissions(), identity.BindingManagementPermissions()...)
	repository := &bindingPolicyFake{catalog: catalog, at: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC), state: bindingPolicyState{
		members:  map[identity.UserID]identity.UserStatus{"actor": identity.StatusActive, "target": identity.StatusActive},
		roles:    map[identity.RoleID]identity.Role{"administrator": {ID: "administrator", Version: 1, Permissions: catalog}, "reader": {ID: "reader", Version: 1, Permissions: []identity.Permission{identity.PermissionConnectionsGet}}, "executor": {ID: "executor", Version: 1, Permissions: []identity.Permission{identity.PermissionRequestsExecute}}, "empty": {ID: "empty", Version: 1}},
		bindings: map[identity.BindingID]identity.RoleBinding{"actor-grant": {ID: "actor-grant", OrganizationID: "org", UserID: "actor", RoleID: "administrator", Version: 1, Scope: identity.ResourceScope{Type: identity.ScopeOrganization}}, "target-grant": {ID: "target-grant", OrganizationID: "org", UserID: "target", RoleID: "reader", Version: 1, Scope: identity.ResourceScope{Type: identity.ScopeOrganization}}}, revision: 1,
	}}
	adapter, err := celcondition.New()
	if err != nil {
		t.Fatal(err)
	}
	conditions, err := condition.New(adapter, adapter, condition.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	service, err := administration.NewBindingService(repository, conditions, repository, administration.DefaultBindingConfig())
	if err != nil {
		t.Fatal(err)
	}
	return repository, service, identity.BindingDefinition{UserID: "target", RoleID: "reader", Scope: identity.ResourceScope{Type: identity.ScopeOrganization}}
}

// bindingCondition supplies an explicit supported source contract for scenario inputs.
func bindingCondition(expression string) identity.Condition {
	return identity.Condition{Title: "Access window", Expression: expression, LanguageVersion: identity.ConditionLanguageCELV1, EnvironmentVersion: identity.ConditionEnvironmentV1}
}
