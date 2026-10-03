package connectapi_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
	accessreq "github.com/aportcullis/portcullis/internal/app/accessrequest"
	auditapp "github.com/aportcullis/portcullis/internal/app/audit"
	"github.com/aportcullis/portcullis/internal/app/auth"
	"github.com/aportcullis/portcullis/internal/app/authz"
	connapp "github.com/aportcullis/portcullis/internal/app/connection"
	connpolicy "github.com/aportcullis/portcullis/internal/app/connectionpolicy"
	executionapp "github.com/aportcullis/portcullis/internal/app/execution"
	resultapp "github.com/aportcullis/portcullis/internal/app/result"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	"github.com/aportcullis/portcullis/internal/infra/pgdialect"
	"github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/aportcullis/portcullis/internal/transport/connectapi"
	"github.com/aportcullis/portcullis/internal/transport/server"
)

type connsTestEnv struct {
	pool      *pgxpool.Pool
	store     *postgres.IdentityStore
	hasher    auth.PasswordHasher
	serverURL *url.URL
	tsURL     string
	transport http.RoundTripper
	logs      *testLogBuffer
}

type testLogBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *testLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}

func (b *testLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
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
		pgdialect.New(pgdialect.Options{ValidateTimeout: 10 * time.Second}),
		crypto.NewConnectionCredentialCodec(keyring),
		postgres.NewAuditStore(pool),
	)
	if err != nil {
		t.Fatalf("connapp.New: %v", err)
	}
	policySvc, err := connpolicy.New(postgres.NewConnectionPolicyStore(pool))
	if err != nil {
		t.Fatalf("connpolicy.New: %v", err)
	}
	requestStore := postgres.NewAccessRequestStore(pool)
	requestSvc, err := accessreq.New(
		requestStore,
		requestStore,
		crypto.NewAccessRequestPayloadCodec(keyring),
		pgdialect.New(pgdialect.Options{ValidateTimeout: 10 * time.Second}),
		24*time.Hour,
	)
	if err != nil {
		t.Fatalf("accessreq.New: %v", err)
	}

	logs := &testLogBuffer{}
	chain := connect.WithInterceptors(
		connectapi.NewErrorLogInterceptor(slog.New(slog.NewJSONHandler(logs, nil))),
		connectapi.NewClientIPInterceptor(nil),
		connectapi.NewAuthInterceptor(authSvc),
	)
	// Apply production limits in the harness: Connect caps decompressed messages and MaxBytesHandler caps the request stream.
	readLimit := connect.WithReadMaxBytes(server.MaxRequestBytes)
	mux := http.NewServeMux()
	authPath, authHandler := portcullisv1connect.NewAuthHandler(connectapi.NewAuthService(authSvc, authzSvc), chain, readLimit)
	auditPath, auditHandler := portcullisv1connect.NewAuditHandler(connectapi.NewAuditService(authzSvc, auditReader), chain, readLimit)
	connsPath, connsHandler := portcullisv1connect.NewConnectionsHandler(connectapi.NewConnectionsService(authzSvc, connSvc), chain, readLimit)
	policiesPath, policiesHandler := portcullisv1connect.NewConnectionPoliciesHandler(connectapi.NewConnectionPoliciesService(authzSvc, policySvc), chain, readLimit)
	requestsPath, requestsHandler := portcullisv1connect.NewAccessRequestsHandler(connectapi.NewAccessRequestsService(authzSvc, requestSvc), chain, readLimit)
	resultSvc, err := resultapp.New(postgres.NewResultStore(pool), crypto.NewResultCodec(keyring), 2)
	if err != nil {
		t.Fatal(err)
	}
	executionSvc, err := executionapp.New(requestStore, requestStore, postgres.NewConnectionStore(pool), crypto.NewAccessRequestPayloadCodec(keyring), crypto.NewConnectionCredentialCodec(keyring), pgdialect.New(pgdialect.Options{}), resultSvc, "test-server", 2)
	if err != nil {
		t.Fatal(err)
	}
	executionPath, executionHandler := portcullisv1connect.NewQueryExecutionsHandler(connectapi.NewQueryExecutionsService(authzSvc, executionSvc, resultSvc), chain, readLimit, connect.WithCodec(connectapi.ExecutionJSONCodec{}), connect.WithInterceptors(connectapi.NewStreamSecurityInterceptor(authSvc, nil)))
	mux.Handle(executionPath, http.MaxBytesHandler(connectapi.BoundCSVWrites(executionHandler), server.MaxRequestBytes))
	mux.Handle(authPath, http.MaxBytesHandler(authHandler, server.MaxRequestBytes))
	mux.Handle(auditPath, http.MaxBytesHandler(auditHandler, server.MaxRequestBytes))
	mux.Handle(connsPath, http.MaxBytesHandler(connsHandler, server.MaxRequestBytes))
	mux.Handle(policiesPath, http.MaxBytesHandler(policiesHandler, server.MaxRequestBytes))
	mux.Handle(requestsPath, http.MaxBytesHandler(requestsHandler, server.MaxRequestBytes))
	ts := httptest.NewTLSServer(mux)
	t.Cleanup(ts.Close)

	serverURL, _ := url.Parse(ts.URL)
	return &connsTestEnv{pool: pool, store: store, hasher: hasher, serverURL: serverURL, tsURL: ts.URL, transport: ts.Client().Transport, logs: logs}
}

func (e *connsTestEnv) executionClient(jar http.CookieJar) portcullisv1connect.QueryExecutionsClient {
	return portcullisv1connect.NewQueryExecutionsClient(&http.Client{Transport: e.transport, Jar: jar}, e.tsURL)
}

func (e *connsTestEnv) clients() (http.CookieJar, portcullisv1connect.AuthClient, portcullisv1connect.ConnectionsClient, portcullisv1connect.AuditClient) {
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Transport: e.transport, Jar: jar}
	return jar, portcullisv1connect.NewAuthClient(hc, e.tsURL),
		portcullisv1connect.NewConnectionsClient(hc, e.tsURL),
		portcullisv1connect.NewAuditClient(hc, e.tsURL)
}

func (e *connsTestEnv) connectionClient(jar http.CookieJar) portcullisv1connect.ConnectionsClient {
	hc := &http.Client{Transport: e.transport, Jar: jar}
	return portcullisv1connect.NewConnectionsClient(hc, e.tsURL)
}

func (e *connsTestEnv) policyClient(jar http.CookieJar) portcullisv1connect.ConnectionPoliciesClient {
	hc := &http.Client{Transport: e.transport, Jar: jar}
	return portcullisv1connect.NewConnectionPoliciesClient(hc, e.tsURL)
}

func (e *connsTestEnv) requestClient(jar http.CookieJar) portcullisv1connect.AccessRequestsClient {
	hc := &http.Client{Transport: e.transport, Jar: jar}
	return portcullisv1connect.NewAccessRequestsClient(hc, e.tsURL)
}

// requestClientJSON is the same service over ProtoJSON — the encoding the SPA's connect-web transport uses by default. Size and encoding claims must be checked on the wire the product actually ships, not only on the Go client's binary one (ADR-0010/0013).
func (e *connsTestEnv) requestClientJSON(jar http.CookieJar) portcullisv1connect.AccessRequestsClient {
	hc := &http.Client{Transport: e.transport, Jar: jar}
	return portcullisv1connect.NewAccessRequestsClient(hc, e.tsURL, connect.WithProtoJSON())
}

func (e *connsTestEnv) authClient(jar http.CookieJar) portcullisv1connect.AuthClient {
	hc := &http.Client{Transport: e.transport, Jar: jar}
	return portcullisv1connect.NewAuthClient(hc, e.tsURL)
}

func (e *connsTestEnv) auditClient(jar http.CookieJar) portcullisv1connect.AuditClient {
	hc := &http.Client{Transport: e.transport, Jar: jar}
	return portcullisv1connect.NewAuditClient(hc, e.tsURL)
}

func (e *connsTestEnv) seedUser(t *testing.T, email, role string) {
	t.Helper()
	ctx := context.Background()
	u, err := e.store.CreateUser(ctx, email, "User "+role)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	org, err := e.store.DefaultOrganizationID(ctx)
	if err != nil {
		t.Fatalf("org: %v", err)
	}
	if _, err := e.pool.Exec(ctx,
		`insert into organization_memberships (organization_id, user_id, role_id)
		 select $1, $2, r.id from roles r where r.organization_id = $1 and r.name = $3`,
		string(org), string(u.ID), role); err != nil {
		t.Fatalf("membership: %v", err)
	}
}

func (e *connsTestEnv) seedUserWithCustomRole(t *testing.T, email, roleName string, perms ...string) {
	t.Helper()
	ctx := context.Background()
	u, err := e.store.CreateUser(ctx, email, "User "+roleName)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	org, err := e.store.DefaultOrganizationID(ctx)
	if err != nil {
		t.Fatalf("org: %v", err)
	}
	var roleID string
	if err := e.pool.QueryRow(ctx,
		`insert into roles (organization_id, name, is_system) values ($1, $2, false) returning id`,
		string(org), roleName).Scan(&roleID); err != nil {
		t.Fatalf("create role: %v", err)
	}
	for _, p := range perms {
		if _, err := e.pool.Exec(ctx,
			`insert into role_permissions (role_id, permission_key) values ($1, $2)`, roleID, p); err != nil {
			t.Fatalf("grant %s: %v", p, err)
		}
	}
	if _, err := e.pool.Exec(ctx,
		`insert into organization_memberships (organization_id, user_id, role_id) values ($1, $2, $3)`,
		string(org), string(u.ID), roleID); err != nil {
		t.Fatalf("membership: %v", err)
	}
}

func (e *connsTestEnv) loginAs(t *testing.T, email, password string) (portcullisv1connect.AccessRequestsClient, string) {
	t.Helper()
	ctx := context.Background()
	uid := e.userIDByEmail(t, email)
	phc, err := e.hasher.Hash(ctx, password)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := e.store.SetPassword(ctx, identity.UserID(uid), phc); err != nil {
		t.Fatalf("set password: %v", err)
	}
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Transport: e.transport, Jar: jar}
	authC := portcullisv1connect.NewAuthClient(hc, e.tsURL)
	if _, err := authC.Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: email, Password: password})); err != nil {
		t.Fatalf("Login %s: %v", email, err)
	}
	return portcullisv1connect.NewAccessRequestsClient(hc, e.tsURL), csrfFromJar(jar, e.serverURL)
}

func (e *connsTestEnv) userIDByEmail(t *testing.T, email string) string {
	t.Helper()
	var id string
	if err := e.pool.QueryRow(context.Background(), `select id from users where email = $1`, email).Scan(&id); err != nil {
		t.Fatalf("user id for %s: %v", email, err)
	}
	return id
}

func (e *connsTestEnv) targetConfig() *portcullisv1.ConnectionConfigInput {
	cc := e.pool.Config().ConnConfig
	return &portcullisv1.ConnectionConfigInput{
		Host:     cc.Host,
		Port:     uint32(cc.Port),
		Database: cc.Database,
		User:     cc.User,
		Password: cc.Password,
		TlsMode:  "disable",
	}
}

func withCSRF[T any](req *connect.Request[T], csrf string) *connect.Request[T] {
	req.Header().Set("X-CSRF-Token", csrf)
	return req
}

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

	// A malformed connection id is a client error (InvalidArgument), not a storage failure surfaced as Internal — checked with an admin who holds connections.get, so it can't be masked by a permission denial.
	if _, err := connsC.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetConnectionRequest{Id: "not-a-uuid"}), csrf)); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("Get(bad uuid) code = %v, want InvalidArgument", connect.CodeOf(err))
	}

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

	detail, err := connsC.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetConnectionRequest{Id: conn.GetId()}), csrf))
	if err != nil || detail.Msg.GetConnection().GetTargetFingerprint() == "" || detail.Msg.GetConnection().GetTlsMode() != "disable" {
		t.Fatalf("Get after Create = %+v, %v", detail.Msg.GetConnection(), err)
	}

	if _, err := connsC.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateConnectionRequest{
		DisplayName: "Primary", Config: env.targetConfig(),
	}), csrf)); connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Errorf("duplicate name code = %v, want AlreadyExists", connect.CodeOf(err))
	}

	testResp, err := connsC.Test(ctx, withCSRF(connect.NewRequest(&portcullisv1.TestConnectionRequest{
		Target: &portcullisv1.TestConnectionRequest_Id{Id: conn.GetId()},
	}), csrf))
	if err != nil || !testResp.Msg.GetOk() {
		t.Fatalf("Test by id = (%v, %v), want ok", testResp.Msg.GetOk(), err)
	}

	testResp, err = connsC.Test(ctx, withCSRF(connect.NewRequest(&portcullisv1.TestConnectionRequest{
		Target: &portcullisv1.TestConnectionRequest_Config{Config: badCfg},
	}), csrf))
	if err != nil {
		t.Fatalf("Test by config: %v", err)
	}
	if testResp.Msg.GetOk() || testResp.Msg.GetMessage() != "auth-failed" {
		t.Errorf("failed test = (%v, %q), want (false, auth-failed)", testResp.Msg.GetOk(), testResp.Msg.GetMessage())
	}

	// A NON-CANONICAL spelling of the id (uppercase) reaches the same row — PostgreSQL compares uuid values — and must behave exactly like the canonical one: the credential resealed by this update has to open for a later canonical-id test, i.e. the AAD must bind to the canonical id, not the caller's spelling.
	upper := strings.ToUpper(conn.GetId())
	if _, err := connsC.Update(ctx, withCSRF(connect.NewRequest(&portcullisv1.UpdateConnectionRequest{
		Id: upper, Config: env.targetConfig(), ExpectedVersion: conn.GetVersion(),
	}), csrf)); err != nil {
		t.Fatalf("Update with uppercase id: %v", err)
	}
	testResp, err = connsC.Test(ctx, withCSRF(connect.NewRequest(&portcullisv1.TestConnectionRequest{
		Target: &portcullisv1.TestConnectionRequest_Id{Id: conn.GetId()},
	}), csrf))
	if err != nil || !testResp.Msg.GetOk() {
		t.Fatalf("Test by canonical id after uppercase-id update = (%v, %v), want ok — credential resealed under a non-canonical AAD", testResp.Msg.GetOk(), err)
	}

	list, err := connsC.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListConnectionsRequest{}), csrf))
	if err != nil || len(list.Msg.GetConnections()) != 1 {
		t.Fatalf("List = %d conns, %v; want 1", len(list.Msg.GetConnections()), err)
	}
	got, err := connsC.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetConnectionRequest{Id: conn.GetId()}), csrf))
	if err != nil || got.Msg.GetConnection().GetDisplayName() != "Primary" {
		t.Fatalf("Get = %+v, %v", got.Msg.GetConnection(), err)
	}

	renamed, err := connsC.Update(ctx, withCSRF(connect.NewRequest(&portcullisv1.UpdateConnectionRequest{
		Id: conn.GetId(), DisplayName: "Primary (renamed)", ExpectedVersion: got.Msg.GetConnection().GetVersion(),
	}), csrf))
	if err != nil || renamed.Msg.GetConnection().GetDisplayName() != "Primary (renamed)" {
		t.Fatalf("Update rename = %+v, %v", renamed.Msg.GetConnection(), err)
	}

	for _, tc := range []struct {
		name    string
		version int64
		want    connect.Code
	}{
		{"the version just spent", got.Msg.GetConnection().GetVersion(), connect.CodeAborted},
		{"a future version", renamed.Msg.GetConnection().GetVersion() + 5, connect.CodeAborted},
		{"absent", 0, connect.CodeInvalidArgument},
		{"negative", -1, connect.CodeInvalidArgument},
	} {
		_, err := connsC.Update(ctx, withCSRF(connect.NewRequest(&portcullisv1.UpdateConnectionRequest{
			Id: conn.GetId(), DisplayName: "Hijacked", ExpectedVersion: tc.version,
		}), csrf))
		if connect.CodeOf(err) != tc.want {
			t.Errorf("update with %s = %v, want %s", tc.name, err, tc.want)
		}
	}
	after, err := connsC.Get(ctx, withCSRF(connect.NewRequest(&portcullisv1.GetConnectionRequest{Id: conn.GetId()}), csrf))
	if err != nil || after.Msg.GetConnection().GetDisplayName() != "Primary (renamed)" {
		t.Fatalf("after the refused updates = %+v, %v; want the rename intact", after.Msg.GetConnection(), err)
	}

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

	list, err = connsC.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListConnectionsRequest{}), csrf))
	if err != nil || len(list.Msg.GetConnections()) != 0 {
		t.Fatalf("List(active) after archive = %d, %v; want 0", len(list.Msg.GetConnections()), err)
	}
	list, err = connsC.List(ctx, withCSRF(connect.NewRequest(&portcullisv1.ListConnectionsRequest{IncludeArchived: true}), csrf))
	if err != nil || len(list.Msg.GetConnections()) != 1 {
		t.Fatalf("List(all) after archive = %d, %v; want 1", len(list.Msg.GetConnections()), err)
	}

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

func TestConnectionsPermissionDenied(t *testing.T) {
	env := newConnsTestEnv(t)
	ctx := context.Background()

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
