package postgres_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

func TestAuthorizedRequestHistoryBeyondCandidateBudget(t *testing.T) {
	for _, scenario := range []string{"reviewer ascending", "reviewer descending", "clamped last page", "scoped reviewer", "scoped review with broad list", "conditional review with broad list", "resource condition", "own requests", "state filter", "empty state", "small batches", "list without reviewer", "false condition", "revoked before output", "count witness revoked", "ancestry changed before output", "cancelled caller", "expired read budget", "binding overflow", "foreign organization"} {
		t.Run(scenario, func(t *testing.T) {
			environment := newAdministrationEnv(t)
			fixture := reqPreparationFixtureOver(t, connFixture{pool: environment.pool, store: pg.NewConnectionStore(environment.pool), org: environment.org, user: environment.admin.ID})
			principal := fixture.userWithRole(t, "approver")
			first, second := fixture.liveConn(t, 1), fixture.liveConn(t, 1)
			template := fixture.draft(t, first)
			own := fixture.draft(t, second)
			ctx := context.Background()
			if _, err := environment.pool.Exec(ctx, `insert into public.access_requests(id,organization_id,connection_id,requester_id,title,payload_key_version,payload_wrapped_dek,payload_nonce,payload_ciphertext,created_at,updated_at) select ('20000000-0000-0000-0000-'||lpad(sequence::text,12,'0'))::uuid,source.organization_id,case when sequence%2=0 then $3::uuid else source.connection_id end,source.requester_id,'History',source.payload_key_version,source.payload_wrapped_dek,source.payload_nonce,source.payload_ciphertext,'2020-01-01'::timestamptz+sequence*interval '1 second','2020-01-01'::timestamptz+sequence*interval '1 second' from public.access_requests source cross join generate_series(1,$2) sequence where source.id=$1`, template.ID, historyPopulation, second); err != nil {
				t.Fatal(err)
			}
			if scenario == "state filter" {
				if _, err := environment.pool.Exec(ctx, `update public.access_requests set state='cancelled' where organization_id=$1 and id in(select id from public.access_requests where organization_id=$1 and id::text like '20000000-%' and right(id::text,1) in('0','2','4','6','8'))`, environment.org); err != nil {
					t.Fatal(err)
				}
			}
			if err := pg.Migrate(ctx, environment.pool); err != nil {
				t.Fatal(err)
			}
			if scenario == "own requests" {
				principal = fixture.requester
			}
			if scenario == "list without reviewer" {
				principal = fixture.requester
				if _, err := environment.pool.Exec(ctx, `update public.access_requests set requester_id=$2 where organization_id=$1 and id<>$3`, environment.org, environment.admin.ID, own.ID); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "scoped reviewer" || scenario == "scoped review with broad list" {
				if _, err := environment.pool.Exec(ctx, `update public.role_binding_versions set scope_type='connection',connection_id=$2 where user_id=$1`, principal, first); err != nil {
					t.Fatal(err)
				}
			}
			blocked := "20000000-0000-0000-0000-000000001025"
			if scenario == "resource condition" || scenario == "conditional review with broad list" {
				setHistoryCondition(t, environment, principal, fmt.Sprintf(`resource.id != %q`, blocked))
			}
			if scenario == "false condition" {
				setHistoryCondition(t, environment, principal, "false")
			}
			if scenario == "scoped review with broad list" || scenario == "conditional review with broad list" {
				seedActiveFixtureRole(t, environment.pool, environment.org, principal, environment.roles["requester"])
			}
			settings := authz.DefaultReadConfig()
			if scenario == "small batches" {
				settings.MaxCandidates = 17
			}
			if scenario == "expired read budget" {
				settings.Timeout = time.Nanosecond
			}
			if scenario == "binding overflow" {
				seedHistoryBindingOverflow(t, environment, principal)
			}
			store, err := pg.NewAuthorizedAccessRequestStore(environment.pool, historyPolicyFixtureWithReads(t, environment.pool, settings), environment.org)
			if err != nil {
				t.Fatal(err)
			}
			ctx = authz.WithOutputFence(authz.WithOperation(ctx, authz.Operation{Principal: principal, Permission: identity.PermissionRequestsList, Type: identity.ResourceRequest, Collection: true}))
			query := access.ListQuery{Page: 52, PageSize: 25, SortDescending: scenario == "reviewer descending"}
			if scenario == "state filter" {
				query.State = access.StateDraft
			}
			if scenario == "empty state" {
				query.State = access.StateCancelled
			}
			if scenario == "own requests" {
				query.RequesterID = principal
			}
			if scenario == "clamped last page" {
				query.Page = 999999
			}
			if scenario == "cancelled caller" {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			organization := environment.org
			if scenario == "foreign organization" {
				organization = identity.OrganizationID("ffffffff-ffff-ffff-ffff-ffffffffffff")
			}
			result, err := store.List(ctx, organization, query)
			if scenario == "cancelled caller" || scenario == "foreign organization" || scenario == "expired read budget" || scenario == "binding overflow" {
				if err == nil || len(result.Items) != 0 || result.TotalCount != 0 {
					t.Fatalf("refused history disclosed: %+v %v", result, err)
				}
				assertHistoryRefusal(t, scenario, err)
				return
			}
			if err != nil {
				t.Fatalf("history with %d requests must remain available: %v", historyPopulation, err)
			}
			expected := []string{}
			rows, err := environment.pool.Query(context.Background(), `select id::text,connection_id::text,requester_id::text,state from public.access_requests where organization_id=$1 order by created_at,id`, environment.org)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var identifier, connectionID, requester, state string
				if err := rows.Scan(&identifier, &connectionID, &requester, &state); err != nil {
					t.Fatal(err)
				}
				if query.State != "" && state != string(query.State) {
					continue
				}
				if scenario == "false condition" || (scenario == "resource condition" || scenario == "conditional review with broad list") && identifier == blocked || (scenario == "scoped reviewer" || scenario == "scoped review with broad list") && connectionID != string(first) || scenario == "list without reviewer" && requester != string(principal) {
					continue
				}
				expected = append(expected, identifier)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			if query.SortDescending {
				slices.Reverse(expected)
			}
			pages := (len(expected) + query.PageSize - 1) / query.PageSize
			page := max(1, min(query.Page, pages))
			start := (page - 1) * query.PageSize
			wanted := expected[start:min(start+query.PageSize, len(expected))]
			actual := make([]string, 0, len(result.Items))
			for _, item := range result.Items {
				actual = append(actual, string(item.Request.ID))
				if item.EffectiveState != access.StateDraft {
					t.Fatalf("unexpected state %s", item.EffectiveState)
				}
			}
			if result.TotalCount != int64(len(expected)) || result.Page != page || result.PageSize != query.PageSize || result.TotalPages != pages || !slices.Equal(actual, wanted) {
				t.Fatalf("inconsistent authorized requests: total=%d page=%d pages=%d ids=%v want total=%d page=%d pages=%d ids=%v", result.TotalCount, result.Page, result.TotalPages, actual, len(expected), page, pages, wanted)
			}
			if scenario == "revoked before output" {
				setHistoryCondition(t, environment, principal, "false")
			}
			if scenario == "count witness revoked" {
				setHistoryCondition(t, environment, principal, `resource.id != "20000000-0000-0000-0000-000000000001"`)
			}
			if scenario == "ancestry changed before output" {
				if _, err := environment.pool.Exec(ctx, `update public.access_requests set connection_id=$2 where organization_id=$1 and id='20000000-0000-0000-0000-000000000001'`, environment.org, second); err != nil {
					t.Fatal(err)
				}
			}
			err = authz.CheckOutput(ctx)
			if (err != nil) != (scenario == "revoked before output" || scenario == "count witness revoked" || scenario == "ancestry changed before output") {
				t.Fatalf("output error=%v", err)
			}
		})
	}
}
