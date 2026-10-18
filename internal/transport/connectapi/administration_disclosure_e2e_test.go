package connectapi_test

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
)

func TestAdministrationAPIStatusMutationDoesNotDiscloseGetOnlyPasswordPresence(t *testing.T) {
	environment := newAdministrationAPIEnv(t)
	ctx := context.Background()
	target := environment.createUser(t, "password-presence@example.com")
	reserve := environment.createUser(t, "password-presence-reserve@example.com")
	environment.seedBinding(t, reserve, environment.managerRole)
	if _, err := environment.pool.Exec(ctx, `insert into public.auth_methods (user_id,type,secret) values ($1,'password','fixture-credential')`, target); err != nil {
		t.Fatal(err)
	}
	controller := environment.seedRole(t, "Only status management", []string{"users.disable"})
	environment.replaceActorGrant(t, controller, nil)
	if _, err := environment.users.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetUserRequest{Id: target}), environment.csrf)); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("missing detail permission: %v", err)
	}
	enabled, err := environment.users.Enable(ctx, withCSRF(connect.NewRequest(&portcullisv1.EnableUserRequest{Id: target}), environment.csrf))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := protojson.Marshal(enabled.Msg)
	if err != nil || strings.Contains(string(encoded), "hasPassword") || strings.Contains(string(encoded), "email") || strings.Contains(string(encoded), "fixture-credential") {
		t.Fatalf("status response widened detail authority: %s %v", encoded, err)
	}
	disabled, err := environment.users.Disable(ctx, withCSRF(connect.NewRequest(&portcullisv1.DisableUserRequest{Id: target}), environment.csrf))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = protojson.Marshal(disabled.Msg)
	if err != nil || strings.Contains(string(encoded), "hasPassword") || strings.Contains(string(encoded), "email") {
		t.Fatalf("disable response widened detail authority: %s %v", encoded, err)
	}
}

func TestAdministrationAPIBindingMutationsDoNotDiscloseGetOnlyConditionSource(t *testing.T) {
	for _, operation := range []string{"update", "revoke"} {
		t.Run(operation, func(t *testing.T) {
			environment := newAdministrationAPIEnv(t)
			ctx := context.Background()
			target := environment.createUser(t, "source-presence@example.com")
			reserve := environment.createUser(t, "source-presence-reserve@example.com")
			environment.seedBinding(t, reserve, environment.managerRole)
			role := environment.seedRole(t, "Source-protected reader", []string{"connections.get"})
			binding := environment.seedBinding(t, target, role)
			transaction, err := environment.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = transaction.Rollback(ctx) }()
			if _, err := transaction.Exec(ctx, `insert into public.role_binding_versions (binding_id,organization_id,user_id,version,role_id,scope_type,condition_title,condition_expression,condition_language_version,condition_environment_version,created_at)
 select binding_id,organization_id,user_id,2,role_id,scope_type,'Protected','principal.id == "private-condition-marker"','cel-v1','portcullis-v1',clock_timestamp() from public.role_binding_versions where binding_id=$1 and version=1`, binding); err != nil {
				t.Fatal(err)
			}
			if _, err := transaction.Exec(ctx, `update public.role_bindings set current_version=2 where id=$1`, binding); err != nil {
				t.Fatal(err)
			}
			if err := transaction.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			controller := environment.seedRole(t, "Mutation without detail", []string{"bindings.update", "bindings.delete", "connections.get"})
			environment.replaceActorGrant(t, controller, nil)
			if _, err := environment.bindings.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetBindingRequest{Id: binding}), environment.csrf)); connect.CodeOf(err) != connect.CodeNotFound {
				t.Fatalf("missing condition detail permission: %v", err)
			}
			var encoded []byte
			if operation == "update" {
				updated, updateErr := environment.bindings.Update(ctx, withCSRF(connect.NewRequest(&portcullisv1.UpdateBindingRequest{Id: binding, ExpectedVersion: 2}), environment.csrf))
				if updateErr != nil {
					t.Fatal(updateErr)
				}
				encoded, err = protojson.Marshal(updated.Msg)
			} else {
				revoked, revokeErr := environment.bindings.Revoke(ctx, withCSRF(connect.NewRequest(&portcullisv1.RevokeBindingRequest{Id: binding, ExpectedVersion: 2}), environment.csrf))
				if revokeErr != nil {
					t.Fatal(revokeErr)
				}
				encoded, err = protojson.Marshal(revoked.Msg)
			}
			if err != nil || strings.Contains(string(encoded), "private-condition-marker") || strings.Contains(string(encoded), "expression") || strings.Contains(string(encoded), "scope") || strings.Contains(string(encoded), "userId") {
				t.Fatalf("mutation response widened detail authority: %s %v", encoded, err)
			}
		})
	}
}

func TestAdministrationAPIRoleMutationReturnsVersionWithoutReadMetadata(t *testing.T) {
	environment := newAdministrationAPIEnv(t)
	ctx := context.Background()
	target := environment.createUser(t, "role-metadata@example.com")
	reserve := environment.createUser(t, "role-metadata-reserve@example.com")
	environment.seedBinding(t, reserve, environment.managerRole)
	role := environment.seedRole(t, "Metadata reader", []string{"connections.get"})
	environment.seedBinding(t, target, role)
	controller := environment.seedRole(t, "Role write without read", []string{"roles.update", "connections.get"})
	environment.replaceActorGrant(t, controller, nil)
	if _, err := environment.roles.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetRoleRequest{Id: role}), environment.csrf)); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("missing role read permission: %v", err)
	}
	updated, err := environment.roles.Update(ctx, withCSRF(connect.NewRequest(&portcullisv1.UpdateRoleRequest{Id: role, ExpectedVersion: 1, Name: "Renamed metadata reader", Permissions: []string{"connections.get"}}), environment.csrf))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := protojson.Marshal(updated.Msg)
	if err != nil || strings.Contains(string(encoded), "permissions") || strings.Contains(string(encoded), "bindingCount") {
		t.Fatalf("role write widened read authority: %s %v", encoded, err)
	}
}
