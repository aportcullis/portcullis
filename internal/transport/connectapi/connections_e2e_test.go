package connectapi_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
	auditapp "github.com/aportcullis/portcullis/internal/app/audit"
	"github.com/aportcullis/portcullis/internal/app/auth"
	"github.com/aportcullis/portcullis/internal/app/authz"
	connapp "github.com/aportcullis/portcullis/internal/app/connection"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	"github.com/aportcullis/portcullis/internal/infra/pgdialect"
	"github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/aportcullis/portcullis/internal/transport/connectapi"
)

// connsTestEnv mounts Auth + Connections + Audit behind the interceptor chain
// on one TLS server. The FRESH test database doubles as the target the tester
// dials, so the create→test→archive flow runs against a real PostgreSQL.
type connsTestEnv struct {
	pool      *pgxpool.Pool
	store     *postgres.IdentityStore
	hasher    auth.PasswordHasher
	serverURL *url.URL
	tsURL     string
	transport http.RoundTripper
}

func newConnsTestEnv(t *testing.T) *connsTestEnv {
	t.Helper()
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	store := postgres.NewIdentityStore(pool)
	weak := crypto.Argon2Params{Memory: 8 * 1024, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 16}
	hasher := crypto.NewArgon2Hasher(weak, 4)
	keyring := loadTestKeyring(t)
	authSvc, err := auth.New(store, hasher, crypto.NewCSRFProtector(keyring), postgres.NewAuditStore(pool), auth.Config{})
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
	auditReader, err := auditapp.New(postgres.NewAuditStore(pool))
	if err != nil {
		t.Fatalf("auditapp.New: %v", err)
	}
	connSvc, err := connapp.New(
		postgres.NewConnectionStore(pool),
		pgdialect.NewTester(10*time.Second),
		crypto.NewConnectionCredentialCodec(keyring),
		postgres.NewAuditStore(pool),
	)
	if err != nil {
		t.Fatalf("connapp.New: %v", err)
	}

	chain := connect.WithInterceptors(
		connectapi.NewClientIPInterceptor(nil),
		connectapi.NewAuthInterceptor(authSvc),
	)
	mux := http.NewServeMux()
	authPath, authHandler := portcullisv1connect.NewAuthHandler(connectapi.NewAuthService(authSvc), chain)
	auditPath, auditHandler := portcullisv1connect.NewAuditHandler(connectapi.NewAuditService(authzSvc, auditReader), chain)
	connsPath, connsHandler := portcullisv1connect.NewConnectionsHandler(connectapi.NewConnectionsService(authzSvc, connSvc), chain)
	mux.Handle(authPath, authHandler)
	mux.Handle(auditPath, auditHandler)
	mux.Handle(connsPath, connsHandler)
	ts := httptest.NewTLSServer(mux)
	t.Cleanup(ts.Close)

	serverURL, _ := url.Parse(ts.URL)
	return &connsTestEnv{pool: pool, store: store, hasher: hasher, serverURL: serverURL, tsURL: ts.URL, transport: ts.Client().Transport}
}

func (e *connsTestEnv) clients() (http.CookieJar, portcullisv1connect.AuthClient, portcullisv1connect.ConnectionsClient, portcullisv1connect.AuditClient) {
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Transport: e.transport, Jar: jar}
	return jar, portcullisv1connect.NewAuthClient(hc, e.tsURL),
		portcullisv1connect.NewConnectionsClient(hc, e.tsURL),
		portcullisv1connect.NewAuditClient(hc, e.tsURL)
}

// targetConfig returns the fresh test database's own coordinates as the
// connection config under test.
func (e *connsTestEnv) targetConfig() *portcullisv1.ConnectionConfigInput {
	cc := e.pool.Config().ConnConfig
	return &portcullisv1.ConnectionConfigInput{
		Host:     cc.Host,
		Port:     uint32(cc.Port),
		Database: cc.Database,
		User:     cc.User,
		Password: cc.Password,
		TlsMode:  "disable", // the test container has no TLS — the relaxed path
	}
}

func withCSRF[T any](req *connect.Request[T], csrf string) *connect.Request[T] {
	req.Header().Set("X-CSRF-Token", csrf)
	return req
}

// TestConnectionsLifecycle drives bootstrap → create (test-before-save) →
// list/get → test → rename → archive against a real PostgreSQL target, and
// verifies the audit trail and the credential-free read model along the way.
func TestConnectionsLifecycle(t *testing.T) {
	env := newConnsTestEnv(t)
	ctx := context.Background()

	jar, authC, connsC, auditC := env.clients()
	const email, password = "admin@example.com", "correct-horse-battery"
	if _, err := authC.Bootstrap(ctx, connect.NewRequest(&portcullisv1.BootstrapRequest{Email: email, Password: password, DisplayName: "Admin"})); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if _, err := authC.Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: email, Password: password})); err != nil {
		t.Fatalf("Login: %v", err)
	}
	csrf := csrfFromJar(jar, env.serverURL)

	// A malformed connection id is a client error (InvalidArgument), not a
	// storage failure surfaced as Internal (external review) — checked with an
	// admin who holds connections.get, so it can't be masked by a permission
	// denial.
	if _, err := connsC.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetConnectionRequest{Id: "not-a-uuid"}), csrf)); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("Get(bad uuid) code = %v, want InvalidArgument", connect.CodeOf(err))
	}

	// Create with a wrong password: the mandatory pre-save test refuses to save.
	badCfg := env.targetConfig()
	badCfg.Password = "definitely-wrong"
	_, err := connsC.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateConnectionRequest{
		DisplayName: "Broken", Config: badCfg,
	}), csrf))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("create with failing test code = %v, want FailedPrecondition", connect.CodeOf(err))
	}
	if err != nil && (strings.Contains(err.Error(), "definitely-wrong") || strings.Contains(err.Error(), badCfg.GetUser())) {
		t.Fatalf("error leaks credential material: %v", err)
	}

	// Create succeeds against the live target.
	created, err := connsC.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateConnectionRequest{
		DisplayName: "Primary", Config: env.targetConfig(),
	}), csrf))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	conn := created.Msg.GetConnection()
	if conn.GetId() == "" || conn.GetDbType() != "postgresql" || conn.GetArchivedAt() != nil {
		t.Fatalf("created connection incomplete: %+v", conn)
	}
	// Creation returns a list-safe summary. Only connections.get can expose the
	// descriptor's inner target and TLS fields.
	detail, err := connsC.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetConnectionRequest{Id: conn.GetId()}), csrf))
	if err != nil || detail.Msg.GetConnection().GetTargetFingerprint() == "" || detail.Msg.GetConnection().GetTlsMode() != "disable" {
		t.Fatalf("Get after Create = %+v, %v", detail.Msg.GetConnection(), err)
	}

	// Duplicate active display name is refused.
	if _, err := connsC.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateConnectionRequest{
		DisplayName: "Primary", Config: env.targetConfig(),
	}), csrf)); connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Errorf("duplicate name code = %v, want AlreadyExists", connect.CodeOf(err))
	}

	// Test by id uses the STORED credential (decrypt → dial).
	testResp, err := connsC.Test(ctx, withCSRF(connect.NewRequest(&portcullisv1.TestConnectionRequest{
		Target: &portcullisv1.TestConnectionRequest_Id{Id: conn.GetId()},
	}), csrf))
	if err != nil || !testResp.Msg.GetOk() {
		t.Fatalf("Test by id = (%v, %v), want ok", testResp.Msg.GetOk(), err)
	}
	// Test by config with a wrong password reports the bucket in-band.
	testResp, err = connsC.Test(ctx, withCSRF(connect.NewRequest(&portcullisv1.TestConnectionRequest{
		Target: &portcullisv1.TestConnectionRequest_Config{Config: badCfg},
	}), csrf))
	if err != nil {
		t.Fatalf("Test by config: %v", err)
	}
	if testResp.Msg.GetOk() || testResp.Msg.GetMessage() != "auth-failed" {
		t.Errorf("failed test = (%v, %q), want (false, auth-failed)", testResp.Msg.GetOk(), testResp.Msg.GetMessage())
	}

	// A NON-CANONICAL spelling of the id (uppercase) reaches the same row —
	// PostgreSQL compares uuid values — and must behave exactly like the
	// canonical one: the credential resealed by this update has to open for a
	// later canonical-id test, i.e. the AAD must bind to the canonical id, not
	// the caller's spelling (external review).
	upper := strings.ToUpper(conn.GetId())
	if _, err := connsC.Update(ctx, withCSRF(connect.NewRequest(&portcullisv1.UpdateConnectionRequest{
		Id: upper, Config: env.targetConfig(),
	}), csrf)); err != nil {
		t.Fatalf("Update with uppercase id: %v", err)
	}
	testResp, err = connsC.Test(ctx, withCSRF(connect.NewRequest(&portcullisv1.TestConnectionRequest{
		Target: &portcullisv1.TestConnectionRequest_Id{Id: conn.GetId()},
	}), csrf))
	if err != nil || !testResp.Msg.GetOk() {
		t.Fatalf("Test by canonical id after uppercase-id update = (%v, %v), want ok — credential resealed under a non-canonical AAD", testResp.Msg.GetOk(), err)
	}

	// List and Get return the descriptor only.
	list, err := connsC.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListConnectionsRequest{}), csrf))
	if err != nil || len(list.Msg.GetConnections()) != 1 {
		t.Fatalf("List = %d conns, %v; want 1", len(list.Msg.GetConnections()), err)
	}
	got, err := connsC.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetConnectionRequest{Id: conn.GetId()}), csrf))
	if err != nil || got.Msg.GetConnection().GetDisplayName() != "Primary" {
		t.Fatalf("Get = %+v, %v", got.Msg.GetConnection(), err)
	}

	// Rename-only update (no config, no re-test).
	renamed, err := connsC.Update(ctx, withCSRF(connect.NewRequest(&portcullisv1.UpdateConnectionRequest{
		Id: conn.GetId(), DisplayName: "Primary (renamed)",
	}), csrf))
	if err != nil || renamed.Msg.GetConnection().GetDisplayName() != "Primary (renamed)" {
		t.Fatalf("Update rename = %+v, %v", renamed.Msg.GetConnection(), err)
	}

	// Archive blocks further tests and repeats.
	archived, err := connsC.Archive(ctx, withCSRF(connect.NewRequest(&portcullisv1.ArchiveConnectionRequest{Id: conn.GetId()}), csrf))
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if archived.Msg.GetConnection().GetArchivedAt() == nil {
		t.Fatal("archived connection has no archived_at")
	}
	if _, err := connsC.Archive(ctx, withCSRF(connect.NewRequest(&portcullisv1.ArchiveConnectionRequest{Id: conn.GetId()}), csrf)); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("second archive code = %v, want FailedPrecondition", connect.CodeOf(err))
	}
	if _, err := connsC.Test(ctx, withCSRF(connect.NewRequest(&portcullisv1.TestConnectionRequest{
		Target: &portcullisv1.TestConnectionRequest_Id{Id: conn.GetId()},
	}), csrf)); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("test after archive code = %v, want FailedPrecondition", connect.CodeOf(err))
	}
	// The archived row disappears from the default list and returns with the flag.
	list, err = connsC.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListConnectionsRequest{}), csrf))
	if err != nil || len(list.Msg.GetConnections()) != 0 {
		t.Fatalf("List(active) after archive = %d, %v; want 0", len(list.Msg.GetConnections()), err)
	}
	list, err = connsC.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListConnectionsRequest{IncludeArchived: true}), csrf))
	if err != nil || len(list.Msg.GetConnections()) != 1 {
		t.Fatalf("List(all) after archive = %d, %v; want 1", len(list.Msg.GetConnections()), err)
	}

	// The audit trail carries the whole story (ADR-0014 vocabulary). disable is
	// a relaxed mode, so the create also emitted CONNECTION_TLS_RELAXED.
	trail, err := auditC.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.AuditListRequest{Page: 1, PageSize: 100}), csrf))
	if err != nil {
		t.Fatalf("Audit.List: %v", err)
	}
	wantActions := map[string]bool{
		"CONNECTION_CREATED": false, "CONNECTION_TLS_RELAXED": false,
		"CONNECTION_UPDATED": false, "CONNECTION_ARCHIVED": false, "CONNECTION_TEST": false,
	}
	for _, e := range trail.Msg.GetEvents() {
		if _, ok := wantActions[e.GetAction()]; ok {
			wantActions[e.GetAction()] = true
			if e.GetTargetType() != "connection" {
				t.Errorf("%s target_type = %q, want connection", e.GetAction(), e.GetTargetType())
			}
		}
	}
	for action, seen := range wantActions {
		if !seen {
			t.Errorf("audit trail is missing %s", action)
		}
	}
}

// TestConnectionsPermissionDenied verifies every Connections RPC is gated: an
// authenticated user with no role gets the uniform generic denial (ADR-0008).
func TestConnectionsPermissionDenied(t *testing.T) {
	env := newConnsTestEnv(t)
	ctx := context.Background()

	// Bootstrap the install, then log in as a role-less viewer.
	jar, authC, _, _ := env.clients()
	if _, err := authC.Bootstrap(ctx, connect.NewRequest(&portcullisv1.BootstrapRequest{Email: "admin@example.com", Password: "correct-horse-battery", DisplayName: "Admin"})); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	_ = jar
	u, err := env.store.CreateUser(ctx, "viewer@example.com", "Viewer")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	phc, err := env.hasher.Hash(ctx, "correct-horse-battery")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if err := env.store.SetPassword(ctx, u.ID, phc); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	viewerJar, viewerAuth, viewerConns, _ := env.clients()
	if _, err := viewerAuth.Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: "viewer@example.com", Password: "correct-horse-battery"})); err != nil {
		t.Fatalf("viewer Login: %v", err)
	}
	csrf := csrfFromJar(viewerJar, env.serverURL)

	calls := map[string]func() error{
		"List": func() error {
			_, err := viewerConns.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListConnectionsRequest{}), csrf))
			return err
		},
		"Get": func() error {
			_, err := viewerConns.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetConnectionRequest{Id: "x"}), csrf))
			return err
		},
		"Create": func() error {
			_, err := viewerConns.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateConnectionRequest{DisplayName: "n", Config: env.targetConfig()}), csrf))
			return err
		},
		"Update": func() error {
			_, err := viewerConns.Update(ctx, withCSRF(connect.NewRequest(&portcullisv1.UpdateConnectionRequest{Id: "x", DisplayName: "n"}), csrf))
			return err
		},
		"Test": func() error {
			_, err := viewerConns.Test(ctx, withCSRF(connect.NewRequest(&portcullisv1.TestConnectionRequest{
				Target: &portcullisv1.TestConnectionRequest_Config{Config: env.targetConfig()},
			}), csrf))
			return err
		},
		"Archive": func() error {
			_, err := viewerConns.Archive(ctx, withCSRF(connect.NewRequest(&portcullisv1.ArchiveConnectionRequest{Id: "x"}), csrf))
			return err
		},
	}
	for name, call := range calls {
		err := call()
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Errorf("%s code = %v, want PermissionDenied", name, connect.CodeOf(err))
		}
		if err != nil && err.Error() != "permission_denied: permission denied" {
			t.Errorf("%s message = %q — must be the uniform generic denial", name, err.Error())
		}
	}
}
