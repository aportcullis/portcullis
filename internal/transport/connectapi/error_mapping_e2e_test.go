package connectapi_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"connectrpc.com/connect"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
	"github.com/aportcullis/portcullis/internal/app/accessrequest"
	"github.com/aportcullis/portcullis/internal/app/auth"
	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	"github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/aportcullis/portcullis/internal/transport/connectapi"
)

// Inject application errors to verify deterministic Connect mappings without scheduling real races.

type policyConflictApp struct{}

func (policyConflictApp) Create(context.Context, identity.UserID, accessrequest.CreateParams) (access.RequestView, error) {
	return access.RequestView{}, nil
}

func (policyConflictApp) UpdateDraft(context.Context, identity.UserID, access.RequestID, accessrequest.UpdateDraftParams) (access.RequestView, error) {
	return access.RequestView{}, nil
}

func (policyConflictApp) Submit(context.Context, identity.UserID, access.RequestID, int64) (access.RequestView, error) {
	return access.RequestView{}, connection.ErrPolicyConflict
}

func (policyConflictApp) Cancel(context.Context, identity.UserID, access.RequestID) (access.RequestView, error) {
	return access.RequestView{}, nil
}

func (policyConflictApp) Approve(context.Context, identity.UserID, access.RequestID, string) (access.RequestView, error) {
	return access.RequestView{}, nil
}

func (policyConflictApp) Reject(context.Context, identity.UserID, access.RequestID, string) (access.RequestView, error) {
	return access.RequestView{}, nil
}

func (policyConflictApp) Get(context.Context, identity.UserID, bool, access.RequestID) (access.RequestView, error) {
	return access.RequestView{}, nil
}

func (policyConflictApp) List(context.Context, identity.UserID, bool, access.ListQuery) (access.RequestPage, error) {
	return access.RequestPage{}, nil
}

func (policyConflictApp) ListRequestableConnections(context.Context) ([]access.RequestableConnection, error) {
	return nil, nil
}

func TestSubmitPolicyConflictMapsToAborted(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.FreshPostgres(t)
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := postgres.NewIdentityStore(pool)
	weak := crypto.Argon2Params{Memory: 8 * 1024, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 16}
	keyring := loadTestKeyring(t)
	authSvc, err := auth.New(store, crypto.NewArgon2Hasher(weak, 4), crypto.NewCSRFProtector(keyring), postgres.NewAuditStore(pool), auth.Config{})
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	catalog, err := authz.LoadCatalog(ctx, store)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	authzSvc, err := authz.New(store, catalog)
	if err != nil {
		t.Fatalf("authz.New: %v", err)
	}

	chain := connect.WithInterceptors(
		connectapi.NewClientIPInterceptor(nil),
		connectapi.NewAuthInterceptor(authSvc),
	)
	mux := http.NewServeMux()
	authPath, authHandler := portcullisv1connect.NewAuthHandler(connectapi.NewAuthService(authSvc, authzSvc), chain)
	reqPath, reqHandler := portcullisv1connect.NewAccessRequestsHandler(connectapi.NewAccessRequestsService(authzSvc, policyConflictApp{}), chain)
	mux.Handle(authPath, authHandler)
	mux.Handle(reqPath, reqHandler)
	ts := httptest.NewTLSServer(mux)
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Transport: ts.Client().Transport, Jar: jar}
	authC := portcullisv1connect.NewAuthClient(hc, ts.URL)
	const email, password = "admin@example.com", "correct-horse-battery"
	if _, err := authC.Bootstrap(ctx, connect.NewRequest(&portcullisv1.BootstrapRequest{Email: email, Password: password, DisplayName: "Admin"})); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if _, err := authC.Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: email, Password: password})); err != nil {
		t.Fatalf("Login: %v", err)
	}
	serverURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	csrf := csrfFromJar(jar, serverURL)

	reqC := portcullisv1connect.NewAccessRequestsClient(hc, ts.URL)
	_, err = reqC.Submit(ctx, withCSRF(connect.NewRequest(&portcullisv1.SubmitAccessRequestRequest{
		Id: "11111111-2222-3333-4444-555555555555", ExpectedVersion: 1,
	}), csrf))
	if code := connect.CodeOf(err); code != connect.CodeAborted {
		t.Errorf("policy conflict surfaced as %v (%v), want Aborted — a retryable business outcome, not a server fault", code, err)
	}
}

func TestApprovalReasonLimitContract(t *testing.T) {
	env, jar, csrf, connID := reqEnv(t, 1)
	ctx := context.Background()
	adminC := env.requestClient(jar)
	limit := access.MaxApprovalReasonChars

	cfg, err := env.authClient(jar).GetConfig(ctx, connect.NewRequest(&portcullisv1.GetConfigRequest{}))
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if got := cfg.Msg.GetMaxApprovalReasonChars(); got != int32(limit) {
		t.Errorf("GetConfig max_approval_reason_chars = %d, want %d", got, limit)
	}

	// One submitted request per case: a decision is terminal, so cases that are EXPECTED to succeed cannot share a request.
	submit := func(t *testing.T) string {
		t.Helper()
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
		return rid
	}

	env.seedUser(t, "approver@example.com", "approver")
	approverC, approverCSRF := env.loginAs(t, "approver@example.com", "approver-password-1")

	for _, tc := range []struct {
		name   string
		reason string
		reject bool
		accept bool
		want   connect.Code
	}{
		{name: "approve one over the cap", reason: strings.Repeat("r", limit+1), want: connect.CodeInvalidArgument},
		{name: "reject one over the cap", reason: strings.Repeat("r", limit+1), reject: true, want: connect.CodeInvalidArgument},
		// A rune is not a byte: 1000 Hangul syllables are 3000 bytes and must be ACCEPTED, while 1001 of them must be refused. A byte-based cap would get both of these wrong.
		{name: "approve at the cap in multi-byte runes", reason: strings.Repeat("가", limit), accept: true},
		{name: "reject one over the cap in multi-byte runes", reason: strings.Repeat("가", limit+1), reject: true, want: connect.CodeInvalidArgument},
		{name: "approve exactly at the cap", reason: strings.Repeat("r", limit), accept: true},
		{name: "approve with no reason at all", reason: "", accept: true},
		// A reasonless rejection is a different user error with its own code path; it must also read as InvalidArgument, not Internal.
		{name: "reject with a whitespace-only reason", reason: "   ", reject: true, want: connect.CodeInvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rid := submit(t)
			var err error
			if tc.reject {
				_, err = approverC.Reject(ctx, withCSRF(connect.NewRequest(&portcullisv1.RejectAccessRequestRequest{
					Id: rid, Reason: tc.reason,
				}), approverCSRF))
			} else {
				_, err = approverC.Approve(ctx, withCSRF(connect.NewRequest(&portcullisv1.ApproveAccessRequestRequest{
					Id: rid, Reason: tc.reason,
				}), approverCSRF))
			}
			if tc.accept {
				if err != nil {
					t.Errorf("decision with a %d-rune reason = %v (%v), want accepted", len([]rune(tc.reason)), connect.CodeOf(err), err)
				}
				return
			}
			if code := connect.CodeOf(err); code != tc.want {
				t.Errorf("decision with a %d-rune reason = %v (%v), want %v", len([]rune(tc.reason)), code, err, tc.want)
			}
		})
	}
}

func TestMaximumPayloadTravelsThroughTheJSONAPI(t *testing.T) {
	env, jar, csrf, connID := reqEnv(t, 1)
	ctx := context.Background()
	jsonC := env.requestClientJSON(jar)

	const paramName, paramValue = "id", "seven"
	overhead := len(paramName) + len(paramValue)
	plain := "select " + strings.Repeat("x", access.MaxPayloadBytes-len("select ")-overhead)
	params := []*portcullisv1.TypedParam{{Name: paramName, Type: "string", Value: paramValue}}

	if _, err := jsonC.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateAccessRequestRequest{
		ConnectionId: connID, Sql: plain, Params: params,
	}), csrf)); err != nil {
		t.Fatalf("a plain payload at the budget was refused over JSON: %v", err)
	}

	if _, err := jsonC.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateAccessRequestRequest{
		ConnectionId: connID, Sql: plain + "x", Params: params,
	}), csrf)); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("one byte over the budget over JSON = %v (%v), want InvalidArgument", connect.CodeOf(err), err)
	}

	// The documented limit of the JSON wire: a payload WITHIN the domain budget but built from characters JSON must escape does not fit the request cap, so the transport refuses it first. The user-visible outcome is a size refusal (ResourceExhausted), never a silent truncation or a 500.
	quoted := `select '` + strings.Repeat(`"`, access.MaxPayloadBytes-len("select ''")) + `'`
	if _, err := jsonC.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateAccessRequestRequest{
		ConnectionId: connID, Sql: quoted,
	}), csrf)); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Errorf("an all-quotes payload at the budget over JSON = %v (%v), want ResourceExhausted from the request cap",
			connect.CodeOf(err), err)
	}
}

func TestMaximumPayloadTravelsThroughTheAPI(t *testing.T) {
	env, jar, csrf, connID := reqEnv(t, 1)
	ctx := context.Background()
	requestsC := env.requestClient(jar)

	const paramName, paramValue = "id", "seven"
	overhead := len(paramName) + len(paramValue)
	sql := "select " + strings.Repeat("x", access.MaxPayloadBytes-len("select ")-overhead)
	params := []*portcullisv1.TypedParam{{Name: paramName, Type: "string", Value: paramValue}}

	created, err := requestsC.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateAccessRequestRequest{
		ConnectionId: connID, Sql: sql, Params: params,
	}), csrf))
	if err != nil {
		t.Fatalf("a payload at the domain budget was refused end to end: %v", err)
	}
	if created.Msg.GetRequest().GetId() == "" {
		t.Error("Create returned no request for a payload at the budget")
	}

	_, err = requestsC.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateAccessRequestRequest{
		ConnectionId: connID, Sql: sql + "x", Params: params,
	}), csrf))
	if code := connect.CodeOf(err); code != connect.CodeInvalidArgument {
		t.Errorf("one byte over the budget = %v (%v), want InvalidArgument from the payload validator", code, err)
	}
}

func TestArchiveWithExecutionInFlightMapsToFailedPrecondition(t *testing.T) {
	env, jar, csrf, connID := reqEnv(t, 1)
	ctx := context.Background()
	requestsC := env.requestClient(jar)

	created, err := requestsC.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateAccessRequestRequest{
		ConnectionId: connID, Sql: "select 1",
	}), csrf))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	submitted, err := requestsC.Submit(ctx, withCSRF(connect.NewRequest(&portcullisv1.SubmitAccessRequestRequest{
		Id: created.Msg.GetRequest().GetId(), ExpectedVersion: created.Msg.GetRequest().GetVersion(),
	}), csrf))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := env.pool.Exec(ctx,
		`update access_requests set state = 'executing' where id = $1`,
		submitted.Msg.GetRequest().GetId()); err != nil {
		t.Fatalf("force executing: %v", err)
	}

	_, err = env.connectionClient(jar).Archive(ctx, withCSRF(connect.NewRequest(&portcullisv1.ArchiveConnectionRequest{
		Id: connID,
	}), csrf))
	if code := connect.CodeOf(err); code != connect.CodeFailedPrecondition {
		t.Errorf("archive with an execution in flight surfaced as %v (%v), want FailedPrecondition", code, err)
	}
}
