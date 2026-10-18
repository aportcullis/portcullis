package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/google/uuid"
)

func TestBindingActivationExecutionRechecksRequesterAndApprovalPolicy(t *testing.T) {
	for _, stage := range []string{"admission", "heartbeat"} {
		for _, scenario := range []string{"preserved grants", "matching connection", "true requester condition", "true approver condition", "false requester condition", "false approver condition", "foreign connection", "revoked requester"} {
			t.Run(stage+"/"+scenario, func(t *testing.T) {
				environment := newAdministrationEnv(t)
				fixture := reqPreparationFixtureOver(t, connFixture{pool: environment.pool, store: pg.NewConnectionStore(environment.pool), org: environment.org, user: environment.admin.ID})
				approver := fixture.userWithRole(t, "approver")
				connectionID, foreignConnection := fixture.liveConn(t, 1), fixture.liveConn(t, 1)
				request := fixture.submitted(t, connectionID, 1)
				ctx := context.Background()
				if err := pg.Migrate(ctx, environment.pool); err != nil {
					t.Fatal(err)
				}
				store, err := pg.NewAuthorizedAccessRequestStore(environment.pool, activePolicyFixture(t, environment.pool), environment.org)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := store.Approve(ctx, fixture.org, request.ID, approver, "", time.Hour, reqEvent(audit.ActionAccessRequestApproved, request.ID)); err != nil {
					t.Fatal(err)
				}
				acquire := func() (access.ExecutionLease, error) {
					return store.AcquireExecution(ctx, fixture.org, request.ID, fixture.requester, "activation", uuid.NewString(), request.Digest, reqEvent(audit.ActionExecutionStarted, request.ID))
				}
				var lease access.ExecutionLease
				if stage == "heartbeat" {
					lease, err = acquire()
					if err != nil {
						t.Fatal(err)
					}
				}
				success := true
				switch scenario {
				case "matching connection", "foreign connection":
					target := connectionID
					if scenario == "foreign connection" {
						target = foreignConnection
						success = false
					}
					if _, err := environment.pool.Exec(ctx, `update public.role_binding_versions set scope_type='connection',connection_id=$2 where user_id=$1`, fixture.requester, target); err != nil {
						t.Fatal(err)
					}
				case "true requester condition", "true approver condition", "false requester condition", "false approver condition":
					subject := fixture.requester
					if scenario == "true approver condition" || scenario == "false approver condition" {
						subject = approver
					}
					expression := "true"
					if scenario == "false requester condition" || scenario == "false approver condition" {
						expression = `request.time < timestamp("2000-01-01T00:00:00Z")`
						success = false
					}
					if _, err := environment.pool.Exec(ctx, `update public.role_binding_versions set condition_title='Execution window',condition_expression=$2,condition_language_version='cel-v1',condition_environment_version='portcullis-v1' where user_id=$1`, subject, expression); err != nil {
						t.Fatal(err)
					}
				case "revoked requester":
					success = false
					if _, err := environment.pool.Exec(ctx, `update public.role_binding_versions set revoked_at=clock_timestamp() where user_id=$1`, fixture.requester); err != nil {
						t.Fatal(err)
					}
				}
				if stage == "admission" {
					_, err = acquire()
				} else {
					err = store.HeartbeatExecution(ctx, fixture.org, lease)
				}
				if success && err != nil {
					t.Fatal(err)
				}
				if !success && err == nil {
					t.Fatal("execution continued without current requester/quorum authority")
				}
			})
		}
	}
}
