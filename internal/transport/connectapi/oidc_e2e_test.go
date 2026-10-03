package connectapi_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
	"github.com/aportcullis/portcullis/internal/app/auth"
	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	"github.com/aportcullis/portcullis/internal/infra/googleoidc"
	"github.com/aportcullis/portcullis/internal/infra/oidctest"
	"github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/aportcullis/portcullis/internal/transport/connectapi"
)

const (
	oidcClientID   = "portcullis-test-client"
	oidcAdminEmail = "admin@example.com"
)

type oidcTestEnv struct {
	pool       *pgxpool.Pool
	issuer     *oidctest.Issuer
	serverURL  *url.URL
	jar        http.CookieJar
	client     *http.Client
	authClient portcullisv1connect.AuthClient
}

func newOIDCTestEnv(t *testing.T) *oidcTestEnv {
	t.Helper()
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := postgres.NewIdentityStore(pool)
	weak := crypto.Argon2Params{Memory: 8 * 1024, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 16}
	kr := loadTestKeyring(t)
	svc, err := auth.New(store, crypto.NewArgon2Hasher(weak, 4), crypto.NewCSRFProtector(kr), postgres.NewAuditStore(pool), auth.Config{})
	if err != nil {
		t.Fatal(err)
	}

	issuer := oidctest.New(t)
	provider, err := googleoidc.New(ctx, issuer.URL(), oidcClientID, "test-secret", "https://portcullis.test/auth/google/callback")
	if err != nil {
		t.Fatalf("googleoidc.New: %v", err)
	}
	svc.WithOIDCProvider(provider)

	orgID, err := store.DefaultOrganizationID(ctx)
	if err != nil {
		t.Fatalf("DefaultOrganizationID: %v", err)
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	oidcHandler := connectapi.NewOIDCHandler(svc, crypto.NewOIDCPendingCodec(kr, string(orgID)), nil, quiet)

	mux := http.NewServeMux()
	mux.Handle(connectapi.OIDCStartPattern, oidcHandler.Start())
	mux.Handle(connectapi.OIDCCallbackPattern, oidcHandler.Callback())
	catalog, err := authz.LoadCatalog(ctx, store)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	authzSvc, err := authz.New(store, catalog)
	if err != nil {
		t.Fatalf("authz.New: %v", err)
	}
	path, handler := portcullisv1connect.NewAuthHandler(
		connectapi.NewAuthService(svc, authzSvc),
		connect.WithInterceptors(connectapi.NewClientIPInterceptor(nil), connectapi.NewAuthInterceptor(svc)),
	)
	mux.Handle(path, handler)
	ts := httptest.NewTLSServer(mux)
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	client := ts.Client()
	client.Jar = jar
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	serverURL, _ := url.Parse(ts.URL)
	return &oidcTestEnv{
		pool:       pool,
		issuer:     issuer,
		serverURL:  serverURL,
		jar:        jar,
		client:     client,
		authClient: portcullisv1connect.NewAuthClient(client, ts.URL),
	}
}

func (e *oidcTestEnv) bootstrapAdmin(t *testing.T) {
	t.Helper()
	_, err := e.authClient.Bootstrap(context.Background(), connect.NewRequest(&portcullisv1.BootstrapRequest{
		Email: oidcAdminEmail, Password: "hunter2-secretz", DisplayName: "Admin",
	}))
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
}

func (e *oidcTestEnv) get(t *testing.T, path string) *http.Response {
	t.Helper()
	resp, err := e.client.Get(e.serverURL.String() + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func (e *oidcTestEnv) startFlow(t *testing.T) url.Values {
	t.Helper()
	resp := e.get(t, "/auth/google/start")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("start status = %d, want 302", resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse start Location: %v", err)
	}
	if got := loc.Scheme + "://" + loc.Host; got != e.issuer.URL() {
		t.Fatalf("start redirects to %q, want the issuer %q", got, e.issuer.URL())
	}
	if cookieFromJar(e.jar, e.serverURL, "__Host-portcullis_oidc") == "" {
		t.Fatal("start did not set the pending cookie")
	}
	return loc.Query()
}

func (e *oidcTestEnv) mintCode(q url.Values, subject, email string) string {
	return e.issuer.MintCode(oidctest.CodeOptions{
		Challenge:     q.Get("code_challenge"),
		Nonce:         q.Get("nonce"),
		Audience:      oidcClientID,
		Subject:       subject,
		Email:         email,
		EmailVerified: true,
		HostedDomain:  "example.com",
	})
}

func (e *oidcTestEnv) callback(t *testing.T, state, code string) *http.Response {
	t.Helper()
	return e.get(t, "/auth/google/callback?state="+url.QueryEscape(state)+"&code="+url.QueryEscape(code))
}

func (e *oidcTestEnv) assertFailureRedirect(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/login?error=oidc" {
		t.Errorf("failure Location = %q, want /login?error=oidc", loc)
	}
	if cookieFromJar(e.jar, e.serverURL, "__Host-portcullis_session") != "" {
		t.Error("failed login issued a session cookie")
	}
}

func (e *oidcTestEnv) countLoginAudit(t *testing.T, outcome audit.Outcome) int {
	t.Helper()
	var n int
	err := e.pool.QueryRow(context.Background(),
		`select count(*) from audit_events where action = 'AUTH_LOGIN' and outcome = $1 and metadata->>'method' = 'google'`,
		outcome).Scan(&n)
	if err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	return n
}

func TestOIDCLoginHappyPath(t *testing.T) {
	env := newOIDCTestEnv(t)
	env.bootstrapAdmin(t)

	q := env.startFlow(t)

	if q.Get("client_id") != oidcClientID || q.Get("response_type") != "code" {
		t.Errorf("client_id/response_type = %q/%q", q.Get("client_id"), q.Get("response_type"))
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Errorf("PKCE = %q/%q, want S256 + a challenge", q.Get("code_challenge_method"), q.Get("code_challenge"))
	}
	if q.Get("state") == "" || q.Get("nonce") == "" {
		t.Error("authorization URL is missing state or nonce")
	}
	if q.Get("scope") != "openid email profile" {
		t.Errorf("scope = %q", q.Get("scope"))
	}
	if q.Has("access_type") {
		t.Errorf("authorization URL requests access_type=%q; offline access is prohibited", q.Get("access_type"))
	}

	code := env.mintCode(q, "google-sub-1", oidcAdminEmail)
	resp := env.callback(t, q.Get("state"), code)
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		t.Fatalf("callback = %d → %q, want 302 → /", resp.StatusCode, resp.Header.Get("Location"))
	}

	if cookieFromJar(env.jar, env.serverURL, "__Host-portcullis_session") == "" {
		t.Error("no session cookie after Google login")
	}
	if csrfFromJar(env.jar, env.serverURL) == "" {
		t.Error("no CSRF cookie after Google login")
	}
	if cookieFromJar(env.jar, env.serverURL, "__Host-portcullis_oidc") != "" {
		t.Error("pending cookie survived the callback; it must be single-use")
	}

	meReq := connect.NewRequest(&portcullisv1.MeRequest{})
	meReq.Header().Set("X-CSRF-Token", csrfFromJar(env.jar, env.serverURL))
	me, err := env.authClient.Me(context.Background(), meReq)
	if err != nil || me.Msg.GetUser().GetEmail() != oidcAdminEmail {
		t.Errorf("Me after Google login = %v, %v", me, err)
	}

	var linked int
	if err := env.pool.QueryRow(context.Background(),
		`select count(*) from oidc_identities where subject = 'google-sub-1'`).Scan(&linked); err != nil || linked != 1 {
		t.Errorf("oidc_identities rows = %d (%v), want 1", linked, err)
	}
	if n := env.countLoginAudit(t, audit.OutcomeSucceeded); n != 1 {
		t.Errorf("AUTH_LOGIN/SUCCEEDED method=google audit rows = %d, want 1", n)
	}
}

func TestOIDCCallbackRejectsWrongState(t *testing.T) {
	env := newOIDCTestEnv(t)
	env.bootstrapAdmin(t)
	q := env.startFlow(t)
	code := env.mintCode(q, "google-sub-1", oidcAdminEmail)

	env.assertFailureRedirect(t, env.callback(t, "attacker-forged-state", code))
	if n := env.countLoginAudit(t, audit.OutcomeFailed); n != 1 {
		t.Errorf("AUTH_LOGIN/FAILED method=google audit rows = %d, want 1", n)
	}
}

func TestOIDCCallbackRejectsMissingPendingCookie(t *testing.T) {
	env := newOIDCTestEnv(t)
	env.bootstrapAdmin(t)

	env.assertFailureRedirect(t, env.callback(t, "some-state", "some-code"))
	if n := env.countLoginAudit(t, audit.OutcomeFailed); n != 1 {
		t.Errorf("AUTH_LOGIN/FAILED method=google audit rows = %d, want 1", n)
	}
}

func TestOIDCCallbackReplayFails(t *testing.T) {
	env := newOIDCTestEnv(t)
	env.bootstrapAdmin(t)
	q := env.startFlow(t)
	code := env.mintCode(q, "google-sub-1", oidcAdminEmail)

	first := env.callback(t, q.Get("state"), code)
	if first.Header.Get("Location") != "/" {
		t.Fatalf("first callback failed: → %q", first.Header.Get("Location"))
	}

	replay := env.callback(t, q.Get("state"), code)
	if replay.Header.Get("Location") != "/login?error=oidc" {
		t.Errorf("replayed callback → %q, want /login?error=oidc", replay.Header.Get("Location"))
	}
}

func TestOIDCCallbackRejectsUnknownEmail(t *testing.T) {
	env := newOIDCTestEnv(t)
	env.bootstrapAdmin(t)
	q := env.startFlow(t)
	code := env.mintCode(q, "google-sub-9", "stranger@example.com")

	env.assertFailureRedirect(t, env.callback(t, q.Get("state"), code))

	var users int
	if err := env.pool.QueryRow(context.Background(), `select count(*) from users`).Scan(&users); err != nil || users != 1 {
		t.Errorf("users = %d (%v), want 1", users, err)
	}
}

func TestOIDCCallbackRejectsProviderError(t *testing.T) {
	env := newOIDCTestEnv(t)
	env.bootstrapAdmin(t)
	q := env.startFlow(t)

	resp := env.get(t, "/auth/google/callback?state="+url.QueryEscape(q.Get("state"))+"&error=access_denied")
	env.assertFailureRedirect(t, resp)
}

func TestOIDCStartRateLimited(t *testing.T) {
	env := newOIDCTestEnv(t)

	var throttled bool
	for i := 0; i < 30; i++ {
		resp := env.get(t, "/auth/google/start")
		if resp.StatusCode == http.StatusTooManyRequests {
			throttled = true
			break
		}
	}
	if !throttled {
		t.Error("repeated /auth/google/start requests were never rate-limited")
	}
}

func TestOIDCCallbackRefusesHistoricallyVerifiedThirdPartyEmail(t *testing.T) {
	env := newOIDCTestEnv(t)
	env.bootstrapAdmin(t)
	q := env.startFlow(t)
	code := env.issuer.MintCode(oidctest.CodeOptions{Challenge: q.Get("code_challenge"), Nonce: q.Get("nonce"), Audience: oidcClientID, Subject: "untrusted-email-owner", Email: oidcAdminEmail, EmailVerified: true})
	env.assertFailureRedirect(t, env.callback(t, q.Get("state"), code))
	var links int
	if err := env.pool.QueryRow(context.Background(), `select count(*) from public.oidc_identities where subject=$1`, "untrusted-email-owner").Scan(&links); err != nil {
		t.Fatal(err)
	}
	if links != 0 {
		t.Fatalf("refused subject links=%d", links)
	}
	if failures := env.countLoginAudit(t, audit.OutcomeFailed); failures != 1 {
		t.Fatalf("login failures=%d", failures)
	}
}

func TestOIDCCallbackLinksGmailWithoutAHostedDomainClaim(t *testing.T) {
	env := newOIDCTestEnv(t)
	_, err := env.authClient.Bootstrap(context.Background(), connect.NewRequest(&portcullisv1.BootstrapRequest{Email: "admin@gmail.com", Password: "hunter2-secretz", DisplayName: "Admin"}))
	if err != nil {
		t.Fatal(err)
	}
	q := env.startFlow(t)
	code := env.issuer.MintCode(oidctest.CodeOptions{Challenge: q.Get("code_challenge"), Nonce: q.Get("nonce"), Audience: oidcClientID, Subject: "gmail-owner", Email: "admin@gmail.com", EmailVerified: true})
	response := env.callback(t, q.Get("state"), code)
	if response.Header.Get("Location") != "/" || cookieFromJar(env.jar, env.serverURL, "__Host-portcullis_session") == "" {
		t.Fatal("Gmail ownership did not issue a session")
	}
	if successes := env.countLoginAudit(t, audit.OutcomeSucceeded); successes != 1 {
		t.Fatalf("login successes=%d", successes)
	}
}
