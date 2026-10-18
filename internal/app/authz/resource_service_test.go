package authz_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/app/condition"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/celcondition"
)

// resourceRepositoryFake supplies explicit trusted snapshots without legacy grants.
type resourceRepositoryFake struct {
	snapshot authz.AuthorizationSnapshot
	err      error
	calls    int
	wait     bool
}

func (repository *resourceRepositoryFake) LoadAuthorizationSnapshot(ctx context.Context, _ identity.UserID, _ identity.Permission, _ authz.ResourceTarget, _ int) (authz.AuthorizationSnapshot, error) {
	repository.calls++
	if repository.wait {
		<-ctx.Done()
		return authz.AuthorizationSnapshot{}, ctx.Err()
	}
	return repository.snapshot, repository.err
}

// resourceScenario prepares real CEL evaluation with an immutable authorization snapshot.
func resourceScenario(t *testing.T) (*resourceRepositoryFake, *authz.ResourceService, authz.ResourceTarget) {
	t.Helper()
	adapter, err := celcondition.New()
	if err != nil {
		t.Fatal(err)
	}
	conditions, err := condition.New(adapter, adapter, condition.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	target := authz.ResourceTarget{OrganizationID: "org", Type: identity.ResourceRequest, ID: "request"}
	repository := &resourceRepositoryFake{snapshot: authz.AuthorizationSnapshot{
		ActiveMember: true, Resource: identity.ResourceReference{OrganizationID: "org", Type: identity.ResourceRequest, ID: "request", ConnectionID: "connection"},
		EvaluatedAt: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC), Revision: 7,
		Grants: []authz.BindingGrant{{Binding: identity.RoleBinding{ID: "binding", OrganizationID: "org", UserID: "user", RoleID: "role", Version: 3, Scope: identity.ResourceScope{Type: identity.ScopeOrganization}}, Permission: identity.PermissionRequestsGet, RoleVersion: 2}},
	}}
	service, err := authz.NewResourceService(repository, conditions, identity.EnforcedPermissions(), authz.DefaultResourceConfig())
	if err != nil {
		t.Fatal(err)
	}
	return repository, service, target
}

// setResourceCondition attaches explicit versioned source to a scenario grant.
func setResourceCondition(grant *authz.BindingGrant, expression string) {
	grant.Binding.HasCondition = true
	grant.Binding.Condition = identity.Condition{Title: "Access window", Expression: expression, LanguageVersion: identity.ConditionLanguageCELV1, EnvironmentVersion: identity.ConditionEnvironmentV1}
}

func TestResourceAuthorizationGrantsAdditively(t *testing.T) {
	for _, scenario := range []string{"organization", "exact connection", "trusted time", "invalid condition beside valid grant", "expired grant beside valid grant"} {
		t.Run(scenario, func(t *testing.T) {
			repository, service, target := resourceScenario(t)
			grant := &repository.snapshot.Grants[0]
			switch scenario {
			case "exact connection":
				grant.Binding.Scope = identity.ResourceScope{Type: identity.ScopeConnection, ConnectionID: "connection"}
			case "trusted time":
				setResourceCondition(grant, `request.time < timestamp("2026-10-18T12:00:01Z") && resource.connection_id == "connection"`)
			case "invalid condition beside valid grant", "expired grant beside valid grant":
				rejected := *grant
				rejected.Binding.ID = "rejected"
				expression := `unknown.attribute == "x"`
				if scenario == "expired grant beside valid grant" {
					expression = `request.time < timestamp("2026-10-18T12:00:00Z")`
				}
				setResourceCondition(&rejected, expression)
				repository.snapshot.Grants = append([]authz.BindingGrant{rejected}, repository.snapshot.Grants...)
			}
			decision, err := service.AuthorizeResource(context.Background(), "user", identity.PermissionRequestsGet, target)
			if err != nil || decision.BindingID != "binding" || decision.BindingVersion != 3 || decision.RoleVersion != 2 || decision.Revision != 7 || !decision.EvaluatedAt.Equal(repository.snapshot.EvaluatedAt) {
				t.Fatalf("grant checkpoint: %+v, %v", decision, err)
			}
		})
	}
}

func TestResourceAuthorizationRefusesUnsafeSnapshots(t *testing.T) {
	scenarios := []struct {
		name  string
		alter func(*resourceRepositoryFake)
		want  error
	}{
		{"no bindings despite legacy membership", func(repository *resourceRepositoryFake) { repository.snapshot.Grants = nil }, authz.ErrPermissionDenied},
		{"disabled member", func(repository *resourceRepositoryFake) {
			repository.snapshot.ActiveMember = false
		}, authz.ErrPermissionDenied},
		{"foreign organization", func(repository *resourceRepositoryFake) {
			repository.snapshot.Grants[0].Binding.OrganizationID = "foreign"
		}, authz.ErrInvalidSnapshot},
		{"foreign principal", func(repository *resourceRepositoryFake) {
			repository.snapshot.Grants[0].Binding.UserID = "foreign"
		}, authz.ErrInvalidSnapshot},
		{"different connection", func(repository *resourceRepositoryFake) {
			repository.snapshot.Grants[0].Binding.Scope = identity.ResourceScope{Type: identity.ScopeConnection, ConnectionID: "other"}
		}, authz.ErrPermissionDenied},
		{"forged ancestry response", func(repository *resourceRepositoryFake) {
			repository.snapshot.Resource.ID = "other"
		}, authz.ErrInvalidSnapshot},
		{"zero revision", func(repository *resourceRepositoryFake) { repository.snapshot.Revision = 0 }, authz.ErrInvalidSnapshot},
		{"missing clock", func(repository *resourceRepositoryFake) {
			repository.snapshot.EvaluatedAt = time.Time{}
		}, authz.ErrInvalidSnapshot},
		{"missing role version", func(repository *resourceRepositoryFake) {
			repository.snapshot.Grants[0].RoleVersion = 0
		}, authz.ErrInvalidSnapshot},
		{"wrong permission", func(repository *resourceRepositoryFake) {
			repository.snapshot.Grants[0].Permission = identity.PermissionRequestsExecute
		}, authz.ErrInvalidSnapshot},
		{"oversized candidates even with valid prefix", func(repository *resourceRepositoryFake) {
			repository.snapshot.Grants = make([]authz.BindingGrant, 65)
		}, authz.ErrBindingLimitExceeded},
		{"storage failure", func(repository *resourceRepositoryFake) {
			repository.err = errors.New("private driver detail")
		}, authz.ErrSnapshotUnavailable},
		{"inactive authorization", func(repository *resourceRepositoryFake) {
			repository.err = authz.ErrAuthorizationInactive
		}, authz.ErrAuthorizationInactive},
		{"revoked", func(repository *resourceRepositoryFake) {
			repository.snapshot.Grants[0].Binding.Revoked = true
		}, authz.ErrPermissionDenied},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			repository, service, target := resourceScenario(t)
			scenario.alter(repository)
			_, err := service.AuthorizeResource(context.Background(), "user", identity.PermissionRequestsGet, target)
			if !errors.Is(err, scenario.want) {
				t.Fatalf("error=%v want %v", err, scenario.want)
			}
		})
	}
}

func TestResourceAuthorizationRefusesConditionsWithoutFallback(t *testing.T) {
	for _, expression := range []string{`false`, `resource.connection_id == "foreign"`, `request.time < timestamp("2026-10-18T12:00:00Z")`, `principal.missing == "x"`, `resource.id.matches(".*")`} {
		t.Run(expression, func(t *testing.T) {
			repository, service, target := resourceScenario(t)
			setResourceCondition(&repository.snapshot.Grants[0], expression)
			_, err := service.AuthorizeResource(context.Background(), "user", identity.PermissionRequestsGet, target)
			if !errors.Is(err, authz.ErrPermissionDenied) {
				t.Fatalf("condition granted: %v", err)
			}
		})
	}
	t.Run("metadata is not implicit absence", func(t *testing.T) {
		repository, service, target := resourceScenario(t)
		repository.snapshot.Grants[0].Binding.Condition.Title = "stray"
		_, err := service.AuthorizeResource(context.Background(), "user", identity.PermissionRequestsGet, target)
		if !errors.Is(err, authz.ErrPermissionDenied) {
			t.Fatal(err)
		}
	})
}

func TestResourceAuthorizationEnforcesOperationShapes(t *testing.T) {
	for _, scenario := range []struct {
		permission    identity.Permission
		resource      identity.ResourceType
		allowed       bool
		unconditional bool
	}{
		{identity.PermissionConnectionsGet, identity.ResourceConnection, true, false},
		{identity.PermissionPoliciesUpdate, identity.ResourcePolicy, true, false},
		{identity.PermissionRequestsGet, identity.ResourceResult, true, false},
		{identity.PermissionRequestsExecute, identity.ResourceExecution, true, false},
		{identity.PermissionConnectionsCreate, identity.ResourceConnection, false, false},
		{identity.PermissionConnectionsCreate, identity.ResourceOrganization, true, false},
		{identity.PermissionConnectionsTest, identity.ResourceOrganization, true, false},
		{identity.PermissionUsersList, identity.ResourceOrganization, true, false},
		{identity.PermissionRolesCreate, identity.ResourceOrganization, true, true},
		{identity.PermissionAuditGet, identity.ResourceAudit, true, false},
		{identity.PermissionAuditGet, identity.ResourceConnection, false, false},
		{identity.PermissionRequestsExecute, identity.ResourceResult, false, false},
		{identity.PermissionUsersUpdate, identity.ResourceUser, false, false},
		{identity.PermissionUsersUpdate, identity.ResourceUser, true, true},
		{identity.PermissionRolesDelete, identity.ResourceRole, false, false},
		{identity.PermissionRolesDelete, identity.ResourceRole, true, true},
	} {
		t.Run(string(scenario.permission)+"/"+string(scenario.resource)+"/"+fmt.Sprint(scenario.unconditional), func(t *testing.T) {
			repository, service, target := resourceScenario(t)
			target.Type = scenario.resource
			repository.snapshot.Resource.Type = scenario.resource
			if scenario.resource == identity.ResourceConnection {
				repository.snapshot.Resource.ConnectionID = target.ID
			}
			if scenario.resource == identity.ResourceUser || scenario.resource == identity.ResourceRole || scenario.resource == identity.ResourceAudit || scenario.resource == identity.ResourceOrganization {
				repository.snapshot.Resource.ConnectionID = ""
			}
			if scenario.resource == identity.ResourceOrganization {
				target.ID = string(target.OrganizationID)
				repository.snapshot.Resource.ID = target.ID
			}
			repository.snapshot.Grants[0].Permission = scenario.permission
			if !scenario.unconditional {
				setResourceCondition(&repository.snapshot.Grants[0], `true`)
			}
			_, err := service.AuthorizeResource(context.Background(), "user", scenario.permission, target)
			if (err == nil) != scenario.allowed {
				t.Fatalf("allowed=%t error=%v", scenario.allowed, err)
			}
		})
	}
	t.Run("unknown catalog key never reads repository", func(t *testing.T) {
		repository, service, target := resourceScenario(t)
		_, err := service.AuthorizeResource(context.Background(), "user", "unknown.key", target)
		if !errors.Is(err, authz.ErrUnknownPermission) || repository.calls != 0 {
			t.Fatalf("calls=%d error=%v", repository.calls, err)
		}
	})
}

func TestResourceAuthorizationHonorsCancellation(t *testing.T) {
	repository, service, target := resourceScenario(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.AuthorizeResource(ctx, "user", identity.PermissionRequestsGet, target); !errors.Is(err, context.Canceled) || repository.calls != 0 {
		t.Fatalf("cancel: %v calls=%d", err, repository.calls)
	}
	repository.wait = true
	ctx, cancel = context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := service.AuthorizeResource(ctx, "user", identity.PermissionRequestsGet, target); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline: %v", err)
	}
}

// expiredResourceContext models an elapsed deadline before its cancellation signal is delivered.
type expiredResourceContext struct{ context.Context }

func (expiredResourceContext) Deadline() (time.Time, bool) {
	return time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), true
}

func TestResourceAuthorizationRefusesElapsedDeadlineBeforeSignal(t *testing.T) {
	repository, service, target := resourceScenario(t)
	_, err := service.AuthorizeResource(expiredResourceContext{context.Background()}, "user", identity.PermissionRequestsGet, target)
	if !errors.Is(err, context.DeadlineExceeded) || repository.calls != 0 {
		t.Fatalf("elapsed deadline: error=%v repository calls=%d", err, repository.calls)
	}
}
