package connectapi_test

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
	"github.com/aportcullis/portcullis/internal/app/auth"
	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	"github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/aportcullis/portcullis/internal/transport/connectapi"
)

func loadTestKeyring(t *testing.T) *crypto.Keyring {
	t.Helper()
	raw := make([]byte, 32)
	for idx := range raw {
		raw[idx] = byte(idx + 1)
	}
	kr, err := crypto.LoadKeyring(base64.StdEncoding.EncodeToString(raw), "")
	if err != nil {
		t.Fatalf("LoadKeyring: %v", err)
	}
	return kr
}

func cookieFromJar(jar http.CookieJar, u *url.URL, name string) string {
	for _, c := range jar.Cookies(u) {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func csrfFromJar(jar http.CookieJar, u *url.URL) string {
	return cookieFromJar(jar, u, "__Host-portcullis_csrf")
}

type authTestEnv struct {
	pool      *pgxpool.Pool
	jar       http.CookieJar
	serverURL *url.URL
	client    portcullisv1connect.AuthClient
	raw       portcullisv1connect.AuthClient
}

type authEnvOptions struct {
	wrapStore      func(*postgres.IdentityStore) auth.Repository
	trustedProxies []*net.IPNet
	rateLimit      bool
	// sharedPool serves another instance over an already-migrated metadata database, as after a restart with a different keyring.
	sharedPool *pgxpool.Pool
	// keyring overrides the default single-version test keyring.
	keyring *crypto.Keyring
}

func newAuthTestEnv(t *testing.T, opts authEnvOptions) *authTestEnv {
	t.Helper()
	ctx := context.Background()
	pool := opts.sharedPool
	if pool == nil {
		pool = dbtest.FreshPostgres(t)
		if err := postgres.Migrate(ctx, pool); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	}
	keyring := opts.keyring
	if keyring == nil {
		keyring = loadTestKeyring(t)
	}

	store := postgres.NewIdentityStore(pool)
	var repo auth.Repository = store
	if opts.wrapStore != nil {
		repo = opts.wrapStore(store)
	}
	weak := crypto.Argon2Params{Memory: 8 * 1024, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 16}
	svc, err := auth.New(repo, crypto.NewArgon2Hasher(weak, 4), crypto.NewCSRFProtector(keyring), postgres.NewAuditStore(pool), auth.Config{})
	if err != nil {
		t.Fatal(err)
	}

	catalog, err := authz.LoadCatalog(ctx, store)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	authzSvc, err := authz.New(store, catalog)
	if err != nil {
		t.Fatalf("authz.New: %v", err)
	}

	interceptors := []connect.Interceptor{connectapi.NewClientIPInterceptor(opts.trustedProxies)}
	if opts.rateLimit {
		interceptors = append(interceptors, connectapi.NewRateLimitInterceptor())
	}
	interceptors = append(interceptors, connectapi.NewAuthInterceptor(svc))

	path, handler := portcullisv1connect.NewAuthHandler(
		connectapi.NewAuthService(svc, authzSvc),
		connect.WithInterceptors(interceptors...),
	)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	ts := httptest.NewTLSServer(mux)
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	httpClient := ts.Client()
	httpClient.Jar = jar
	serverURL, _ := url.Parse(ts.URL)
	return &authTestEnv{
		pool:      pool,
		jar:       jar,
		serverURL: serverURL,
		client:    portcullisv1connect.NewAuthClient(httpClient, ts.URL),
		raw:       portcullisv1connect.NewAuthClient(&http.Client{Transport: ts.Client().Transport}, ts.URL),
	}
}

func (e *authTestEnv) bootstrapAndLogin(t *testing.T, email, password string) string {
	t.Helper()
	ctx := context.Background()
	if _, err := e.client.Bootstrap(ctx, connect.NewRequest(&portcullisv1.BootstrapRequest{Email: email, Password: password, DisplayName: "Admin"})); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	login, err := e.client.Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: email, Password: password}))
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if got := login.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Login Cache-Control = %q, want no-store", got)
	}
	csrf := csrfFromJar(e.jar, e.serverURL)
	if csrf == "" {
		t.Fatal("login did not set the CSRF cookie")
	}
	return csrf
}

func TestLoginProgressiveBackoffE2E(t *testing.T) {
	env := newAuthTestEnv(t, authEnvOptions{})
	ctx := context.Background()

	const email, password = "admin@example.com", "correct-horse-battery"
	if _, err := env.client.Bootstrap(ctx, connect.NewRequest(&portcullisv1.BootstrapRequest{Email: email, Password: password, DisplayName: "Admin"})); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	login := func(pw string) error {
		_, err := env.client.Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: email, Password: pw}))
		return err
	}

	var wrongMsg string
	for i := 0; i < 5; i++ {
		err := login("wrong-password-xx")
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Fatalf("wrong password %d code = %v, want Unauthenticated", i+1, connect.CodeOf(err))
		}
		wrongMsg = err.Error()
	}

	// Locked: the correct password is refused with the SAME code and message as a wrong password — the lockout must not be observable.
	err := login(password)
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("correct password while locked code = %v, want Unauthenticated", connect.CodeOf(err))
	}
	if err.Error() != wrongMsg {
		t.Errorf("locked rejection %q differs from wrong-password rejection %q (oracle)", err.Error(), wrongMsg)
	}

	// The lockout is server-side state; expire it directly (the window is jittered wall-clock time, not something a test should sleep through).
	if _, err := env.pool.Exec(ctx, `update login_backoff set locked_until = now() - interval '1 second'
		where user_id = (select id from users where lower(email) = lower($1))`, email); err != nil {
		t.Fatalf("expire lockout: %v", err)
	}

	if err := login(password); err != nil {
		t.Fatalf("correct password after expiry = %v, want success", err)
	}

	var count int
	var lockedUntil *time.Time
	if err := env.pool.QueryRow(ctx, `select failure_count, locked_until from login_backoff
		where user_id = (select id from users where lower(email) = lower($1))`, email).Scan(&count, &lockedUntil); err != nil {
		t.Fatalf("query login_backoff: %v", err)
	}
	if count != 0 || lockedUntil != nil {
		t.Errorf("backoff after success = (%d, %v), want (0, nil)", count, lockedUntil)
	}

	var failures, lockouts int
	if err := env.pool.QueryRow(ctx, `
		select count(*),
		       count(*) filter (where metadata->>'lockout' = 'true')
		from audit_events where action = 'AUTH_LOGIN' and outcome = 'FAILED'`).Scan(&failures, &lockouts); err != nil {
		t.Fatalf("query audit_events: %v", err)
	}
	if failures != 6 {
		t.Errorf("failed-login audit events = %d, want 6", failures)
	}
	if lockouts != 2 {
		t.Errorf("lockout-tagged audit events = %d, want 2 (failures 5 and 6)", lockouts)
	}
}

func TestLoginRateLimited(t *testing.T) {
	env := newAuthTestEnv(t, authEnvOptions{rateLimit: true})
	ctx := context.Background()

	var throttled bool
	for i := 0; i < 40; i++ {
		_, err := env.client.Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: "nobody@example.com", Password: "wrong-but-long-enough"}))
		if connect.CodeOf(err) == connect.CodeResourceExhausted {
			throttled = true
			break
		}
	}
	if !throttled {
		t.Error("repeated logins from one client were never rate-limited")
	}
}

func TestOrdinaryInteractionBurstIsNotThrottled(t *testing.T) {
	env := newAuthTestEnv(t, authEnvOptions{rateLimit: true})
	ctx := context.Background()
	csrf := env.bootstrapAndLogin(t, "burst-admin@example.com", "burst-admin-password-1")

	const burst = 120
	for i := range burst {
		req := connect.NewRequest(&portcullisv1.MeRequest{})
		req.Header().Set("X-CSRF-Token", csrf)
		if _, err := env.client.Me(ctx, req); err != nil {
			t.Fatalf("request %d of an ordinary interaction burst was refused: %v (%v)", i+1, connect.CodeOf(err), err)
		}
	}
}

func TestAuthenticatedProcedureRateLimited(t *testing.T) {
	env := newAuthTestEnv(t, authEnvOptions{rateLimit: true})
	ctx := context.Background()

	// Beyond any interactive burst the test above pins: the flood must still be shed, only later.
	var throttled bool
	for i := 0; i < 600; i++ {
		req := connect.NewRequest(&portcullisv1.MeRequest{})
		req.Header().Set("Cookie", "__Host-portcullis_session=garbage-token")
		_, err := env.raw.Me(ctx, req)
		if connect.CodeOf(err) == connect.CodeResourceExhausted {
			throttled = true
			break
		}
	}
	if !throttled {
		t.Error("garbage-session requests to an authenticated procedure were never rate-limited")
	}
}

func TestLoginRateLimitTrustsProxyForwardedFor(t *testing.T) {

	_, loopback, err := net.ParseCIDR("127.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	env := newAuthTestEnv(t, authEnvOptions{trustedProxies: []*net.IPNet{loopback}, rateLimit: true})
	ctx := context.Background()

	login := func(forwardedFor string) error {
		req := connect.NewRequest(&portcullisv1.LoginRequest{Email: "", Password: "wrong-but-long-enough"})
		req.Header().Set("X-Forwarded-For", forwardedFor)
		_, err := env.client.Login(ctx, req)
		return err
	}

	var throttled bool
	for i := 0; i < 40; i++ {
		if connect.CodeOf(login("1.1.1.1, 9.9.9.9")) == connect.CodeResourceExhausted {
			throttled = true
			break
		}
	}
	if !throttled {
		t.Fatal("forwarded client 9.9.9.9 was never throttled")
	}
	// Same real client, different spoofed left entry: the key must come from the right, so this stays throttled (a left-to-right bug would open a fresh bucket).
	if code := connect.CodeOf(login("2.2.2.2, 9.9.9.9")); code != connect.CodeResourceExhausted {
		t.Errorf("spoofed left entry opened a fresh bucket (key not taken right-to-left); got %v", code)
	}

	if code := connect.CodeOf(login("2.2.2.2, 7.7.7.7")); code == connect.CodeResourceExhausted {
		t.Errorf("distinct forwarded client 7.7.7.7 shares a bucket; got %v", code)
	}
}

func TestCSRFRequiresSessionBoundHMAC(t *testing.T) {
	env := newAuthTestEnv(t, authEnvOptions{})
	ctx := context.Background()
	csrf := env.bootstrapAndLogin(t, "admin@example.com", "correct-horse-battery")
	sess := cookieFromJar(env.jar, env.serverURL, "__Host-portcullis_session")
	if sess == "" {
		t.Fatal("login did not set the session cookie")
	}

	me := func(cookieVal, headerVal string) error {
		req := connect.NewRequest(&portcullisv1.MeRequest{})
		req.Header().Set("Cookie", "__Host-portcullis_session="+sess+"; __Host-portcullis_csrf="+cookieVal)
		req.Header().Set("X-CSRF-Token", headerVal)
		_, err := env.raw.Me(ctx, req)
		return err
	}

	if err := me(csrf, csrf); err != nil {
		t.Fatalf("genuine CSRF token via manual cookies rejected: %v", err)
	}

	// Attack: a well-formed forged token (mac.nonce shape), identical in cookie and header. Naive double-submit would accept it.
	enc := base64.RawURLEncoding
	forged := enc.EncodeToString([]byte("forged-mac-value")) + "." + enc.EncodeToString([]byte("forged-nonce"))
	if code := connect.CodeOf(me(forged, forged)); code != connect.CodePermissionDenied {
		t.Errorf("forged cookie==header CSRF token code = %v, want PermissionDenied (HMAC binding)", code)
	}
}

func TestSessionSurvivesMalformedSiblingCookie(t *testing.T) {
	env := newAuthTestEnv(t, authEnvOptions{})
	ctx := context.Background()
	csrf := env.bootstrapAndLogin(t, "admin@example.com", "correct-horse-battery")
	sess := cookieFromJar(env.jar, env.serverURL, "__Host-portcullis_session")
	if sess == "" {
		t.Fatal("login did not set the session cookie")
	}

	req := connect.NewRequest(&portcullisv1.MeRequest{})

	req.Header().Set("Cookie",
		"promo=caf\xc3\xa9; __Host-portcullis_session="+sess+"; __Host-portcullis_csrf="+csrf)
	req.Header().Set("X-CSRF-Token", csrf)
	if _, err := env.raw.Me(ctx, req); err != nil {
		t.Errorf("a malformed sibling cookie locked the session out: %v", err)
	}
}

func TestAuditSourceIPIsCanonical(t *testing.T) {
	_, loopback, err := net.ParseCIDR("127.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	env := newAuthTestEnv(t, authEnvOptions{trustedProxies: []*net.IPNet{loopback}})
	ctx := context.Background()

	req := connect.NewRequest(&portcullisv1.LoginRequest{Email: "nobody@example.com", Password: "wrong-but-long-enough"})
	req.Header().Set("X-Forwarded-For", "2001:0db8::1")
	if _, err := env.client.Login(ctx, req); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("unknown-user login code = %v, want Unauthenticated", connect.CodeOf(err))
	}

	var sourceIP string
	if err := env.pool.QueryRow(ctx,
		`select metadata->>'source_ip' from audit_events
		 where action = 'AUTH_LOGIN' and outcome = 'FAILED'
		 order by occurred_at desc limit 1`,
	).Scan(&sourceIP); err != nil {
		t.Fatalf("read audit source_ip: %v", err)
	}
	if sourceIP != "2001:db8::1" {
		t.Errorf("audit source_ip = %q, want canonical %q", sourceIP, "2001:db8::1")
	}
}

type flakyIdleStore struct {
	*postgres.IdentityStore
	failIdle   atomic.Bool
	failLookup atomic.Bool
}

func (s *flakyIdleStore) ExtendSessionIdle(ctx context.Context, id identity.SessionID, idle time.Time) error {
	if s.failIdle.Load() {
		return errors.New("transient db error")
	}
	return s.IdentityStore.ExtendSessionIdle(ctx, id, idle)
}

func (s *flakyIdleStore) GetSessionByTokenHash(ctx context.Context, tokenHash []byte) (identity.Session, error) {
	if s.failLookup.Load() {
		return identity.Session{}, errors.New("transient db error")
	}
	return s.IdentityStore.GetSessionByTokenHash(ctx, tokenHash)
}

func TestSlideIdleInfraErrorIsUnavailable(t *testing.T) {
	var store *flakyIdleStore
	env := newAuthTestEnv(t, authEnvOptions{wrapStore: func(s *postgres.IdentityStore) auth.Repository {
		store = &flakyIdleStore{IdentityStore: s}
		return store
	}})
	ctx := context.Background()
	csrf := env.bootstrapAndLogin(t, "admin@example.com", "correct-horse-battery")

	if _, err := env.pool.Exec(ctx, `update sessions set idle_expires_at = idle_expires_at - interval '2 minutes'`); err != nil {
		t.Fatalf("age session: %v", err)
	}
	store.failIdle.Store(true)

	me := func() error {
		req := connect.NewRequest(&portcullisv1.MeRequest{})
		req.Header().Set("X-CSRF-Token", csrf)
		_, err := env.client.Me(ctx, req)
		return err
	}
	if code := connect.CodeOf(me()); code != connect.CodeUnavailable {
		t.Errorf("Me during idle-slide DB failure code = %v, want Unavailable (session is still valid)", code)
	}

	store.failIdle.Store(false)
	if err := me(); err != nil {
		t.Errorf("Me after recovery = %v, want success", err)
	}

	store.failLookup.Store(true)
	if code := connect.CodeOf(me()); code != connect.CodeUnavailable {
		t.Errorf("Me during session-lookup DB failure code = %v, want Unavailable", code)
	}
	store.failLookup.Store(false)
	if err := me(); err != nil {
		t.Errorf("Me after lookup recovery = %v, want success", err)
	}
}

func TestAuthE2E(t *testing.T) {
	env := newAuthTestEnv(t, authEnvOptions{})
	ctx := context.Background()

	const email, password = "admin@example.com", "correct-horse-battery"

	cfg, err := env.raw.GetConfig(ctx, connect.NewRequest(&portcullisv1.GetConfigRequest{}))
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if !cfg.Msg.GetNeedsBootstrap() || cfg.Msg.GetGoogleEnabled() {
		t.Errorf("fresh GetConfig = %+v, want needs_bootstrap + no google", cfg.Msg)
	}

	if _, err := env.client.Bootstrap(ctx, connect.NewRequest(&portcullisv1.BootstrapRequest{Email: email, Password: password, DisplayName: "Admin"})); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	cfg, err = env.raw.GetConfig(ctx, connect.NewRequest(&portcullisv1.GetConfigRequest{}))
	if err != nil {
		t.Fatalf("GetConfig after bootstrap: %v", err)
	}
	if cfg.Msg.GetNeedsBootstrap() {
		t.Error("GetConfig still reports needs_bootstrap after bootstrap")
	}
	if _, err := env.client.Bootstrap(ctx, connect.NewRequest(&portcullisv1.BootstrapRequest{Email: "x@y.z", Password: "another-valid-password", DisplayName: "X"})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("second Bootstrap code = %v, want FailedPrecondition", connect.CodeOf(err))
	}

	if _, err := env.client.Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: email, Password: "wrong"})); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("bad login code = %v, want Unauthenticated", connect.CodeOf(err))
	}

	loginResp, err := env.client.Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: email, Password: password}))
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if loginResp.Msg.GetUser().GetEmail() != email {
		t.Errorf("login user = %v", loginResp.Msg.GetUser())
	}
	csrf := csrfFromJar(env.jar, env.serverURL)
	if csrf == "" {
		t.Fatal("Login did not set the CSRF cookie")
	}

	if _, err := env.client.Me(ctx, connect.NewRequest(&portcullisv1.MeRequest{})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("Me without CSRF code = %v, want PermissionDenied", connect.CodeOf(err))
	}

	meReq := connect.NewRequest(&portcullisv1.MeRequest{})
	meReq.Header().Set("X-CSRF-Token", csrf)
	meResp, err := env.client.Me(ctx, meReq)
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if meResp.Msg.GetUser().GetEmail() != email {
		t.Errorf("Me user = %v", meResp.Msg.GetUser())
	}

	logoutReq := connect.NewRequest(&portcullisv1.LogoutRequest{})
	logoutReq.Header().Set("X-CSRF-Token", csrf)
	if _, err := env.client.Logout(ctx, logoutReq); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	meReq2 := connect.NewRequest(&portcullisv1.MeRequest{})
	meReq2.Header().Set("X-CSRF-Token", csrf)
	if _, err := env.client.Me(ctx, meReq2); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("Me after logout code = %v, want Unauthenticated", connect.CodeOf(err))
	}

	rows, err := env.pool.Query(ctx, `select action, outcome, metadata->>'source_ip' from audit_events order by occurred_at, action`)
	if err != nil {
		t.Fatalf("query audit_events: %v", err)
	}
	defer rows.Close()
	got := map[string]int{}
	for rows.Next() {
		var action, outcome string
		var sourceIP *string
		if err := rows.Scan(&action, &outcome, &sourceIP); err != nil {
			t.Fatalf("scan audit row: %v", err)
		}
		got[action+"/"+outcome]++
		if sourceIP == nil || *sourceIP == "" {
			t.Errorf("audit %s/%s has no source_ip", action, outcome)
		}
	}
	want := map[string]int{
		"AUTH_BOOTSTRAP/SUCCEEDED": 1,
		"AUTH_LOGIN/FAILED":        1,
		"AUTH_LOGIN/SUCCEEDED":     1,
		"AUTH_LOGOUT/SUCCEEDED":    1,
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("audit trail %s = %d, want %d (all: %v)", k, got[k], n, got)
		}
	}
}
