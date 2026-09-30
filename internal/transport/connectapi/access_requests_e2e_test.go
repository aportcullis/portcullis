package connectapi_test

import (
	"context"
	"net/http"
	"testing"

	"connectrpc.com/connect"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
)

func TestAccessRequestRejectsNestedSchemaCommandsE2E(t *testing.T) {
	env, jar, csrf, connID := reqEnv(t, 1)
	ctx := context.Background()
	if _, err := env.policyClient(jar).Update(ctx, withCSRF(connect.NewRequest(&portcullisv1.UpdateConnectionPolicyRequest{
		ConnectionId: connID, ExpectedVersion: 1,
		Read:                &portcullisv1.ClassPolicy{Allowed: true, RequiredApprovals: 1},
		Write:               &portcullisv1.ClassPolicy{Allowed: false, RequiredApprovals: 1},
		Ddl:                 &portcullisv1.ClassPolicy{Allowed: true, RequiredApprovals: 0},
		QueryTimeoutSeconds: 30, MaxRows: 10_000, MaxResultBytes: 16 << 20,
	}), csrf)); err != nil {
		t.Fatalf("enable DDL: %v", err)
	}
	client := env.requestClient(jar)
	for _, sql := range []string{
		"CREATE SCHEMA s CREATE TABLE t (id int) GRANT SELECT ON t TO PUBLIC",
		"CREATE SCHEMA s CREATE TABLE t (id int) CREATE TRIGGER tr BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION public.f()",
		"CREATE SCHEMA s CREATE TABLE t (id int) CREATE INDEX CONCURRENTLY i ON t (id)",
	} {
		created, err := client.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateAccessRequestRequest{ConnectionId: connID, Sql: sql}), csrf))
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		r := created.Msg.GetRequest()
		if _, err := client.Submit(ctx, withCSRF(connect.NewRequest(&portcullisv1.SubmitAccessRequestRequest{Id: r.GetId(), ExpectedVersion: r.GetVersion()}), csrf)); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatalf("Submit(%s) code=%v, want InvalidArgument", sql, connect.CodeOf(err))
		}
		got, err := client.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetAccessRequestRequest{Id: r.GetId()}), csrf))
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Msg.GetRequest().GetState() != portcullisv1.AccessRequestState_ACCESS_REQUEST_STATE_DRAFT || got.Msg.GetRequest().GetVersion() != r.GetVersion() {
			t.Fatalf("refused submit changed the draft: %+v", got.Msg.GetRequest())
		}
	}
	page, err := env.auditClient(jar).List(ctx, withCSRF(connect.NewRequest(&portcullisv1.AuditListRequest{PageSize: 100}), csrf))
	if err != nil {
		t.Fatalf("Audit.List: %v", err)
	}
	for _, event := range page.Msg.GetEvents() {
		if event.GetAction() == "ACCESS_REQUEST_SUBMITTED" || event.GetAction() == "ACCESS_REQUEST_APPROVED" {
			t.Fatalf("refused submits emitted %s", event.GetAction())
		}
	}
}

func reqEnv(t *testing.T, readQuorum int) (*connsTestEnv, http.CookieJar, string, string) {
	t.Helper()
	env := newConnsTestEnv(t)
	ctx := context.Background()
	jar, authC, connsC, _ := env.clients()
	const email, password = "admin@example.com", "correct-horse-battery"
	if _, err := authC.Bootstrap(ctx, connect.NewRequest(&portcullisv1.BootstrapRequest{Email: email, Password: password, DisplayName: "Admin"})); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if _, err := authC.Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: email, Password: password})); err != nil {
		t.Fatalf("Login: %v", err)
	}
	csrf := csrfFromJar(jar, env.serverURL)

	created, err := connsC.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateConnectionRequest{
		DisplayName: "Target", Config: env.targetConfig(),
	}), csrf))
	if err != nil {
		t.Fatalf("Create connection: %v", err)
	}
	id := created.Msg.GetConnection().GetId()

	if readQuorum != 1 {
		policiesC := env.policyClient(jar)
		if _, err := policiesC.Update(ctx, withCSRF(connect.NewRequest(&portcullisv1.UpdateConnectionPolicyRequest{
			ConnectionId:        id,
			ExpectedVersion:     1,
			Read:                &portcullisv1.ClassPolicy{Allowed: true, RequiredApprovals: uint32(readQuorum)}, //nolint:gosec // test input
			Write:               &portcullisv1.ClassPolicy{Allowed: false, RequiredApprovals: 1},
			Ddl:                 &portcullisv1.ClassPolicy{Allowed: false, RequiredApprovals: 1},
			QueryTimeoutSeconds: 30, MaxRows: 10_000, MaxResultBytes: 16 << 20,
		}), csrf)); err != nil {
			t.Fatalf("policy update: %v", err)
		}
	}
	return env, jar, csrf, id
}

func TestAccessRequestAutoApprovalE2E(t *testing.T) {
	env, jar, csrf, connID := reqEnv(t, 0)
	ctx := context.Background()
	requestsC := env.requestClient(jar)
	auditC := env.auditClient(jar)

	created, err := requestsC.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateAccessRequestRequest{
		ConnectionId: connID,
		Sql:          "select id from t where n = :n",
		Params:       []*portcullisv1.TypedParam{{Name: "n", Type: "integer", Value: "7"}},
	}), csrf))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	r := created.Msg.GetRequest()
	if r.GetState() != portcullisv1.AccessRequestState_ACCESS_REQUEST_STATE_DRAFT {
		t.Fatalf("state = %v, want DRAFT", r.GetState())
	}

	if r.GetRedactedSql() != "" {
		t.Errorf("draft redacted_sql should be empty, got %q", r.GetRedactedSql())
	}

	submitted, err := requestsC.Submit(ctx, withCSRF(connect.NewRequest(&portcullisv1.SubmitAccessRequestRequest{
		Id: r.GetId(), ExpectedVersion: r.GetVersion(),
	}), csrf))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	sr := submitted.Msg.GetRequest()
	if sr.GetState() != portcullisv1.AccessRequestState_ACCESS_REQUEST_STATE_APPROVED {
		t.Errorf("auto-approve state = %v, want APPROVED", sr.GetState())
	}
	if sr.GetExpiresAt() == nil || sr.GetStatementClass() != "read" || sr.GetPolicyVersion() == 0 {
		t.Errorf("snapshot not pinned: %+v", sr)
	}

	got, err := requestsC.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetAccessRequestRequest{Id: r.GetId()}), csrf))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Msg.GetPayload() == nil || got.Msg.GetPayload().GetSql() == "" || len(got.Msg.GetPayload().GetParams()) != 1 {
		t.Errorf("owner payload = %+v", got.Msg.GetPayload())
	}

	page, err := auditC.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.AuditListRequest{PageSize: 100}), csrf))
	if err != nil {
		t.Fatalf("audit List: %v", err)
	}
	var sawSubmit, sawSystemApprove bool
	for _, e := range page.Msg.GetEvents() {
		switch e.GetAction() {
		case "ACCESS_REQUEST_SUBMITTED":
			sawSubmit = true
		case "ACCESS_REQUEST_APPROVED":
			if e.GetActorType() == "system" {
				sawSystemApprove = true
			}
		}
	}
	if !sawSubmit || !sawSystemApprove {
		t.Errorf("audit missing events: submit=%t systemApprove=%t", sawSubmit, sawSystemApprove)
	}
}

func TestAccessRequestQuorumE2E(t *testing.T) {
	env, jar, csrf, connID := reqEnv(t, 1)
	ctx := context.Background()
	adminC := env.requestClient(jar)

	created, err := adminC.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateAccessRequestRequest{
		ConnectionId: connID, Sql: "select 1",
	}), csrf))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rid := created.Msg.GetRequest().GetId()
	if _, err := adminC.Submit(ctx, withCSRF(connect.NewRequest(&portcullisv1.SubmitAccessRequestRequest{
		Id: rid, ExpectedVersion: created.Msg.GetRequest().GetVersion(),
	}), csrf)); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// The requester cannot approve their own request.
	if _, err := adminC.Approve(ctx, withCSRF(connect.NewRequest(&portcullisv1.ApproveAccessRequestRequest{Id: rid}), csrf)); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("self-approval code = %v, want FailedPrecondition", connect.CodeOf(err))
	}

	env.seedUser(t, "approver@example.com", "approver")
	approverC, approverCSRF := env.loginAs(t, "approver@example.com", "approver-password-1")
	approved, err := approverC.Approve(ctx, withCSRF(connect.NewRequest(&portcullisv1.ApproveAccessRequestRequest{Id: rid, Reason: "ok"}), approverCSRF))
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if approved.Msg.GetRequest().GetState() != portcullisv1.AccessRequestState_ACCESS_REQUEST_STATE_APPROVED {
		t.Errorf("state after approve = %v, want APPROVED", approved.Msg.GetRequest().GetState())
	}
}

func TestAccessRequestVisibilityE2E(t *testing.T) {
	env, jar, csrf, connID := reqEnv(t, 1)
	ctx := context.Background()
	adminC := env.requestClient(jar)

	created, err := adminC.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateAccessRequestRequest{
		ConnectionId: connID, Sql: "select secret",
	}), csrf))
	if err != nil {
		t.Fatalf("admin Create: %v", err)
	}
	adminReq := created.Msg.GetRequest().GetId()

	env.seedUser(t, "req@example.com", "requester")
	reqC, reqCSRF := env.loginAs(t, "req@example.com", "requester-password-1")
	page, err := reqC.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListAccessRequestsRequest{PageSize: 50}), reqCSRF))
	if err != nil {
		t.Fatalf("requester List: %v", err)
	}
	if page.Msg.GetTotalCount() != 0 {
		t.Errorf("requester sees %d requests, want 0 (own-only scope)", page.Msg.GetTotalCount())
	}
	// And cannot fetch the admin's request at all.
	if _, err := reqC.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetAccessRequestRequest{Id: adminReq}), reqCSRF)); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("foreign Get code = %v, want NotFound", connect.CodeOf(err))
	}
}

func TestAccessRequestRejectOnlyRoleE2E(t *testing.T) {
	env, jar, csrf, connID := reqEnv(t, 1)
	ctx := context.Background()
	adminC := env.requestClient(jar)

	created, err := adminC.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateAccessRequestRequest{
		ConnectionId: connID, Sql: "select 1",
	}), csrf))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rid := created.Msg.GetRequest().GetId()
	if _, err := adminC.Submit(ctx, withCSRF(connect.NewRequest(&portcullisv1.SubmitAccessRequestRequest{
		Id: rid, ExpectedVersion: created.Msg.GetRequest().GetVersion(),
	}), csrf)); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	env.seedUserWithCustomRole(t, "rejector@example.com", "rejector", "requests.list", "requests.get", "requests.reject")
	rejC, rejCSRF := env.loginAs(t, "rejector@example.com", "rejector-password-1")

	page, err := rejC.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListAccessRequestsRequest{PageSize: 50}), rejCSRF))
	if err != nil {
		t.Fatalf("rejector List: %v", err)
	}
	if page.Msg.GetTotalCount() != 1 {
		t.Errorf("reject-only reviewer sees %d requests, want 1 (org-wide)", page.Msg.GetTotalCount())
	}
	got, err := rejC.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetAccessRequestRequest{Id: rid}), rejCSRF))
	if err != nil || got.Msg.GetPayload() == nil {
		t.Errorf("reject-only reviewer Get payload = %+v, %v", got.Msg.GetPayload(), err)
	}

	// Cannot approve (lacks requests.approve) but can reject.
	if _, err := rejC.Approve(ctx, withCSRF(connect.NewRequest(&portcullisv1.ApproveAccessRequestRequest{Id: rid}), rejCSRF)); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("reject-only approve code = %v, want PermissionDenied", connect.CodeOf(err))
	}
	rejected, err := rejC.Reject(ctx, withCSRF(connect.NewRequest(&portcullisv1.RejectAccessRequestRequest{Id: rid, Reason: "not allowed"}), rejCSRF))
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if rejected.Msg.GetRequest().GetState() != portcullisv1.AccessRequestState_ACCESS_REQUEST_STATE_REJECTED {
		t.Errorf("state after reject = %v, want REJECTED", rejected.Msg.GetRequest().GetState())
	}
}

func TestRequestableConnectionsForPlainRequesterE2E(t *testing.T) {
	env, jar, csrf, connID := reqEnv(t, 1)
	ctx := context.Background()
	connsC := env.connectionClient(jar)

	// An archived connection must never be offered as a target.
	archived, err := connsC.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateConnectionRequest{
		DisplayName: "ArchivedTarget", Config: env.targetConfig(),
	}), csrf))
	if err != nil {
		t.Fatalf("create second connection: %v", err)
	}
	if _, err := connsC.Archive(ctx, withCSRF(connect.NewRequest(&portcullisv1.ArchiveConnectionRequest{
		Id: archived.Msg.GetConnection().GetId(),
	}), csrf)); err != nil {
		t.Fatalf("archive: %v", err)
	}
	env.seedUser(t, "plain@example.com", "requester")
	reqC, reqCSRF := env.loginAs(t, "plain@example.com", "requester-password-1")

	got, err := reqC.ListRequestableConnections(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListRequestableConnectionsRequest{}), reqCSRF))
	if err != nil {
		t.Fatalf("requester ListRequestableConnections: %v", err)
	}
	ids := make([]string, 0, len(got.Msg.GetConnections()))
	for _, c := range got.Msg.GetConnections() {
		ids = append(ids, c.GetId())
		if c.GetDisplayName() == "" || c.GetDbType() == "" || c.GetEnvironment() == "" {
			t.Errorf("requestable connection missing a form field: %+v", c)
		}
	}
	if len(ids) != 1 || ids[0] != connID {
		t.Errorf("requestable ids = %v, want only the active %s", ids, connID)
	}
}
