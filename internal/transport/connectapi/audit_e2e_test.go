package connectapi_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
	auditapp "github.com/aportcullis/portcullis/internal/app/audit"
	"github.com/aportcullis/portcullis/internal/app/auth"
	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	"github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/aportcullis/portcullis/internal/transport/connectapi"
)

// auditTestEnv mounts the Auth and Audit RPCs behind the shared interceptor chain
// on one TLS server, so a bootstrapped session can be used to exercise the
// permission gate on Audit.List end-to-end.
type auditTestEnv struct {
	pool      *pgxpool.Pool
	store     *postgres.IdentityStore
	hasher    auth.PasswordHasher
	serverURL *url.URL
	tsURL     string
	transport http.RoundTripper
}

func newAuditTestEnv(t *testing.T) *auditTestEnv {
	t.Helper()
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	store := postgres.NewIdentityStore(pool)
	weak := crypto.Argon2Params{Memory: 8 * 1024, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 16}
	hasher := crypto.NewArgon2Hasher(weak, 4)
	authSvc, err := auth.New(store, hasher, crypto.NewCSRFProtector(loadTestKeyring(t)), postgres.NewAuditStore(pool), auth.Config{})
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

	chain := connect.WithInterceptors(
		connectapi.NewClientIPInterceptor(nil),
		connectapi.NewAuthInterceptor(authSvc),
	)
	mux := http.NewServeMux()
	authPath, authHandler := portcullisv1connect.NewAuthHandler(connectapi.NewAuthService(authSvc), chain)
	auditPath, auditHandler := portcullisv1connect.NewAuditHandler(connectapi.NewAuditService(authzSvc, auditReader), chain)
	mux.Handle(authPath, authHandler)
	mux.Handle(auditPath, auditHandler)
	ts := httptest.NewTLSServer(mux)
	t.Cleanup(ts.Close)

	serverURL, _ := url.Parse(ts.URL)
	return &auditTestEnv{pool: pool, store: store, hasher: hasher, serverURL: serverURL, tsURL: ts.URL, transport: ts.Client().Transport}
}

// TestAuditListAuthorization exercises the three enforcement paths of the first
// permission-gated RPC: no session, an authenticated user lacking the permission,
// and an admin that holds it (ADR-0008).
func TestAuditListAuthorization(t *testing.T) {
	env := newAuditTestEnv(t)
	ctx := context.Background()

	// Bootstrap the admin: the bootstrap-default role (admin) holds every catalog
	// permission, including audit.list.
	adminJar, adminAuth, adminAudit := env.clients()
	const email, password = "admin@example.com", "correct-horse-battery"
	if _, err := adminAuth.Bootstrap(ctx, connect.NewRequest(&portcullisv1.BootstrapRequest{Email: email, Password: password, DisplayName: "Admin"})); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if _, err := adminAuth.Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: email, Password: password})); err != nil {
		t.Fatalf("Login: %v", err)
	}
	adminCSRF := csrfFromJar(adminJar, env.serverURL)

	// 1) No session → Unauthenticated (the auth interceptor rejects before the handler).
	_, _, anonAudit := env.clients()
	if _, err := anonAudit.List(ctx, connect.NewRequest(&portcullisv1.AuditListRequest{})); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("anonymous Audit.List code = %v, want Unauthenticated", connect.CodeOf(err))
	}

	// 2) Authenticated but lacking audit.list: a user with a password and NO role
	// membership resolves to an empty permission set → PermissionDenied.
	u, err := env.store.CreateUser(ctx, "viewer@example.com", "Viewer")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	phc, err := env.hasher.Hash(ctx, password)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if err := env.store.SetPassword(ctx, u.ID, phc); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	viewerJar, viewerAuth, viewerAudit := env.clients()
	if _, err := viewerAuth.Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: "viewer@example.com", Password: password})); err != nil {
		t.Fatalf("viewer Login: %v", err)
	}
	viewerReq := connect.NewRequest(&portcullisv1.AuditListRequest{})
	viewerReq.Header().Set("X-CSRF-Token", csrfFromJar(viewerJar, env.serverURL))
	if _, err := viewerAudit.List(ctx, viewerReq); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("viewer Audit.List code = %v, want PermissionDenied", connect.CodeOf(err))
	}

	// 3) Admin holds audit.list: the trail is returned with the PRD §7.1 page
	// envelope (page, page_size, total_count, total_pages), already carrying the
	// bootstrap + login events emitted above.
	adminReq := connect.NewRequest(&portcullisv1.AuditListRequest{Page: 1, PageSize: 50})
	adminReq.Header().Set("X-CSRF-Token", adminCSRF)
	resp, err := adminAudit.List(ctx, adminReq)
	if err != nil {
		t.Fatalf("admin Audit.List: %v", err)
	}
	msg := resp.Msg
	if len(msg.GetEvents()) == 0 {
		t.Fatal("admin Audit.List returned no events, want the bootstrap/login trail")
	}
	if msg.GetPage() != 1 || msg.GetPageSize() != 50 {
		t.Errorf("echoed page/page_size = %d/%d, want 1/50", msg.GetPage(), msg.GetPageSize())
	}
	if msg.GetTotalCount() < uint64(len(msg.GetEvents())) || msg.GetTotalCount() == 0 {
		t.Errorf("total_count = %d, want ≥ returned events and > 0", msg.GetTotalCount())
	}
	if msg.GetTotalPages() != 1 { // a handful of events fit one 50-row page
		t.Errorf("total_pages = %d, want 1", msg.GetTotalPages())
	}
	newest := msg.GetEvents()[0]
	if newest.GetAction() == "" || newest.GetOutcome() == "" {
		t.Errorf("audit event missing action/outcome: %+v", newest)
	}
	// List is deliberately summary-only. The same event's correlation/network
	// details require audit.get (ADR-0008's collection/detail boundary).
	detailReq := connect.NewRequest(&portcullisv1.GetAuditEventRequest{Id: newest.GetId()})
	detailReq.Header().Set("X-CSRF-Token", adminCSRF)
	detail, err := adminAudit.Get(ctx, detailReq)
	if err != nil {
		t.Fatalf("admin Audit.Get: %v", err)
	}
	if detail.Msg.GetEvent().GetSourceIp() == "" {
		t.Errorf("Audit.Get event has no source_ip: %+v", detail.Msg.GetEvent())
	}

	// An off-whitelist sort column is rejected (PRD §7.1 column whitelist).
	badSort := connect.NewRequest(&portcullisv1.AuditListRequest{Page: 1, PageSize: 20, Sort: &portcullisv1.AuditSort{Field: "actor_user_id"}})
	badSort.Header().Set("X-CSRF-Token", adminCSRF)
	if _, err := adminAudit.List(ctx, badSort); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("off-whitelist sort code = %v, want InvalidArgument", connect.CodeOf(err))
	}

	// A sort that names the column without a direction keeps the documented
	// default: descending, newest first. Plain proto3 bool could not express
	// "unset", silently flipping this request to oldest-first (external review) —
	// descending is optional so absence is distinguishable from explicit false.
	fieldOnly := connect.NewRequest(&portcullisv1.AuditListRequest{Page: 1, PageSize: 50, Sort: &portcullisv1.AuditSort{Field: "occurred_at"}})
	fieldOnly.Header().Set("X-CSRF-Token", adminCSRF)
	byField, err := adminAudit.List(ctx, fieldOnly)
	if err != nil {
		t.Fatalf("field-only sort Audit.List: %v", err)
	}
	events := byField.Msg.GetEvents()
	if len(events) < 2 {
		t.Fatalf("want ≥2 events (bootstrap + logins), got %d", len(events))
	}
	first, last := events[0].GetOccurredAt().AsTime(), events[len(events)-1].GetOccurredAt().AsTime()
	if first.Before(last) {
		t.Errorf("field-only sort returned oldest-first (%v … %v), want the documented descending default", first, last)
	}

	// Explicit descending=false is the ascending opt-in.
	asc := connect.NewRequest(&portcullisv1.AuditListRequest{Page: 1, PageSize: 50, Sort: &portcullisv1.AuditSort{Field: "occurred_at", Descending: proto.Bool(false)}})
	asc.Header().Set("X-CSRF-Token", adminCSRF)
	byAsc, err := adminAudit.List(ctx, asc)
	if err != nil {
		t.Fatalf("ascending sort Audit.List: %v", err)
	}
	events = byAsc.Msg.GetEvents()
	first, last = events[0].GetOccurredAt().AsTime(), events[len(events)-1].GetOccurredAt().AsTime()
	if first.After(last) {
		t.Errorf("descending=false returned newest-first (%v … %v), want ascending", first, last)
	}
}

// clients returns an Auth+Audit client pair sharing a fresh cookie jar (one
// browser-shaped session), so each caller in a test has an isolated session.
func (e *auditTestEnv) clients() (http.CookieJar, portcullisv1connect.AuthClient, portcullisv1connect.AuditClient) {
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Transport: e.transport, Jar: jar}
	return jar, portcullisv1connect.NewAuthClient(hc, e.tsURL), portcullisv1connect.NewAuditClient(hc, e.tsURL)
}
