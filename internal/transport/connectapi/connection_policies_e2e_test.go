package connectapi_test

import (
	"context"
	"slices"
	"testing"

	"connectrpc.com/connect"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
)

func defaultPolicyUpdate(id string, expected int64) *portcullisv1.UpdateConnectionPolicyRequest {
	return &portcullisv1.UpdateConnectionPolicyRequest{
		ConnectionId:        id,
		ExpectedVersion:     expected,
		Read:                &portcullisv1.ClassPolicy{Allowed: true, RequiredApprovals: 1},
		Write:               &portcullisv1.ClassPolicy{Allowed: false, RequiredApprovals: 1},
		Ddl:                 &portcullisv1.ClassPolicy{Allowed: false, RequiredApprovals: 1},
		QueryTimeoutSeconds: 30,
		MaxRows:             10_000,
		MaxResultBytes:      16 << 20,
	}
}

func TestConnectionPoliciesLifecycle(t *testing.T) {
	env := newConnsTestEnv(t)
	ctx := context.Background()

	jar, authC, connsC, auditC := env.clients()
	policiesC := env.policyClient(jar)
	const email, password = "admin@example.com", "correct-horse-battery"
	if _, err := authC.Bootstrap(ctx, connect.NewRequest(&portcullisv1.BootstrapRequest{SetupToken: mustIssueSetupToken(t, env.setupTokens), Email: email, Password: password, DisplayName: "Admin"})); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	login, err := authC.Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: email, Password: password}))
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	csrf := csrfFromJar(jar, env.serverURL)

	if !slices.Contains(login.Msg.GetPermissions(), "policies.update") {
		t.Errorf("Login.permissions = %v, want policies.update present", login.Msg.GetPermissions())
	}
	me, err := authC.Me(ctx, withCSRF(connect.NewRequest(&portcullisv1.MeRequest{}), csrf))
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if !slices.Contains(me.Msg.GetPermissions(), "policies.get") || !slices.Contains(me.Msg.GetPermissions(), "connections.create") {
		t.Errorf("Me.permissions = %v, want the admin catalog", me.Msg.GetPermissions())
	}

	created, err := connsC.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateConnectionRequest{
		DisplayName: "Policied",
		Environment: "production",
		Description: "policy e2e target",
		Config:      env.targetConfig(),
	}), csrf))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := created.Msg.GetConnection().GetId()
	if created.Msg.GetConnection().GetEnvironment() != "production" {
		t.Errorf("summary environment = %q, want production", created.Msg.GetConnection().GetEnvironment())
	}

	got, err := policiesC.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetConnectionPolicyRequest{ConnectionId: id}), csrf))
	if err != nil {
		t.Fatalf("Get policy: %v", err)
	}
	p := got.Msg.GetPolicy()
	if p.GetVersion() != 1 || !p.GetRead().GetAllowed() || p.GetWrite().GetAllowed() || p.GetDdl().GetAllowed() {
		t.Fatalf("v1 policy = %+v, want read-only defaults", p)
	}
	if p.GetQueryTimeoutSeconds() != 30 || p.GetMaxRows() != 10_000 || p.GetMaxResultBytes() != 16<<20 {
		t.Errorf("v1 limits = %d/%d/%d", p.GetQueryTimeoutSeconds(), p.GetMaxRows(), p.GetMaxResultBytes())
	}

	up := defaultPolicyUpdate(id, 1)
	up.Write = &portcullisv1.ClassPolicy{Allowed: true, RequiredApprovals: 2}
	up.Read = &portcullisv1.ClassPolicy{Allowed: true, RequiredApprovals: 0}
	updated, err := policiesC.Update(ctx, withCSRF(connect.NewRequest(up), csrf))
	if err != nil {
		t.Fatalf("Update policy: %v", err)
	}
	if v := updated.Msg.GetPolicy(); v.GetVersion() != 2 || !v.GetWrite().GetAllowed() || v.GetRead().GetRequiredApprovals() != 0 {
		t.Fatalf("v2 policy = %+v", v)
	}

	got, err = policiesC.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetConnectionPolicyRequest{ConnectionId: id}), csrf))
	if err != nil {
		t.Fatal(err)
	}
	if got.Msg.GetPolicy().GetVersion() != 2 {
		t.Errorf("current version = %d, want 2", got.Msg.GetPolicy().GetVersion())
	}

	if _, err := policiesC.Update(ctx, withCSRF(connect.NewRequest(defaultPolicyUpdate(id, 1)), csrf)); connect.CodeOf(err) != connect.CodeAborted {
		t.Errorf("stale update code = %v, want Aborted", connect.CodeOf(err))
	}

	if _, err := policiesC.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetConnectionPolicyRequest{ConnectionId: "not-a-uuid"}), csrf)); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("Get(bad uuid) code = %v, want InvalidArgument", connect.CodeOf(err))
	}

	bad := defaultPolicyUpdate(id, 2)
	bad.QueryTimeoutSeconds = 0
	if _, err := policiesC.Update(ctx, withCSRF(connect.NewRequest(bad), csrf)); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("Update(timeout 0) code = %v, want InvalidArgument", connect.CodeOf(err))
	}
	bad = defaultPolicyUpdate(id, 2)
	bad.Read.RequiredApprovals = 101
	if _, err := policiesC.Update(ctx, withCSRF(connect.NewRequest(bad), csrf)); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("Update(approvals 101) code = %v, want InvalidArgument", connect.CodeOf(err))
	}
	// Reject omitted policy classes so defaulting cannot silently reset an approval quorum.
	bad = defaultPolicyUpdate(id, 2)
	bad.Write = nil
	if _, err := policiesC.Update(ctx, withCSRF(connect.NewRequest(bad), csrf)); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("Update(write omitted) code = %v, want InvalidArgument", connect.CodeOf(err))
	}

	if _, err := connsC.Archive(ctx, withCSRF(connect.NewRequest(&portcullisv1.ArchiveConnectionRequest{Id: id}), csrf)); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if _, err := policiesC.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetConnectionPolicyRequest{ConnectionId: id}), csrf)); err != nil {
		t.Errorf("Get on archived = %v, want the historical snapshot", err)
	}
	if _, err := policiesC.Update(ctx, withCSRF(connect.NewRequest(defaultPolicyUpdate(id, 2)), csrf)); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("Update on archived code = %v, want FailedPrecondition", connect.CodeOf(err))
	}

	events, err := auditC.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.AuditListRequest{PageSize: 100}), csrf))
	if err != nil {
		t.Fatalf("Audit.List: %v", err)
	}
	var sawUpdated, sawEnabled bool
	for _, e := range events.Msg.GetEvents() {
		if e.GetTargetId() != id {
			continue
		}
		switch e.GetAction() {
		case "CONNECTION_POLICY_UPDATED":
			sawUpdated = true
		case "CONNECTION_POLICY_CLASS_ENABLED":
			sawEnabled = true
		}
	}
	if !sawUpdated || !sawEnabled {
		t.Errorf("audit trail: updated=%t enabled=%t, want both", sawUpdated, sawEnabled)
	}
}
