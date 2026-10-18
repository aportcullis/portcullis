package administration_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/administration"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

func TestBindingManagementCreatesExplicitGrants(t *testing.T) {
	for _, scenario := range []string{"organization", "connection", "conditional organization", "conditional connection", "empty role"} {
		t.Run(scenario, func(t *testing.T) {
			repository, service, definition := bindingManagementScenario(t)
			switch scenario {
			case "connection", "conditional connection":
				definition.Scope = identity.ResourceScope{Type: identity.ScopeConnection, ConnectionID: "connection"}
			case "empty role":
				definition.RoleID = "empty"
			}
			if scenario == "conditional organization" || scenario == "conditional connection" {
				definition.HasCondition = true
				definition.Condition = bindingCondition(`request.time < timestamp("9999-01-01T00:00:00Z")`)
			}
			mutation, err := service.Create(context.Background(), "org", "actor", definition)
			if err != nil || mutation.Binding.ID == "" || mutation.Binding.Version != 1 || mutation.Binding.Definition() != definition || mutation.Revision != 2 || !mutation.OccurredAt.Equal(repository.at) {
				t.Fatalf("create: %+v %v", mutation, err)
			}
			if len(repository.state.events) != 1 || repository.state.events[0].Action != audit.ActionBindingCreated || repository.state.revokedSessions != 1 {
				t.Fatalf("create did not commit evidence and session revocation: %+v", repository.state)
			}
		})
	}
}

func TestBindingManagementPreservesConditionHistoryAcrossUpdatesAndRevocation(t *testing.T) {
	repository, service, definition := bindingManagementScenario(t)
	definition.HasCondition = true
	definition.Condition = bindingCondition(`true`)
	created, err := service.Create(context.Background(), "org", "actor", definition)
	if err != nil {
		t.Fatal(err)
	}
	preserved, err := service.Update(context.Background(), "org", "actor", created.Binding.ID, 1, identity.BindingUpdate{RoleID: "executor"})
	if err != nil || preserved.Binding.Condition != definition.Condition || !preserved.Binding.HasCondition || preserved.Binding.Version != 2 {
		t.Fatalf("preserve: %+v %v", preserved, err)
	}
	replacement := bindingCondition(`false`)
	replaced, err := service.Update(context.Background(), "org", "actor", created.Binding.ID, 2, identity.BindingUpdate{ConditionMode: identity.ConditionReplace, Condition: replacement})
	if err != nil || replaced.Binding.Condition != replacement || replaced.Binding.Version != 3 {
		t.Fatalf("replace: %+v %v", replaced, err)
	}
	cleared, err := service.Update(context.Background(), "org", "actor", created.Binding.ID, 3, identity.BindingUpdate{ConditionMode: identity.ConditionClear})
	if err != nil || cleared.Binding.HasCondition || cleared.Binding.Condition != (identity.Condition{}) || cleared.Binding.Version != 4 {
		t.Fatalf("clear: %+v %v", cleared, err)
	}
	revoked, err := service.Revoke(context.Background(), "org", "actor", created.Binding.ID, 4)
	if err != nil || !revoked.Binding.Revoked || revoked.Binding.Version != 5 || revoked.Revision != 6 {
		t.Fatalf("revoke: %+v %v", revoked, err)
	}
	if len(repository.state.history) != 5 || repository.state.history[0].Condition != definition.Condition || len(repository.state.events) != 5 || repository.state.revokedSessions != 5 {
		t.Fatalf("immutable history/evidence: %+v", repository.state)
	}
	if _, err := service.Update(context.Background(), "org", "actor", created.Binding.ID, 5, identity.BindingUpdate{RoleID: "reader"}); !errors.Is(err, identity.ErrBindingRevoked) {
		t.Fatalf("revival: %v", err)
	}
	if _, err := service.Revoke(context.Background(), "org", "actor", created.Binding.ID, 4); !errors.Is(err, identity.ErrBindingConflict) {
		t.Fatalf("stale replay: %v", err)
	}
}

func TestBindingManagementRefusesUnsafeWritesWithoutPartialEffects(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		alter func(*bindingPolicyFake, *identity.BindingDefinition)
		want  error
	}{
		{"self binding", func(_ *bindingPolicyFake, definition *identity.BindingDefinition) { definition.UserID = "actor" }, identity.ErrSelfAdministration},
		{"foreign member", func(_ *bindingPolicyFake, definition *identity.BindingDefinition) { definition.UserID = "foreign" }, identity.ErrUserNotFound},
		{"foreign role", func(_ *bindingPolicyFake, definition *identity.BindingDefinition) { definition.RoleID = "foreign" }, identity.ErrRoleNotFound},
		{"foreign connection", func(_ *bindingPolicyFake, definition *identity.BindingDefinition) {
			definition.Scope = identity.ResourceScope{Type: identity.ScopeConnection, ConnectionID: "foreign"}
		}, identity.ErrInvalidResourceScope},
		{"disabled actor", func(repository *bindingPolicyFake, _ *identity.BindingDefinition) {
			repository.state.members["actor"] = identity.StatusDisabled
		}, identity.ErrActorNotAuthorized},
		{"conditional manager even when true", func(repository *bindingPolicyFake, _ *identity.BindingDefinition) {
			grant := repository.state.bindings["actor-grant"]
			grant.HasCondition = true
			grant.Condition = bindingCondition(`true`)
			repository.state.bindings[grant.ID] = grant
		}, identity.ErrActorNotAuthorized},
		{"connection manager", func(repository *bindingPolicyFake, _ *identity.BindingDefinition) {
			grant := repository.state.bindings["actor-grant"]
			grant.Scope = identity.ResourceScope{Type: identity.ScopeConnection, ConnectionID: "connection"}
			repository.state.bindings[grant.ID] = grant
		}, identity.ErrActorNotAuthorized},
		{"delegation exceeds held rights", func(repository *bindingPolicyFake, _ *identity.BindingDefinition) {
			repository.state.roles["administrator"] = identity.Role{ID: "administrator", Version: 1, Permissions: identity.BindingManagementPermissions()}
		}, identity.ErrPrivilegeEscalation},
		{"invalid CEL", func(_ *bindingPolicyFake, definition *identity.BindingDefinition) {
			definition.HasCondition = true
			definition.Condition = bindingCondition(`undeclared.attribute == "yes"`)
		}, identity.ErrInvalidCondition},
		{"implicit condition absence", func(_ *bindingPolicyFake, definition *identity.BindingDefinition) {
			definition.Condition.Title = "stray"
		}, identity.ErrInvalidCondition},
		{"unsupported contract", func(_ *bindingPolicyFake, definition *identity.BindingDefinition) {
			definition.HasCondition = true
			definition.Condition = bindingCondition(`true`)
			definition.Condition.LanguageVersion = "future"
		}, identity.ErrInvalidCondition},
		{"missing catalog cannot borrow legacy authority", func(repository *bindingPolicyFake, _ *identity.BindingDefinition) {
			repository.catalog = slices.DeleteFunc(identity.EnforcedPermissions(), func(permission identity.Permission) bool { return strings.HasPrefix(string(permission), "bindings.") })
		}, administration.ErrBindingManagementInactive},
		{"missing revision cannot borrow legacy authority", func(repository *bindingPolicyFake, _ *identity.BindingDefinition) { repository.state.revision = 0 }, administration.ErrBindingManagementInactive},
		{"audit or storage failure", func(repository *bindingPolicyFake, _ *identity.BindingDefinition) { repository.fail = "audit" }, administration.ErrBindingManagementUnavailable},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			repository, service, definition := bindingManagementScenario(t)
			scenario.alter(repository, &definition)
			before := cloneBindingPolicyState(repository.state)
			mutation, err := service.Create(context.Background(), "org", "actor", definition)
			if !errors.Is(err, scenario.want) || mutation != (administration.BindingMutation{}) || !reflect.DeepEqual(before, repository.state) {
				t.Fatalf("refusal: %+v %v committed=%+v", mutation, err, repository.state)
			}
		})
	}
}

func TestBindingManagementChecksBothAffectedRolesAndOptimisticVersion(t *testing.T) {
	for _, scenario := range []string{"old role not held", "new role not held", "stale update", "stale revoke", "self update", "self revoke", "foreign binding", "unsupported stored contract"} {
		t.Run(scenario, func(t *testing.T) {
			repository, service, _ := bindingManagementScenario(t)
			binding := identity.BindingID("target-grant")
			expected := int64(1)
			want := identity.ErrPrivilegeEscalation
			switch scenario {
			case "old role not held":
				repository.state.roles["administrator"] = identity.Role{ID: "administrator", Version: 1, Permissions: append(identity.BindingManagementPermissions(), identity.PermissionRequestsExecute)}
			case "new role not held":
				repository.state.roles["administrator"] = identity.Role{ID: "administrator", Version: 1, Permissions: append(identity.BindingManagementPermissions(), identity.PermissionConnectionsGet)}
			case "stale update", "stale revoke":
				expected = 2
				want = identity.ErrBindingConflict
			case "self update", "self revoke":
				binding = "actor-grant"
				want = identity.ErrSelfAdministration
			case "foreign binding":
				binding = "foreign"
				want = identity.ErrBindingNotFound
			case "unsupported stored contract":
				grant := repository.state.bindings[binding]
				grant.HasCondition = true
				grant.Condition = bindingCondition(`true`)
				grant.Condition.EnvironmentVersion = "future"
				repository.state.bindings[binding] = grant
				want = identity.ErrInvalidCondition
			}
			before := cloneBindingPolicyState(repository.state)
			var err error
			if scenario == "stale revoke" || scenario == "self revoke" {
				_, err = service.Revoke(context.Background(), "org", "actor", binding, expected)
			} else {
				_, err = service.Update(context.Background(), "org", "actor", binding, expected, identity.BindingUpdate{RoleID: "executor", ConditionMode: identity.ConditionClear})
			}
			if !errors.Is(err, want) || !reflect.DeepEqual(before, repository.state) {
				t.Fatalf("affected roles/version: %v want %v", err, want)
			}
		})
	}
}

func TestBindingManagementProtectsCapabilityDefinedRecoveryAdministrator(t *testing.T) {
	for _, scenario := range []string{"last administrator", "conditional reserve", "scoped reserve", "disabled reserve", "independent reserve", "split reserve", "catalog extension"} {
		t.Run(scenario, func(t *testing.T) {
			repository, service, _ := bindingManagementScenario(t)
			piecePermission := identity.PermissionBindingsUpdate
			if scenario == "catalog extension" {
				piecePermission = "users.rotate"
				repository.catalog = append(repository.catalog, piecePermission)
			}
			required := identity.SelectRecoveryPermissions(repository.catalog)
			repository.state.roles["controller"] = identity.Role{ID: "controller", Version: 1, Permissions: []identity.Permission{identity.PermissionBindingsDelete, piecePermission}}
			repository.state.roles["piece"] = identity.Role{ID: "piece", Version: 1, Permissions: []identity.Permission{piecePermission}}
			rest := slices.DeleteFunc(slices.Clone(required), func(permission identity.Permission) bool { return permission == piecePermission })
			repository.state.roles["rest"] = identity.Role{ID: "rest", Version: 1, Permissions: rest}
			actorGrant := repository.state.bindings["actor-grant"]
			actorGrant.RoleID = "controller"
			repository.state.bindings[actorGrant.ID] = actorGrant
			targetGrant := repository.state.bindings["target-grant"]
			targetGrant.RoleID = "piece"
			repository.state.bindings[targetGrant.ID] = targetGrant
			repository.state.bindings["target-rest"] = identity.RoleBinding{ID: "target-rest", OrganizationID: "org", UserID: "target", RoleID: "rest", Version: 1, Scope: identity.ResourceScope{Type: identity.ScopeOrganization}}
			if scenario != "last administrator" && scenario != "catalog extension" {
				repository.state.members["reserve"] = identity.StatusActive
				reserve := identity.RoleBinding{ID: "reserve", OrganizationID: "org", UserID: "reserve", RoleID: "administrator", Version: 1, Scope: identity.ResourceScope{Type: identity.ScopeOrganization}}
				switch scenario {
				case "conditional reserve":
					reserve.HasCondition = true
					reserve.Condition = bindingCondition(`true`)
				case "scoped reserve":
					reserve.Scope = identity.ResourceScope{Type: identity.ScopeConnection, ConnectionID: "connection"}
				case "disabled reserve":
					repository.state.members["reserve"] = identity.StatusDisabled
				case "split reserve":
					reserve.RoleID = "piece"
					extra := reserve
					extra.ID = "reserve-rest"
					extra.RoleID = "rest"
					repository.state.bindings[extra.ID] = extra
				}
				repository.state.bindings[reserve.ID] = reserve
			}
			before := cloneBindingPolicyState(repository.state)
			mutation, err := service.Revoke(context.Background(), "org", "actor", "target-grant", 1)
			allowed := scenario == "independent reserve" || scenario == "split reserve"
			if allowed {
				if err != nil || !mutation.Binding.Revoked {
					t.Fatalf("reserve administrator: %+v %v", mutation, err)
				}
			} else if !errors.Is(err, identity.ErrLastAdministrator) || !reflect.DeepEqual(before, repository.state) || len(repository.refusals) != 1 || repository.refusals[0].Metadata["reason"] != "last_administrator" {
				t.Fatalf("last administrator: %+v %v", mutation, err)
			}
		})
	}
}

func TestBindingManagementHonorsCancellation(t *testing.T) {
	repository, service, definition := bindingManagementScenario(t)
	repository.wait = true
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := service.Create(ctx, "org", "actor", definition); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("bounded lock wait: %v", err)
	}
}

func TestBindingManagementRejectsAmbiguousConditionEdits(t *testing.T) {
	for _, update := range []identity.BindingUpdate{
		{ConditionMode: identity.ConditionPreserve, Condition: bindingCondition(`true`)},
		{ConditionMode: identity.ConditionClear, Condition: bindingCondition(`true`)},
		{ConditionMode: "unknown"},
		{ConditionMode: identity.ConditionReplace},
		{ConditionMode: identity.ConditionReplace, Condition: bindingCondition(`resource.id.matches(".*")`)},
	} {
		repository, service, _ := bindingManagementScenario(t)
		before := cloneBindingPolicyState(repository.state)
		_, err := service.Update(context.Background(), "org", "actor", "target-grant", 1, update)
		if !errors.Is(err, identity.ErrInvalidCondition) || !reflect.DeepEqual(before, repository.state) {
			t.Fatalf("ambiguous condition edit: %+v %v", update, err)
		}
	}
}

func TestBindingManagementRevokesConditionalGrantsWithoutErasingSource(t *testing.T) {
	for _, expression := range []string{`true`, `false`, `request.time < timestamp("2000-01-01T00:00:00Z")`, `undeclared.attribute == "legacy source"`} {
		repository, service, _ := bindingManagementScenario(t)
		current := repository.state.bindings["target-grant"]
		current.HasCondition = true
		current.Condition = bindingCondition(expression)
		repository.state.bindings[current.ID] = current
		mutation, err := service.Revoke(context.Background(), "org", "actor", current.ID, 1)
		if err != nil || !mutation.Binding.Revoked || mutation.Binding.Condition != current.Condition || !mutation.Binding.HasCondition {
			t.Fatalf("conditional revoke: %+v %v", mutation, err)
		}
	}
}

func TestBindingManagementRejectsInvalidConfigurationAndExpiredContext(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Second, 5*time.Second + time.Nanosecond} {
		if err := (administration.BindingConfig{Timeout: timeout}).Validate(); !errors.Is(err, administration.ErrInvalidBindingManagementConfig) {
			t.Fatalf("timeout=%v error=%v", timeout, err)
		}
	}
	for _, timeout := range []time.Duration{time.Nanosecond, 2 * time.Second, 5 * time.Second} {
		if err := (administration.BindingConfig{Timeout: timeout}).Validate(); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PORTCULLIS_BINDING_MANAGEMENT_TIMEOUT", "750ms")
	settings, err := administration.LoadBindingConfig()
	if err != nil || settings.Timeout != 750*time.Millisecond {
		t.Fatalf("environment: %+v %v", settings, err)
	}
	t.Setenv("PORTCULLIS_BINDING_MANAGEMENT_TIMEOUT", "0s")
	if _, err := administration.LoadBindingConfig(); err == nil {
		t.Fatal("zero deadline accepted")
	}
	t.Setenv("PORTCULLIS_BINDING_MANAGEMENT_TIMEOUT", "invalid")
	if _, err := administration.LoadBindingConfig(); err == nil {
		t.Fatal("malformed deadline accepted")
	}
	repository, service, definition := bindingManagementScenario(t)
	before := cloneBindingPolicyState(repository.state)
	_, err = service.Create(expiredBindingManagementContext{context.Background()}, "org", "actor", definition)
	if !errors.Is(err, context.DeadlineExceeded) || !reflect.DeepEqual(before, repository.state) {
		t.Fatalf("elapsed deadline: %v", err)
	}
}

// expiredBindingManagementContext models a passed deadline whose cancellation signal has not yet arrived.
type expiredBindingManagementContext struct{ context.Context }

func (expiredBindingManagementContext) Deadline() (time.Time, bool) {
	return time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), true
}
