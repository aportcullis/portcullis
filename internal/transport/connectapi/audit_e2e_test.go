package connectapi_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/encoding/protojson"
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
	authPath, authHandler := portcullisv1connect.NewAuthHandler(connectapi.NewAuthService(authSvc, authzSvc), chain)
	auditPath, auditHandler := portcullisv1connect.NewAuditHandler(connectapi.NewAuditService(authzSvc, auditReader), chain)
	mux.Handle(authPath, authHandler)
	mux.Handle(auditPath, auditHandler)
	ts := httptest.NewTLSServer(mux)
	t.Cleanup(ts.Close)

	serverURL, _ := url.Parse(ts.URL)
	return &auditTestEnv{pool: pool, store: store, hasher: hasher, serverURL: serverURL, tsURL: ts.URL, transport: ts.Client().Transport}
}

func TestAuditGetReturnsStoredEvidence(t *testing.T) {
	env := newAuditTestEnv(t)
	ctx := context.Background()

	adminJar, adminAuth, adminAudit := env.clients()
	const email, password = "admin@example.com", "correct-horse-battery"
	if _, err := adminAuth.Bootstrap(ctx, connect.NewRequest(&portcullisv1.BootstrapRequest{
		Email: email, Password: password, DisplayName: "Admin",
	})); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if _, err := adminAuth.Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: email, Password: password})); err != nil {
		t.Fatalf("Login: %v", err)
	}
	csrf := csrfFromJar(adminJar, env.serverURL)

	var org, id string
	if err := env.pool.QueryRow(ctx, `select id::text from organizations limit 1`).Scan(&org); err != nil {
		t.Fatalf("read org: %v", err)
	}
	connID := uuid.NewString()
	digest := []byte{0xd1, 0x9e, 0x57}
	if err := env.pool.QueryRow(ctx,
		`insert into audit_events (
		     organization_id, actor_type, actor_service, action, target_type, target_id, outcome,
		     previous_state, next_state, connection_id, query_type, payload_digest, payload_digest_key_version)
		 values ($1, 'system', 'system:auto-approval', 'ACCESS_REQUEST_APPROVED', 'access_request', $2,
		         'succeeded', 'pending', 'approved', $3, 'read', $4, 1)
		 returning id::text`,
		org, uuid.NewString(), connID, digest).Scan(&id); err != nil {
		t.Fatalf("seed audit event: %v", err)
	}

	detailReq := connect.NewRequest(&portcullisv1.GetAuditEventRequest{Id: id})
	detailReq.Header().Set("X-CSRF-Token", csrf)
	detail, err := adminAudit.Get(ctx, detailReq)
	if err != nil {
		t.Fatalf("Audit.Get: %v", err)
	}
	got := detail.Msg.GetEvent()
	if got.GetPreviousState() != "pending" || got.GetNextState() != "approved" {
		t.Errorf("state transition = %q → %q, want pending → approved",
			got.GetPreviousState(), got.GetNextState())
	}
	if got.GetConnectionId() != connID {
		t.Errorf("connection_id = %q, want %q", got.GetConnectionId(), connID)
	}
	if got.GetQueryType() != "read" {
		t.Errorf("query_type = %q, want read", got.GetQueryType())
	}
	if !bytes.Equal(got.GetPayloadDigest(), digest) || got.GetPayloadDigestKeyVersion() != 1 {
		t.Errorf("digest = %x (key v%d), want %x (key v1)",
			got.GetPayloadDigest(), got.GetPayloadDigestKeyVersion(), digest)
	}

	listReq := connect.NewRequest(&portcullisv1.AuditListRequest{Page: 1, PageSize: 50})
	listReq.Header().Set("X-CSRF-Token", csrf)
	list, err := adminAudit.List(ctx, listReq)
	if err != nil {
		t.Fatalf("Audit.List: %v", err)
	}
	summary := list.Msg.GetEvents()[0]
	if summary.GetId() == "" || summary.GetAction() == "" {
		t.Fatalf("summary missing its own fields: %+v", summary)
	}

	for _, field := range []string{"sql", "select", "password", "param"} {
		if strings.Contains(strings.ToLower(protojson.Format(got)), field) {
			t.Errorf("Audit.Get response mentions %q — the trail must never echo payloads", field)
		}
	}
}

func TestAuditListAuthorization(t *testing.T) {
	env := newAuditTestEnv(t)
	ctx := context.Background()

	adminJar, adminAuth, adminAudit := env.clients()
	const email, password = "admin@example.com", "correct-horse-battery"
	if _, err := adminAuth.Bootstrap(ctx, connect.NewRequest(&portcullisv1.BootstrapRequest{Email: email, Password: password, DisplayName: "Admin"})); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if _, err := adminAuth.Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: email, Password: password})); err != nil {
		t.Fatalf("Login: %v", err)
	}
	adminCSRF := csrfFromJar(adminJar, env.serverURL)

	_, _, anonAudit := env.clients()
	if _, err := anonAudit.List(ctx, connect.NewRequest(&portcullisv1.AuditListRequest{})); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("anonymous Audit.List code = %v, want Unauthenticated", connect.CodeOf(err))
	}

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
	if msg.GetTotalPages() != 1 {
		t.Errorf("total_pages = %d, want 1", msg.GetTotalPages())
	}
	newest := msg.GetEvents()[0]
	if newest.GetAction() == "" || newest.GetOutcome() == "" {
		t.Errorf("audit event missing action/outcome: %+v", newest)
	}

	detailReq := connect.NewRequest(&portcullisv1.GetAuditEventRequest{Id: newest.GetId()})
	detailReq.Header().Set("X-CSRF-Token", adminCSRF)
	detail, err := adminAudit.Get(ctx, detailReq)
	if err != nil {
		t.Fatalf("admin Audit.Get: %v", err)
	}
	if detail.Msg.GetEvent().GetSourceIp() == "" {
		t.Errorf("Audit.Get event has no source_ip: %+v", detail.Msg.GetEvent())
	}

	badSort := connect.NewRequest(&portcullisv1.AuditListRequest{Page: 1, PageSize: 20, Sort: &portcullisv1.AuditSort{Field: "actor_user_id"}})
	badSort.Header().Set("X-CSRF-Token", adminCSRF)
	if _, err := adminAudit.List(ctx, badSort); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("off-whitelist sort code = %v, want InvalidArgument", connect.CodeOf(err))
	}

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

func (e *auditTestEnv) clients() (http.CookieJar, portcullisv1connect.AuthClient, portcullisv1connect.AuditClient) {
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Transport: e.transport, Jar: jar}
	return jar, portcullisv1connect.NewAuthClient(hc, e.tsURL), portcullisv1connect.NewAuditClient(hc, e.tsURL)
}
