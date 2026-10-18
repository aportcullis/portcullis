package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

func TestSessionCapabilitiesBeyondResourceCandidateBudget(t *testing.T) {
	for _, scenario := range []string{"scoped requests", "execution history", "live results", "ambiguous result", "expired results", "evicted results", "late conditional target", "false condition", "wrong scope", "revoked", "disabled", "no membership", "binding overflow", "binding summary overflow", "cancelled caller", "expired read budget"} {
		t.Run(scenario, func(t *testing.T) {
			environment := newAdministrationEnv(t)
			fixture := reqPreparationFixtureOver(t, connFixture{pool: environment.pool, store: pg.NewConnectionStore(environment.pool), org: environment.org, user: environment.admin.ID})
			principal := fixture.userWithCustomRole(t, "Capability reader", "requests.list", "requests.get", "requests.create", "requests.execute")
			first, second := fixture.liveConn(t, 1), fixture.liveConn(t, 1)
			template := fixture.draft(t, first)
			ctx := context.Background()
			if _, err := environment.pool.Exec(ctx, `insert into public.access_requests(id,organization_id,connection_id,requester_id,title,payload_key_version,payload_wrapped_dek,payload_nonce,payload_ciphertext,created_at,updated_at) select ('40000000-0000-0000-0000-'||lpad(sequence::text,12,'0'))::uuid,source.organization_id,source.connection_id,source.requester_id,'Capability fixture',source.payload_key_version,source.payload_wrapped_dek,source.payload_nonce,source.payload_ciphertext,clock_timestamp(),clock_timestamp() from public.access_requests source cross join generate_series(1,$2) sequence where source.id=$1`, template.ID, historyPopulation); err != nil {
				t.Fatal(err)
			}
			if scenario == "execution history" || scenario == "live results" || scenario == "ambiguous result" || scenario == "expired results" || scenario == "evicted results" {
				if _, err := environment.pool.Exec(ctx, `insert into public.query_executions(request_id,organization_id,owner,attempt_id,heartbeat,deadline,started_at,finished_at,outcome,result_id,result_expires_at) select request.id,request.organization_id,'capability-fixture',gen_random_uuid(),clock_timestamp(),clock_timestamp()+interval '1 hour',clock_timestamp(),clock_timestamp(),'succeeded',('50000000-0000-0000-0000-'||right(request.id::text,12))::uuid,clock_timestamp()+interval '1 hour' from public.access_requests request where request.organization_id=$1 and request.id::text like '40000000-%'`, environment.org); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "live results" || scenario == "ambiguous result" || scenario == "expired results" {
				if _, err := environment.pool.Exec(ctx, `insert into result_cache.result_sets(id,organization_id,owner_user_id,row_count,byte_size,created_at,expires_at,last_accessed_at,key_version,wrapped_dek) select execution.result_id,execution.organization_id,$2,0,0,clock_timestamp(),execution.result_expires_at,clock_timestamp(),1,'fixture'::bytea from public.query_executions execution where execution.organization_id=$1`, environment.org, fixture.requester); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "ambiguous result" {
				if _, err := environment.pool.Exec(ctx, `insert into public.query_executions(request_id,organization_id,owner,attempt_id,heartbeat,deadline,started_at,result_id,result_expires_at) values($1,$2,'duplicate-ancestry',gen_random_uuid(),clock_timestamp(),clock_timestamp()+interval '1 hour',clock_timestamp(),$3,clock_timestamp()+interval '1 hour')`, template.ID, environment.org, fmt.Sprintf("50000000-0000-0000-0000-%012d", historyPopulation)); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "expired results" {
				if _, err := environment.pool.Exec(ctx, `update result_cache.result_sets set expires_at=clock_timestamp()-interval '1 second' where organization_id=$1`, environment.org); err != nil {
					t.Fatal(err)
				}
			}
			if err := pg.Migrate(ctx, environment.pool); err != nil {
				t.Fatal(err)
			}
			target := first
			if scenario == "wrong scope" {
				target = second
			}
			if _, err := environment.pool.Exec(ctx, `update public.role_binding_versions set scope_type='connection',connection_id=$2 where user_id=$1`, principal, target); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "execution history":
				setHistoryCondition(t, environment, principal, `resource.type == "execution"`)
			case "ambiguous result":
				setHistoryCondition(t, environment, principal, fmt.Sprintf(`resource.id == "50000000-0000-0000-0000-%012d"`, historyPopulation))
			case "live results", "expired results", "evicted results":
				setHistoryCondition(t, environment, principal, `resource.type == "result"`)
			case "late conditional target":
				setHistoryCondition(t, environment, principal, fmt.Sprintf(`resource.id == "40000000-0000-0000-0000-%012d"`, historyPopulation))
			case "false condition":
				setHistoryCondition(t, environment, principal, "false")
			case "revoked":
				if _, err := environment.pool.Exec(ctx, `update public.role_binding_versions set revoked_at=clock_timestamp() where user_id=$1`, principal); err != nil {
					t.Fatal(err)
				}
			case "disabled":
				if _, err := environment.pool.Exec(ctx, `update public.users set status='disabled' where id=$1`, principal); err != nil {
					t.Fatal(err)
				}
			case "no membership":
				account, err := environment.store.CreateUser(ctx, "capability-outsider@example.com", "Outsider")
				if err != nil {
					t.Fatal(err)
				}
				principal = account.ID
			case "binding overflow":
				seedHistoryBindingOverflow(t, environment, principal)
				setHistoryCondition(t, environment, principal, "true")
			}
			if scenario == "binding summary overflow" {
				seedActiveFixtureRole(t, environment.pool, environment.org, principal, environment.roles["requester"])
			}
			settings := authz.DefaultReadConfig()
			if scenario == "binding summary overflow" {
				settings.MaxCandidates = 1
			}
			if scenario == "expired read budget" {
				settings.Timeout = time.Nanosecond
			}
			store, err := pg.NewBindingPolicyStore(environment.pool, activePolicyFixtureWithReads(t, environment.pool, settings), environment.org)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "cancelled caller" {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			result, err := store.SessionCapabilities(ctx, principal)
			refused := scenario == "ambiguous result" || scenario == "disabled" || scenario == "no membership" || scenario == "binding overflow" || scenario == "binding summary overflow" || scenario == "cancelled caller" || scenario == "expired read budget"
			if refused {
				if err == nil || result.Revision != 0 || len(result.Capabilities) != 0 || len(result.Permissions) != 0 || len(result.Bindings) != 0 {
					t.Fatalf("refused metadata disclosed: %+v %v", result, err)
				}
				if scenario == "expired read budget" && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("deadline identity lost: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("capabilities must survive %d resources: %v", historyPopulation, err)
			}
			if result.Revision <= 0 || len(result.Permissions) != 0 {
				t.Fatalf("scoped metadata flattened or lost: %+v", result)
			}
			expected := scenario != "false condition" && scenario != "wrong scope" && scenario != "revoked" && scenario != "expired results" && scenario != "evicted results"
			if slices.Contains(result.Capabilities, identity.PermissionRequestsGet) != expected {
				t.Fatalf("current scoped capability mismatch: %v", result.Capabilities)
			}
			if scenario == "late conditional target" && !slices.Contains(result.Capabilities, identity.PermissionRequestsList) {
				t.Fatalf("last-batch condition match lost: %v", result.Capabilities)
			}
			if (scenario == "false condition" || scenario == "revoked") && len(result.Capabilities) != 0 {
				t.Fatalf("unearned capabilities: %v", result.Capabilities)
			}
			if scenario != "revoked" && len(result.Bindings) != 1 {
				t.Fatalf("caller binding summary lost: %+v", result.Bindings)
			}
		})
	}
}

func TestSessionCapabilitiesIgnoreUnboundConnectionPopulation(t *testing.T) {
	environment := newAdministrationEnv(t)
	fixture := reqPreparationFixtureOver(t, connFixture{pool: environment.pool, store: pg.NewConnectionStore(environment.pool), org: environment.org, user: environment.admin.ID})
	principal := fixture.userWithCustomRole(t, "Scoped navigation", "connections.list", "requests.create")
	connectionID := fixture.liveConn(t, 1)
	ctx := context.Background()
	if _, err := environment.pool.Exec(ctx, `with clones as(insert into public.connections(id,organization_id,db_type,display_name,host,port,database_name,tls_mode,target_fingerprint,credential_key_version,credential_wrapped_dek,credential_nonce,credential_ciphertext,created_by,current_policy_version) select ('30000000-0000-0000-0000-'||lpad(sequence::text,12,'0'))::uuid,source.organization_id,source.db_type,'Capability '||sequence,source.host,source.port,source.database_name,source.tls_mode,source.target_fingerprint,source.credential_key_version,source.credential_wrapped_dek,source.credential_nonce,source.credential_ciphertext,source.created_by,1 from public.connections source cross join generate_series(1,$2) sequence where source.id=$1 returning id,organization_id,created_by) insert into public.connection_policy_versions(connection_id,organization_id,version,read_allowed,write_allowed,ddl_allowed,read_required_approvals,write_required_approvals,ddl_required_approvals,query_timeout_seconds,max_rows,max_result_bytes,created_by) select clones.id,clones.organization_id,1,source.read_allowed,source.write_allowed,source.ddl_allowed,source.read_required_approvals,source.write_required_approvals,source.ddl_required_approvals,source.query_timeout_seconds,source.max_rows,source.max_result_bytes,clones.created_by from clones cross join public.connection_policy_versions source where source.connection_id=$1 and source.version=1`, connectionID, historyPopulation); err != nil {
		t.Fatal(err)
	}
	if err := pg.Migrate(ctx, environment.pool); err != nil {
		t.Fatal(err)
	}
	if _, err := environment.pool.Exec(ctx, `update public.role_binding_versions set scope_type='connection',connection_id=$2 where user_id=$1`, principal, connectionID); err != nil {
		t.Fatal(err)
	}
	store, err := pg.NewBindingPolicyStore(environment.pool, activePolicyFixture(t, environment.pool), environment.org)
	if err != nil {
		t.Fatal(err)
	}
	view, err := store.SessionCapabilities(ctx, principal)
	if err != nil || !slices.Contains(view.Capabilities, identity.PermissionConnectionsList) || !slices.Contains(view.Capabilities, identity.PermissionRequestsCreate) || len(view.Permissions) != 0 {
		t.Fatalf("unbound connection population broke scoped navigation: %+v %v", view, err)
	}
	operation := authz.Operation{Principal: principal, Permission: identity.PermissionConnectionsList, Type: identity.ResourceConnection, ID: "30000000-0000-0000-0000-000000000001"}
	if err := store.AuthorizeOperation(ctx, principal, identity.PermissionConnectionsList, operation); !errors.Is(err, authz.ErrPermissionDenied) {
		t.Fatalf("navigation hint widened resource authority: %v", err)
	}
}
