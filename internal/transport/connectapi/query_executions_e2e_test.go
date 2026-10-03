package connectapi_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"connectrpc.com/connect"
	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

func TestGovernedExecutionOwnershipReplayAndStreamCSRF(t *testing.T) {
	env, jar, csrf, connectionID := reqEnv(t, 0)
	ctx := context.Background()
	requests := env.requestClient(jar)
	created, err := requests.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateAccessRequestRequest{ConnectionId: connectionID, Sql: "SELECT :exact AS exact_value, '=private-value' AS note", Params: []*portcullisv1.TypedParam{{Name: "exact", Type: "integer", Value: "9007199254740993"}}}), csrf))
	if err != nil {
		t.Fatal(err)
	}
	id := created.Msg.GetRequest().GetId()
	client := env.executionClient(jar)
	if _, err := client.Execute(ctx, withCSRF(connect.NewRequest(&portcullisv1.ExecuteQueryRequest{RequestId: id}), csrf)); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("draft execution: %v", err)
	}
	if _, err = requests.Submit(ctx, withCSRF(connect.NewRequest(&portcullisv1.SubmitAccessRequestRequest{Id: id, ExpectedVersion: 1}), csrf)); err != nil {
		t.Fatal(err)
	}
	finished, err := client.Execute(ctx, withCSRF(connect.NewRequest(&portcullisv1.ExecuteQueryRequest{RequestId: id}), csrf))
	if err != nil {
		t.Fatal(err)
	}
	if finished.Msg.GetState() != portcullisv1.AccessRequestState_ACCESS_REQUEST_STATE_SUCCEEDED || !finished.Msg.ResultAvailable {
		t.Fatalf("execution: %+v", finished.Msg)
	}
	page, err := client.GetResult(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetQueryResultRequest{RequestId: id, PageSize: 10}), csrf))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Msg.Rows) != 1 || page.Msg.Rows[0].Cells[0].GetIntValue() != "9007199254740993" {
		t.Fatalf("exact integer lost: %+v", page.Msg)
	}
	if _, err := client.Execute(ctx, withCSRF(connect.NewRequest(&portcullisv1.ExecuteQueryRequest{RequestId: id}), csrf)); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("replay: %v", err)
	}
	stream, err := client.ExportCSV(ctx, withCSRF(connect.NewRequest(&portcullisv1.ExportQueryCSVRequest{RequestId: id}), csrf))
	if err != nil {
		t.Fatal(err)
	}
	var csv strings.Builder
	for stream.Receive() {
		csv.Write(stream.Msg().Data)
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(csv.String(), "9007199254740993,'=private-value") {
		t.Fatalf("CSV escaping or precision lost: %q", csv.String())
	}
	missing, err := client.ExportCSV(ctx, connect.NewRequest(&portcullisv1.ExportQueryCSVRequest{RequestId: id}))
	if err == nil {
		for missing.Receive() {
		}
		err = missing.Err()
	}
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("CSV missing CSRF: %v", err)
	}
	anonymous, _ := cookiejar.New(nil)
	other, err := env.executionClient(anonymous).ExportCSV(ctx, connect.NewRequest(&portcullisv1.ExportQueryCSVRequest{RequestId: id}))
	if err == nil {
		for other.Receive() {
		}
		err = other.Err()
	}
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("anonymous CSV: %v", err)
	}
	env.seedUser(t, "other@example.com", "requester")
	phc, err := env.hasher.Hash(ctx, "requester-password-1")
	if err != nil {
		t.Fatal(err)
	}
	if err = env.store.SetPassword(ctx, identity.UserID(env.userIDByEmail(t, "other@example.com")), phc); err != nil {
		t.Fatal(err)
	}

	otherJar, _ := cookiejar.New(nil)
	if _, err := env.authClient(otherJar).Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: "other@example.com", Password: "requester-password-1"})); err != nil {
		t.Fatal(err)
	}
	otherCSRF := csrfFromJar(otherJar, env.serverURL)
	for _, operation := range []func() error{
		func() error {
			_, err := env.executionClient(otherJar).Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetQueryExecutionRequest{RequestId: id}), otherCSRF))
			return err
		},
		func() error {
			_, err := env.executionClient(otherJar).GetResult(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetQueryResultRequest{RequestId: id}), otherCSRF))
			return err
		},
	} {
		if err := operation(); connect.CodeOf(err) != connect.CodeNotFound {
			t.Fatalf("foreign result read: %v", err)
		}
	}
	var starts, finishes int
	var duration, affected *int64
	if err = env.pool.QueryRow(ctx, `select count(*) filter(where action='EXECUTION_STARTED'),count(*) filter(where action='EXECUTION_FINISHED'),max(duration_ms),max(rows_affected) from public.audit_events where target_id=$1`, id).Scan(&starts, &finishes, &duration, &affected); err != nil {
		t.Fatal(err)
	}
	if starts != 1 || finishes != 1 || duration == nil || affected == nil {
		t.Fatal("missing atomic execution metrics/evidence")
	}
	failed, err := requests.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateAccessRequestRequest{ConnectionId: connectionID, Sql: "SELECT 'PRIVATE_FAILURE_LITERAL'::integer"}), csrf))
	if err != nil {
		t.Fatal(err)
	}
	failedID := failed.Msg.Request.Id
	if _, err = requests.Submit(ctx, withCSRF(connect.NewRequest(&portcullisv1.SubmitAccessRequestRequest{Id: failedID, ExpectedVersion: 1}), csrf)); err != nil {
		t.Fatal(err)
	}
	failure, err := client.Execute(ctx, withCSRF(connect.NewRequest(&portcullisv1.ExecuteQueryRequest{RequestId: failedID}), csrf))
	if err != nil || failure.Msg.State != portcullisv1.AccessRequestState_ACCESS_REQUEST_STATE_FAILED {
		t.Fatalf("SQL failure outcome: %v", err)
	}
	for _, secret := range []string{"PRIVATE_FAILURE_LITERAL", "=private-value", "9007199254740993", "SELECT :exact"} {
		if strings.Contains(env.logs.String(), secret) {
			t.Fatal("execution payload or result leaked to application/error logs")
		}
	}
	var evidence string
	if err = env.pool.QueryRow(ctx, `select coalesce(string_agg(metadata::text, ' '), '') from public.audit_events where target_id in ($1,$2)`, id, failedID).Scan(&evidence); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(evidence, "PRIVATE_FAILURE_LITERAL") || strings.Contains(evidence, "=private-value") || strings.Contains(evidence, "9007199254740993") {
		t.Fatal("execution audit leaked a literal or result")
	}
	// A result cache crash must not cause target re-execution or remove durable success.
	if _, err = env.pool.Exec(ctx, `delete from result_cache.result_chunks;delete from result_cache.result_sets`); err != nil {
		t.Fatal(err)
	}
	if _, err = client.GetResult(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetQueryResultRequest{RequestId: id}), csrf)); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("lost result cache: %v", err)
	}
	state, err := client.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetQueryExecutionRequest{RequestId: id}), csrf))
	if err != nil || state.Msg.State != portcullisv1.AccessRequestState_ACCESS_REQUEST_STATE_SUCCEEDED {
		t.Fatal("cache loss rewrote execution")
	}
}

func TestExecuteRejectsReplacementPayloadOnWire(t *testing.T) {
	env, jar, csrf, connectionID := reqEnv(t, 0)
	ctx := context.Background()
	created, err := env.requestClient(jar).Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateAccessRequestRequest{ConnectionId: connectionID, Sql: "SELECT 7 AS original"}), csrf))
	if err != nil {
		t.Fatal(err)
	}
	id := created.Msg.Request.Id
	if _, err = env.requestClient(jar).Submit(ctx, withCSRF(connect.NewRequest(&portcullisv1.SubmitAccessRequestRequest{Id: id, ExpectedVersion: 1}), csrf)); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, env.tsURL+"/portcullis.v1.QueryExecutions/Execute", strings.NewReader(`{"requestId":"`+id+`","sql":"SELECT 999 AS replacement","params":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Connect-Protocol-Version", "1")
	request.Header.Set("X-CSRF-Token", csrf)
	response, err := (&http.Client{Transport: env.transport, Jar: jar}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("payload replacement accepted: HTTP %d", response.StatusCode)
	}
	var count int
	if err = env.pool.QueryRow(ctx, `select count(*) from public.query_executions where request_id=$1`, id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("replacement payload reached execution")
	}
}
