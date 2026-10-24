package connectapi_test

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	resultapp "github.com/aportcullis/portcullis/internal/app/result"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
)

const resultOutputRowCount = 180

// resultOutputReader retains the client-facing result service's exported operations.
type resultOutputReader interface {
	CheckAccess(context.Context, identity.OrganizationID, identity.UserID, string) error
	Page(context.Context, identity.OrganizationID, identity.UserID, string, query.ResultPageQuery) (query.ResultPage, error)
	ExportCSVWithOptions(context.Context, identity.OrganizationID, identity.UserID, string, query.CSVExportOptions, io.Writer) error
}

// resultOutputProbe changes real database state only after the protected result has been read.
type resultOutputProbe struct {
	service         *resultapp.Service
	mutex           sync.Mutex
	hook            func(context.Context) error
	hookFailure     error
	hookCalls       atomic.Int32
	boundaryBytes   atomic.Int64
	additionalBytes atomic.Int64
}

// setBoundary owns the single mutation performed at this scenario's output boundary.
func (probe *resultOutputProbe) setBoundary(hook func(context.Context) error) {
	probe.mutex.Lock()
	defer probe.mutex.Unlock()
	probe.hook = hook
}

// crossBoundary executes the fixture mutation without replacing any authorization decision.
func (probe *resultOutputProbe) crossBoundary(ctx context.Context) error {
	probe.mutex.Lock()
	hook := probe.hook
	probe.hook = nil
	probe.mutex.Unlock()
	if hook == nil {
		return nil
	}
	probe.hookCalls.Add(1)
	err := hook(ctx)
	probe.mutex.Lock()
	probe.hookFailure = err
	probe.mutex.Unlock()
	return err
}

// boundaryFailure reports fixture failure independently of the RPC's authorization refusal.
func (probe *resultOutputProbe) boundaryFailure() error {
	probe.mutex.Lock()
	defer probe.mutex.Unlock()
	return probe.hookFailure
}

// CheckAccess delegates each fresh output check to the real authorized result repository.
func (probe *resultOutputProbe) CheckAccess(ctx context.Context, organization identity.OrganizationID, owner identity.UserID, identifier string) error {
	return probe.service.CheckAccess(ctx, organization, owner, identifier)
}

// Page crosses the boundary after decryption and before the transport releases its response.
func (probe *resultOutputProbe) Page(ctx context.Context, organization identity.OrganizationID, owner identity.UserID, identifier string, request query.ResultPageQuery) (query.ResultPage, error) {
	page, err := probe.service.Page(ctx, organization, owner, identifier, request)
	if err != nil {
		return query.ResultPage{}, err
	}
	if err := probe.crossBoundary(ctx); err != nil {
		return query.ResultPage{}, err
	}
	return page, nil
}

// ExportCSVWithOptions retains production serialization and its current-authority writer.
func (probe *resultOutputProbe) ExportCSVWithOptions(ctx context.Context, organization identity.OrganizationID, owner identity.UserID, identifier string, options query.CSVExportOptions, writer io.Writer) error {
	return probe.service.ExportCSVWithOptions(ctx, organization, owner, identifier, options, &resultOutputWriter{context: ctx, writer: writer, probe: probe})
}

// resultOutputWriter observes a complete data write after the separately flushed header.
type resultOutputWriter struct {
	context context.Context
	writer  io.Writer
	probe   *resultOutputProbe
	writes  int
	bytes   int64
	crossed bool
}

// Write changes authority after the first data write, leaving the next bounded output to production guards.
func (writer *resultOutputWriter) Write(data []byte) (int, error) {
	written, err := writer.writer.Write(data)
	writer.bytes += int64(written)
	if writer.crossed {
		writer.probe.additionalBytes.Add(int64(written))
	}
	if err != nil {
		return written, err
	}
	writer.writes++
	if writer.writes == 2 {
		writer.crossed = true
		writer.probe.boundaryBytes.Store(writer.bytes)
		if err := writer.probe.crossBoundary(writer.context); err != nil {
			return written, err
		}
	}
	return written, nil
}

// resultOutputFixture owns one real executed snapshot and its currently authenticated requester.
type resultOutputFixture struct {
	environment  *connsTestEnv
	probe        *resultOutputProbe
	jar          http.CookieJar
	csrf         string
	requestID    string
	connectionID string
	actorID      string
	organization string
	bindingID    string
	roleID       string
	resultID     string
	expiresAt    time.Time
}

// newResultOutputFixture executes SQL once before the scenario changes output authority.
func newResultOutputFixture(t *testing.T) resultOutputFixture {
	t.Helper()
	probe := &resultOutputProbe{}
	environment := newConnsTestEnvWithResultOutput(t, probe)
	jar, authentication, connections, _ := environment.clients()
	ctx := context.Background()
	const email, password = "output-owner@example.com", "correct-horse-battery"
	if _, err := authentication.Bootstrap(ctx, connect.NewRequest(&portcullisv1.BootstrapRequest{SetupToken: mustIssueSetupToken(t, environment.setupTokens), Email: email, Password: password, DisplayName: "Output owner"})); err != nil {
		t.Fatal(err)
	}
	if _, err := authentication.Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: email, Password: password})); err != nil {
		t.Fatal(err)
	}
	csrf := csrfFromJar(jar, environment.serverURL)
	connection, err := connections.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateConnectionRequest{DisplayName: "Output target", Config: environment.targetConfig()}), csrf))
	if err != nil {
		t.Fatal(err)
	}
	connectionID := connection.Msg.Connection.Id
	if _, err := environment.policyClient(jar).Update(ctx, withCSRF(connect.NewRequest(&portcullisv1.UpdateConnectionPolicyRequest{
		ConnectionId: connectionID, ExpectedVersion: 1,
		Read:  &portcullisv1.ClassPolicy{Allowed: true, RequiredApprovals: 0},
		Write: &portcullisv1.ClassPolicy{RequiredApprovals: 1}, Ddl: &portcullisv1.ClassPolicy{RequiredApprovals: 1},
		QueryTimeoutSeconds: 30, MaxRows: 10_000, MaxResultBytes: 16 << 20,
	}), csrf)); err != nil {
		t.Fatal(err)
	}
	requests := environment.requestClient(jar)
	statement := fmt.Sprintf("SELECT 9007199254740992::bigint + sequence AS exact_value, '=formula' AS note, repeat('x', 2048) AS padding FROM generate_series(1, %d) AS sequence ORDER BY sequence", resultOutputRowCount)
	created, err := requests.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateAccessRequestRequest{ConnectionId: connectionID, Title: "Output boundary", Sql: statement}), csrf))
	if err != nil {
		t.Fatal(err)
	}
	requestID := created.Msg.Request.Id
	if _, err := requests.Submit(ctx, withCSRF(connect.NewRequest(&portcullisv1.SubmitAccessRequestRequest{Id: requestID, ExpectedVersion: created.Msg.Request.Version}), csrf)); err != nil {
		t.Fatal(err)
	}
	completion, err := environment.executionClient(jar).Execute(ctx, withCSRF(connect.NewRequest(&portcullisv1.ExecuteQueryRequest{RequestId: requestID}), csrf))
	if err != nil || completion.Msg.GetState() != portcullisv1.AccessRequestState_ACCESS_REQUEST_STATE_SUCCEEDED || !completion.Msg.GetResultAvailable() || completion.Msg.GetRowCount() != resultOutputRowCount {
		t.Fatalf("fixture execution: %+v %v", completion, err)
	}
	fixture := resultOutputFixture{environment: environment, probe: probe, jar: jar, csrf: csrf, requestID: requestID, connectionID: connectionID, actorID: environment.userIDByEmail(t, email)}
	if err := environment.pool.QueryRow(ctx, `select binding.id::text,binding.organization_id::text,version.role_id::text from public.role_bindings binding join public.role_binding_versions version on version.binding_id=binding.id and version.version=binding.current_version where binding.user_id=$1`, fixture.actorID).Scan(&fixture.bindingID, &fixture.organization, &fixture.roleID); err != nil {
		t.Fatal(err)
	}
	if err := environment.pool.QueryRow(ctx, `select result_id::text from public.query_executions where request_id=$1`, requestID).Scan(&fixture.resultID); err != nil {
		t.Fatal(err)
	}
	return fixture
}

// setCondition installs fixture-only condition source without changing the output decision implementation.
func (fixture *resultOutputFixture) setCondition(t *testing.T, expression string) {
	t.Helper()
	if _, err := fixture.environment.pool.Exec(context.Background(), `update public.role_binding_versions set condition_title='Output window',condition_expression=$2,condition_language_version='cel-v1',condition_environment_version='portcullis-v1' where binding_id=$1`, fixture.bindingID, expression); err != nil {
		t.Fatal(err)
	}
}

// prepareScenario establishes the grant shape before any result bytes are requested.
func (fixture *resultOutputFixture) prepareScenario(t *testing.T, scenario string) {
	t.Helper()
	ctx := context.Background()
	switch scenario {
	case "connection grant":
		if _, err := fixture.environment.pool.Exec(ctx, `update public.role_binding_versions set scope_type='connection',connection_id=$2 where binding_id=$1`, fixture.bindingID, fixture.connectionID); err != nil {
			t.Fatal(err)
		}
	case "true condition":
		fixture.setCondition(t, `resource.type == "result" && resource.connection_id == `+strconv.Quote(fixture.connectionID)+` && principal.id == `+strconv.Quote(fixture.actorID))
	case "additive valid grant":
		fixture.setCondition(t, `request.time < timestamp("2000-01-01T00:00:00Z")`)
		if _, err := fixture.environment.pool.Exec(ctx, `with head as(insert into public.role_bindings(organization_id,user_id,current_version,created_at) values($1,$2,1,clock_timestamp()) returning id) insert into public.role_binding_versions(binding_id,organization_id,user_id,version,role_id,scope_type,connection_id,created_at) select head.id,$1,$2,1,$3,'connection',$4,clock_timestamp() from head`, fixture.organization, fixture.actorID, fixture.roleID, fixture.connectionID); err != nil {
			t.Fatal(err)
		}
	case "condition expires":
		if err := fixture.environment.pool.QueryRow(ctx, `select clock_timestamp()+interval '3 seconds'`).Scan(&fixture.expiresAt); err != nil {
			t.Fatal(err)
		}
		fixture.setCondition(t, `request.time < timestamp(`+strconv.Quote(fixture.expiresAt.UTC().Format(time.RFC3339Nano))+`)`)
	}
	fixture.probe.setBoundary(func(ctx context.Context) error { return fixture.changeOutputAuthority(ctx, scenario) })
}

// changeOutputAuthority performs the precise transition after data processing has already succeeded.
func (fixture *resultOutputFixture) changeOutputAuthority(ctx context.Context, scenario string) error {
	var statement string
	var parameters []any
	switch scenario {
	case "binding revoked":
		statement, parameters = `update public.role_binding_versions set revoked_at=clock_timestamp() where binding_id=$1`, []any{fixture.bindingID}
	case "condition expires":
		return fixture.waitForDatabaseExpiry(ctx)
	case "user disabled":
		statement, parameters = `update public.users set status='disabled' where id=$1`, []any{fixture.actorID}
	case "session revoked":
		statement, parameters = `update public.sessions set revoked_at=clock_timestamp() where user_id=$1 and revoked_at is null`, []any{fixture.actorID}
	case "result expires":
		statement, parameters = `update result_cache.result_sets set expires_at=clock_timestamp()-interval '1 second' where id=$1`, []any{fixture.resultID}
	default:
		return nil
	}
	changed, err := fixture.environment.pool.Exec(ctx, statement, parameters...)
	if err != nil {
		return err
	}
	if changed.RowsAffected() != 1 {
		return fmt.Errorf("output boundary changed %d rows, expected one", changed.RowsAffected())
	}
	return nil
}

// waitForDatabaseExpiry observes the server clock rather than assuming that a sleep expired the condition.
func (fixture *resultOutputFixture) waitForDatabaseExpiry(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		var expired bool
		if err := fixture.environment.pool.QueryRow(ctx, `select clock_timestamp()>=$1::timestamptz`, fixture.expiresAt).Scan(&expired); err != nil {
			return err
		}
		if expired {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-poll.C:
		}
	}
}

// assertDurableExecution verifies that a refused output neither replays SQL nor changes its recorded success.
func (fixture *resultOutputFixture) assertDurableExecution(t *testing.T) {
	t.Helper()
	var executions, starts int
	var outcome, state, resultID string
	if err := fixture.environment.pool.QueryRow(context.Background(), `select count(*),min(outcome),min(result_id::text) from public.query_executions where request_id=$1`, fixture.requestID).Scan(&executions, &outcome, &resultID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.environment.pool.QueryRow(context.Background(), `select (select count(*) from public.audit_events where target_id=$1::text and action='EXECUTION_STARTED'),state from public.access_requests where id=$1::uuid`, fixture.requestID).Scan(&starts, &state); err != nil {
		t.Fatal(err)
	}
	if executions != 1 || starts != 1 || outcome != "succeeded" || state != "succeeded" || resultID != fixture.resultID {
		t.Fatalf("output changed durable execution: executions=%d starts=%d outcome=%s state=%s result=%s", executions, starts, outcome, state, resultID)
	}
}

// TestResultOutputRechecksCurrentAuthority qualifies protected unary and CSV output against the real database.
func TestResultOutputRechecksCurrentAuthority(t *testing.T) {
	scenarios := []struct {
		name string
		want connect.Code
	}{
		{"organization grant", 0},
		{"connection grant", 0},
		{"true condition", 0},
		{"additive valid grant", 0},
		{"binding revoked", connect.CodePermissionDenied},
		{"condition expires", connect.CodePermissionDenied},
		{"user disabled", connect.CodePermissionDenied},
		{"session revoked", connect.CodeUnauthenticated},
		{"result expires", connect.CodeFailedPrecondition},
	}
	for _, stage := range []string{"unary", "CSV"} {
		for _, scenario := range scenarios {
			t.Run(stage+"/"+scenario.name, func(t *testing.T) {
				fixture := newResultOutputFixture(t)
				fixture.prepareScenario(t, scenario.name)
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				client := fixture.environment.executionClient(fixture.jar)
				var callError error
				if stage == "unary" {
					page, err := client.GetResult(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetQueryResultRequest{RequestId: fixture.requestID, PageSize: 10}), fixture.csrf))
					callError = err
					if scenario.want == 0 && (page == nil || len(page.Msg.Rows) != 10 || page.Msg.Rows[0].Cells[0].GetIntValue() != "9007199254740993") {
						t.Fatalf("exact result output: %+v %v", page, err)
					}
					if scenario.want != 0 && page != nil {
						t.Fatal("protected unary data escaped after authority loss")
					}
				} else {
					stream, err := client.ExportCSV(ctx, withCSRF(connect.NewRequest(&portcullisv1.ExportQueryCSVRequest{RequestId: fixture.requestID}), fixture.csrf))
					var contents strings.Builder
					if err == nil {
						for stream.Receive() {
							contents.Write(stream.Msg().Data)
						}
						err = stream.Err()
					}
					callError = err
					if scenario.want == 0 {
						records, parseError := csv.NewReader(strings.NewReader(contents.String())).ReadAll()
						if parseError != nil || len(records) != resultOutputRowCount+1 || records[1][0] != "9007199254740993" || records[1][1] != "'=formula" || records[resultOutputRowCount][0] != "9007199254741172" {
							t.Fatalf("complete exact safe CSV: records=%d parse=%v export=%v", len(records), parseError, err)
						}
					} else if fixture.probe.boundaryBytes.Load() <= 64 || fixture.probe.additionalBytes.Load() != 0 || int64(contents.Len()) != fixture.probe.boundaryBytes.Load() {
						t.Fatalf("CSV continued after output boundary: received=%d boundary=%d additional=%d", contents.Len(), fixture.probe.boundaryBytes.Load(), fixture.probe.additionalBytes.Load())
					}
				}
				if boundaryError := fixture.probe.boundaryFailure(); boundaryError != nil {
					t.Fatalf("fixture boundary: %v", boundaryError)
				}
				if fixture.probe.hookCalls.Load() != 1 {
					t.Fatalf("output boundary calls=%d, expected one", fixture.probe.hookCalls.Load())
				}
				if codeOrSuccess(callError) != scenario.want {
					t.Fatalf("output code=%v want=%v error=%v", codeOrSuccess(callError), scenario.want, callError)
				}
				fixture.assertDurableExecution(t)
			})
		}
	}
}
