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
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	"github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/aportcullis/portcullis/internal/transport/connectapi"
)

func loadTestKeyring(t *testing.T) *crypto.Keyring {
	t.Helper()
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i + 1)
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

// authTestEnv is the shared end-to-end fixture: a migrated fresh database, the
// auth service on cheap Argon2 params, and a TLS httptest server (so the jar
// sends the Secure __Host- cookies back) running the interceptor chain.
type authTestEnv struct {
	pool      *pgxpool.Pool
	jar       http.CookieJar
	serverURL *url.URL
	client    portcullisv1connect.AuthClient // cookie-jar client (normal browser shape)
	raw       portcullisv1connect.AuthClient // jar-free: Cookie headers controlled byte-for-byte
}

// authEnvOptions vary the fixture per scenario; the zero value is the common
// shape (real store, no trusted proxies, no rate limiting).
type authEnvOptions struct {
	wrapStore      func(*postgres.IdentityStore) auth.Repository
	trustedProxies []*net.IPNet
	rateLimit      bool
}

func newAuthTestEnv(t *testing.T, opts authEnvOptions) *authTestEnv {
	t.Helper()
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	store := postgres.NewIdentityStore(pool)
	var repo auth.Repository = store
	if opts.wrapStore != nil {
		repo = opts.wrapStore(store)
	}
	weak := crypto.Argon2Params{Memory: 8 * 1024, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 16}
	svc, err := auth.New(repo, crypto.NewArgon2Hasher(weak, 4), crypto.NewCSRFProtector(loadTestKeyring(t)), postgres.NewAuditStore(pool), auth.Config{})
	if err != nil {
		t.Fatal(err)
	}

	interceptors := []connect.Interceptor{connectapi.NewClientIPInterceptor(opts.trustedProxies)}
	if opts.rateLimit {
		interceptors = append(interceptors, connectapi.NewRateLimitInterceptor())
	}
	interceptors = append(interceptors, connectapi.NewAuthInterceptor(svc))

	path, handler := portcullisv1connect.NewAuthHandler(
		connectapi.NewAuthService(svc),
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

// bootstrapAndLogin creates the first admin and logs in through the jar client,
// returning the CSRF cookie value for authenticated calls.
func (e *authTestEnv) bootstrapAndLogin(t *testing.T, email, password string) string {
	t.Helper()
	ctx := context.Background()
	if _, err := e.client.Bootstrap(ctx, connect.NewRequest(&portcullisv1.BootstrapRequest{Email: email, Password: password, DisplayName: "Admin"})); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if _, err := e.client.Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: email, Password: password})); err != nil {
		t.Fatalf("Login: %v", err)
	}
	csrf := csrfFromJar(e.jar, e.serverURL)
	if csrf == "" {
		t.Fatal("login did not set the CSRF cookie")
	}
	return csrf
}

// Progressive backoff end-to-end (ADR-0006): five wrong passwords lock the
// account; the CORRECT password then gets a byte-identical rejection (no
// oracle); once the lockout expires the correct password signs in and the
// slate is cleared; the lockouts are queryable in the audit trail.
func TestLoginProgressiveBackoffE2E(t *testing.T) {
	env := newAuthTestEnv(t, authEnvOptions{}) // rate limiting off: the backoff is under test
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

	// Locked: the correct password is refused with the SAME code and message as a
	// wrong password — the lockout must not be observable.
	err := login(password)
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("correct password while locked code = %v, want Unauthenticated", connect.CodeOf(err))
	}
	if err.Error() != wrongMsg {
		t.Errorf("locked rejection %q differs from wrong-password rejection %q (oracle)", err.Error(), wrongMsg)
	}

	// The lockout is server-side state; expire it directly (the window is
	// jittered wall-clock time, not something a test should sleep through).
	if _, err := env.pool.Exec(ctx, `update login_backoff set locked_until = now() - interval '1 second'
		where user_id = (select id from users where lower(email) = lower($1))`, email); err != nil {
		t.Fatalf("expire lockout: %v", err)
	}

	if err := login(password); err != nil {
		t.Fatalf("correct password after expiry = %v, want success", err)
	}

	// Success cleared the slate.
	var count int
	var lockedUntil *time.Time
	if err := env.pool.QueryRow(ctx, `select failure_count, locked_until from login_backoff
		where user_id = (select id from users where lower(email) = lower($1))`, email).Scan(&count, &lockedUntil); err != nil {
		t.Fatalf("query login_backoff: %v", err)
	}
	if count != 0 || lockedUntil != nil {
		t.Errorf("backoff after success = (%d, %v), want (0, nil)", count, lockedUntil)
	}

	// The trail: 6 failed logins (5 wrong + the locked correct attempt), of which
	// the ones at/after the threshold are tagged with lockout metadata.
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

	// Fire more than the burst of wrong-password logins from one client: the first
	// burst is rejected on credentials (Unauthenticated), then the limiter kicks in
	// with ResourceExhausted — before any hashing on the throttled requests.
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

// Authenticated procedures (Me/Logout) are throttled per-IP too, so a flood of
// requests carrying garbage session cookies can't drive unbounded DB session
// lookups. The per-IP auth bucket trips with ResourceExhausted regardless of the
// (invalid) session, before the auth interceptor's lookup.
func TestAuthenticatedProcedureRateLimited(t *testing.T) {
	env := newAuthTestEnv(t, authEnvOptions{rateLimit: true})
	ctx := context.Background()

	var throttled bool
	for i := 0; i < 200; i++ {
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
	// Trust the loopback proxy (the httptest peer) so X-Forwarded-For is honored.
	_, loopback, err := net.ParseCIDR("127.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	env := newAuthTestEnv(t, authEnvOptions{trustedProxies: []*net.IPNet{loopback}, rateLimit: true})
	ctx := context.Background()

	// Empty email skips the per-email limiter, isolating the per-IP dimension.
	login := func(forwardedFor string) error {
		req := connect.NewRequest(&portcullisv1.LoginRequest{Email: "", Password: "wrong-but-long-enough"})
		req.Header().Set("X-Forwarded-For", forwardedFor)
		_, err := env.client.Login(ctx, req)
		return err
	}

	// Two hops: the attacker can prepend a spoofed left entry, but the real client
	// (9.9.9.9) is the rightmost non-trusted address. Exhaust that bucket.
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
	// Same real client, different spoofed left entry: the key must come from the
	// right, so this stays throttled (a left-to-right bug would open a fresh bucket).
	if code := connect.CodeOf(login("2.2.2.2, 9.9.9.9")); code != connect.CodeResourceExhausted {
		t.Errorf("spoofed left entry opened a fresh bucket (key not taken right-to-left); got %v", code)
	}
	// A genuinely different real client (different rightmost) has its own bucket.
	if code := connect.CodeOf(login("2.2.2.2, 7.7.7.7")); code == connect.CodeResourceExhausted {
		t.Errorf("distinct forwarded client 7.7.7.7 shares a bucket; got %v", code)
	}
}

// The CSRF check's security rests on the session-bound HMAC, not the naive
// header==cookie equality (ADR-0006: "naive double-submit is bypassable").
// A forged value planted in BOTH the cookie and the header must be rejected —
// if the interceptor ever degrades to plain equality, this test catches it.
func TestCSRFRequiresSessionBoundHMAC(t *testing.T) {
	env := newAuthTestEnv(t, authEnvOptions{})
	ctx := context.Background()
	csrf := env.bootstrapAndLogin(t, "admin@example.com", "correct-horse-battery")
	sess := cookieFromJar(env.jar, env.serverURL, "__Host-portcullis_session")
	if sess == "" {
		t.Fatal("login did not set the session cookie")
	}

	// The jar-free client controls the Cookie header byte-for-byte.
	me := func(cookieVal, headerVal string) error {
		req := connect.NewRequest(&portcullisv1.MeRequest{})
		req.Header().Set("Cookie", "__Host-portcullis_session="+sess+"; __Host-portcullis_csrf="+cookieVal)
		req.Header().Set("X-CSRF-Token", headerVal)
		_, err := env.raw.Me(ctx, req)
		return err
	}

	// Control: the manual cookie plumbing accepts the genuine token, so the
	// rejection below can only come from the HMAC check.
	if err := me(csrf, csrf); err != nil {
		t.Fatalf("genuine CSRF token via manual cookies rejected: %v", err)
	}

	// Attack: a well-formed forged token (mac.nonce shape), identical in cookie
	// and header. Naive double-submit would accept it.
	enc := base64.RawURLEncoding
	forged := enc.EncodeToString([]byte("forged-mac-value")) + "." + enc.EncodeToString([]byte("forged-nonce"))
	if code := connect.CodeOf(me(forged, forged)); code != connect.CodePermissionDenied {
		t.Errorf("forged cookie==header CSRF token code = %v, want PermissionDenied (HMAC binding)", code)
	}
}

// One malformed sibling cookie — set by any other app on the same host, here a
// value with non-ASCII bytes — must not hide the session/CSRF cookies: parsing
// is lenient per PAIR, so only the bad pair is skipped and the user stays
// authenticated instead of being locked out until the stray cookie is cleared.
func TestSessionSurvivesMalformedSiblingCookie(t *testing.T) {
	env := newAuthTestEnv(t, authEnvOptions{})
	ctx := context.Background()
	csrf := env.bootstrapAndLogin(t, "admin@example.com", "correct-horse-battery")
	sess := cookieFromJar(env.jar, env.serverURL, "__Host-portcullis_session")
	if sess == "" {
		t.Fatal("login did not set the session cookie")
	}

	req := connect.NewRequest(&portcullisv1.MeRequest{})
	// "café" carries bytes >= 0x80 — invalid cookie-octets a browser still sends.
	req.Header().Set("Cookie",
		"promo=caf\xc3\xa9; __Host-portcullis_session="+sess+"; __Host-portcullis_csrf="+csrf)
	req.Header().Set("X-CSRF-Token", csrf)
	if _, err := env.raw.Me(ctx, req); err != nil {
		t.Errorf("a malformed sibling cookie locked the session out: %v", err)
	}
}

// Equivalent textual IP forms must collapse to ONE canonical form where the
// value is minted (ADR-0010), so the audit trail and the rate limiter stay
// correlatable: a login forwarded as "2001:0db8::1" is recorded as
// "2001:db8::1".
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

// flakyIdleStore fails ExtendSessionIdle / GetSessionByTokenHash on demand
// while delegating everything else to the real store — transient DB errors on
// the idle-slide write and on the session lookup of a valid session.
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

// An idle-slide infra failure must surface as retryable Unavailable, not
// Unauthenticated — a DB blip on a valid session must not read as a logout.
func TestSlideIdleInfraErrorIsUnavailable(t *testing.T) {
	var store *flakyIdleStore
	env := newAuthTestEnv(t, authEnvOptions{wrapStore: func(s *postgres.IdentityStore) auth.Repository {
		store = &flakyIdleStore{IdentityStore: s}
		return store
	}})
	ctx := context.Background()
	csrf := env.bootstrapAndLogin(t, "admin@example.com", "correct-horse-battery")

	// Age the stored idle deadline past the 1-min write throttle so the next
	// request actually attempts the slide, then break the write.
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

	// Once the store recovers, the same session works — nothing was revoked.
	store.failIdle.Store(false)
	if err := me(); err != nil {
		t.Errorf("Me after recovery = %v, want success", err)
	}

	// The same rule holds for a lookup failure: a DB blip while resolving a
	// valid session is Unavailable, not Unauthenticated.
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

	// GetConfig is public (no session, no CSRF) and routes the SPA: a fresh
	// instance needs bootstrap and (in this env) has no Google login.
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

	// After bootstrap the instance no longer advertises the first-run form.
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

	// Wrong password → generic Unauthenticated.
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

	// Me without the X-CSRF-Token header → PermissionDenied (double-submit fails).
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

	// After logout the session cookie is cleared, so Me is Unauthenticated.
	meReq2 := connect.NewRequest(&portcullisv1.MeRequest{})
	meReq2.Header().Set("X-CSRF-Token", csrf)
	if _, err := env.client.Me(ctx, meReq2); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("Me after logout code = %v, want Unauthenticated", connect.CodeOf(err))
	}

	// The whole scenario left an audit trail: bootstrap, the failed login, the
	// successful login, and the logout — each attributed with a source IP.
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
