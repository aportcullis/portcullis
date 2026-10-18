package identity_test

import (
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// bindingSnapshot returns an independent, unconditional organization grant for domain scenarios.
func bindingSnapshot() identity.RoleBinding {
	return identity.RoleBinding{ID: "binding-1", OrganizationID: "org-1", UserID: "user-1", RoleID: "role-1", Version: 1, Scope: identity.ResourceScope{Type: identity.ScopeOrganization}}
}

func TestRoleBindingScopeScenarios(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name     string
		scope    identity.ResourceScope
		resource identity.ResourceReference
		matches  bool
	}{
		{"organization administration", identity.ResourceScope{Type: identity.ScopeOrganization}, identity.ResourceReference{OrganizationID: "org-1", Type: identity.ResourceUser, ID: "user-2"}, true},
		{"exact connection", identity.ResourceScope{Type: identity.ScopeConnection, ConnectionID: "conn-1"}, identity.ResourceReference{OrganizationID: "org-1", Type: identity.ResourceConnection, ID: "conn-1", ConnectionID: "conn-1"}, true},
		{"request ancestry", identity.ResourceScope{Type: identity.ScopeConnection, ConnectionID: "conn-1"}, identity.ResourceReference{OrganizationID: "org-1", Type: identity.ResourceRequest, ID: "request-1", ConnectionID: "conn-1"}, true},
		{"bounded result ancestry", identity.ResourceScope{Type: identity.ScopeConnection, ConnectionID: "conn-1"}, identity.ResourceReference{OrganizationID: "org-1", Type: identity.ResourceResult, ID: "result-1", ConnectionID: "conn-1"}, true},
		{"foreign organization", identity.ResourceScope{Type: identity.ScopeOrganization}, identity.ResourceReference{OrganizationID: "org-2", Type: identity.ResourceUser, ID: "user-2"}, false},
		{"different connection", identity.ResourceScope{Type: identity.ScopeConnection, ConnectionID: "conn-1"}, identity.ResourceReference{OrganizationID: "org-1", Type: identity.ResourceRequest, ID: "request-1", ConnectionID: "conn-2"}, false},
		{"audit cannot inherit connection scope", identity.ResourceScope{Type: identity.ScopeConnection, ConnectionID: "conn-1"}, identity.ResourceReference{OrganizationID: "org-1", Type: identity.ResourceAudit, ID: "audit-1"}, false},
		{"missing request ancestry", identity.ResourceScope{Type: identity.ScopeOrganization}, identity.ResourceReference{OrganizationID: "org-1", Type: identity.ResourceRequest, ID: "request-1"}, false},
		{"forged connection ancestry", identity.ResourceScope{Type: identity.ScopeOrganization}, identity.ResourceReference{OrganizationID: "org-1", Type: identity.ResourceConnection, ID: "conn-1", ConnectionID: "conn-2"}, false},
		{"unmapped asset type", identity.ResourceScope{Type: identity.ScopeOrganization}, identity.ResourceReference{OrganizationID: "org-1", Type: "query_asset", ID: "asset-1"}, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			binding := bindingSnapshot()
			binding.Scope = scenario.scope
			if actual := binding.AppliesTo("user-1", scenario.resource); actual != scenario.matches {
				t.Fatalf("binding scope match=%t, want %t", actual, scenario.matches)
			}
		})
	}
}

func TestRoleBindingDataRefusals(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name   string
		change func(*identity.RoleBinding)
	}{
		{"missing identity", func(binding *identity.RoleBinding) { binding.ID = "" }},
		{"missing organization", func(binding *identity.RoleBinding) { binding.OrganizationID = "" }},
		{"missing principal", func(binding *identity.RoleBinding) { binding.UserID = "" }},
		{"missing role", func(binding *identity.RoleBinding) { binding.RoleID = "" }},
		{"invalid version", func(binding *identity.RoleBinding) { binding.Version = 0 }},
		{"unknown scope", func(binding *identity.RoleBinding) { binding.Scope.Type = "database" }},
		{"implicit condition removal", func(binding *identity.RoleBinding) { binding.Condition.Expression = "true" }},
		{"empty conditional binding", func(binding *identity.RoleBinding) { binding.HasCondition = true }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			binding := bindingSnapshot()
			scenario.change(&binding)
			if binding.Validate() == nil {
				t.Fatal("invalid binding data was accepted")
			}
		})
	}
	resource := identity.ResourceReference{OrganizationID: "org-1", Type: identity.ResourceUser, ID: "user-2"}
	if bindingSnapshot().AppliesTo("user-2", resource) {
		t.Fatal("foreign principal matched a binding")
	}
	revoked := bindingSnapshot()
	revoked.Revoked = true
	if revoked.AppliesTo("user-1", resource) {
		t.Fatal("revoked binding remained applicable")
	}
}

func TestConditionDataStaysIndependentOfRolesAndBindings(t *testing.T) {
	t.Parallel()
	source := identity.Condition{Title: "Temporary access", Description: "A binding-owned condition", Expression: " request.time < timestamp(\"2026-12-01T00:00:00Z\") ", LanguageVersion: identity.ConditionLanguageCELV1, EnvironmentVersion: identity.ConditionEnvironmentV1}
	if err := source.Validate(); err != nil {
		t.Fatal(err)
	}
	first := bindingSnapshot()
	first.Condition, first.HasCondition = source, true
	second := first
	second.ID, second.Version = "binding-2", 2
	second.Condition.Expression = "false"
	if first.Condition != source || first.Condition.Expression == second.Condition.Expression {
		t.Fatal("changing a binding changed another condition value")
	}
	if err := first.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"title", "expression", "language", "environment"} {
		invalid := source
		switch field {
		case "title":
			invalid.Title = "  "
		case "expression":
			invalid.Expression = "\n "
		case "language":
			invalid.LanguageVersion = "cel-v999"
		case "environment":
			invalid.EnvironmentVersion = "portcullis-v999"
		}
		if invalid.Validate() == nil {
			t.Errorf("invalid %s accepted", field)
		}
	}
}
