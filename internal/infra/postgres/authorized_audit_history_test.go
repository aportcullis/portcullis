package postgres_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
)

func TestAuthorizedAuditHistoryBeyondCandidateBudget(t *testing.T) {
	for _, scenario := range []string{"organization ascending", "organization descending", "clamped last page", "resource condition", "small batches", "false condition", "revoked before output", "count witness revoked", "revision changed before output", "empty count revision changed", "disabled before output", "cancelled caller", "expired read budget", "binding overflow", "foreign organization"} {
		t.Run(scenario, func(t *testing.T) {
			environment := newAdministrationEnv(t)
			ctx := context.Background()
			if err := pg.Migrate(ctx, environment.pool); err != nil {
				t.Fatal(err)
			}
			if _, err := environment.pool.Exec(ctx, `insert into public.audit_events(id,organization_id,occurred_at,actor_type,actor_service,action,target_type,outcome) select ('10000000-0000-0000-0000-'||lpad(sequence::text,12,'0'))::uuid,$1,'2020-01-01'::timestamptz+sequence*interval '1 second','service','history','AUTH_LOGIN','user','succeeded' from generate_series(1,$2) sequence`, environment.org, historyPopulation); err != nil {
				t.Fatal(err)
			}
			blocked := "10000000-0000-0000-0000-000000001025"
			if scenario == "resource condition" {
				setHistoryCondition(t, environment, environment.admin.ID, fmt.Sprintf(`resource.id != %q`, blocked))
			}
			if scenario == "false condition" || scenario == "empty count revision changed" {
				setHistoryCondition(t, environment, environment.admin.ID, "false")
			}
			settings := authz.DefaultReadConfig()
			if scenario == "small batches" {
				settings.MaxCandidates = 17
			}
			if scenario == "expired read budget" {
				settings.Timeout = time.Nanosecond
			}
			if scenario == "binding overflow" {
				seedHistoryBindingOverflow(t, environment, environment.admin.ID)
			}
			store, err := pg.NewAuthorizedAuditStore(environment.pool, activePolicyFixtureWithReads(t, environment.pool, settings), environment.org)
			if err != nil {
				t.Fatal(err)
			}
			ctx = authz.WithOutputFence(authz.WithOperation(ctx, authz.Operation{Principal: environment.admin.ID, Permission: identity.PermissionAuditList, Type: identity.ResourceAudit, Collection: true}))
			query := audit.ListParams{Page: 52, PageSize: 25, SortDescending: scenario == "organization descending"}
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
				if err == nil || len(result.Events) != 0 || result.TotalCount != 0 {
					t.Fatalf("refused history disclosed: %+v %v", result, err)
				}
				assertHistoryRefusal(t, scenario, err)
				return
			}
			if err != nil {
				t.Fatalf("history with %d events must remain available: %v", historyPopulation, err)
			}
			var expected []string
			rows, err := environment.pool.Query(context.Background(), `select id::text from public.audit_events where organization_id=$1 order by occurred_at,id`, environment.org)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var identifier string
				if err := rows.Scan(&identifier); err != nil {
					t.Fatal(err)
				}
				if scenario != "false condition" && scenario != "empty count revision changed" && (scenario != "resource condition" || identifier != blocked) {
					expected = append(expected, identifier)
				}
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			if query.SortDescending {
				slices.Reverse(expected)
			}
			page := max(1, min(query.Page, (len(expected)+query.PageSize-1)/query.PageSize))
			start := (page - 1) * query.PageSize
			wanted := expected[start:min(start+query.PageSize, len(expected))]
			actual := make([]string, 0, len(result.Events))
			for _, event := range result.Events {
				actual = append(actual, event.ID)
			}
			if result.TotalCount != int64(len(expected)) || result.Page != page || result.PageSize != query.PageSize || !slices.Equal(actual, wanted) {
				t.Fatalf("inconsistent authorized history: total=%d page=%d ids=%v want total=%d page=%d ids=%v", result.TotalCount, result.Page, actual, len(expected), page, wanted)
			}
			if scenario == "revoked before output" {
				setHistoryCondition(t, environment, environment.admin.ID, "false")
			}
			if scenario == "count witness revoked" {
				setHistoryCondition(t, environment, environment.admin.ID, `resource.id != "10000000-0000-0000-0000-000000000001"`)
			}
			if scenario == "disabled before output" {
				if _, err := environment.pool.Exec(ctx, `update public.users set status='disabled' where id=$1`, environment.admin.ID); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "revision changed before output" || scenario == "empty count revision changed" {
				if _, err := environment.pool.Exec(ctx, `update public.organization_authorization_revisions set revision=revision+1 where organization_id=$1`, environment.org); err != nil {
					t.Fatal(err)
				}
			}
			err = authz.CheckOutput(ctx)
			refused := scenario == "revoked before output" || scenario == "count witness revoked" || scenario == "revision changed before output" || scenario == "empty count revision changed" || scenario == "disabled before output"
			if (err != nil) != refused {
				t.Fatalf("output refusal=%t error=%v", refused, err)
			}
		})
	}
}
