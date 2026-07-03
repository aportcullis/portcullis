package connectapi_test

import (
	"context"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"

	"connectrpc.com/connect"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
	"github.com/aportcullis/portcullis/internal/app/auth"
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

func csrfFromJar(jar http.CookieJar, u *url.URL) string {
	for _, c := range jar.Cookies(u) {
		if c.Name == "__Host-portcullis_csrf" {
			return c.Value
		}
	}
	return ""
}

func TestLoginRateLimited(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	weak := crypto.Argon2Params{Memory: 8 * 1024, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 16}
	svc, err := auth.New(postgres.NewIdentityStore(pool), crypto.NewArgon2Hasher(weak, 4), crypto.NewCSRFProtector(loadTestKeyring(t)), postgres.NewAuditStore(pool), auth.Config{})
	if err != nil {
		t.Fatal(err)
	}

	path, handler := portcullisv1connect.NewAuthHandler(
		connectapi.NewAuthService(svc),
		connect.WithInterceptors(connectapi.NewClientIPInterceptor(nil), connectapi.NewRateLimitInterceptor(), connectapi.NewAuthInterceptor(svc)),
	)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	ts := httptest.NewTLSServer(mux)
	defer ts.Close()
	client := portcullisv1connect.NewAuthClient(ts.Client(), ts.URL)

	// Fire more than the burst of wrong-password logins from one client: the first
	// burst is rejected on credentials (Unauthenticated), then the limiter kicks in
	// with ResourceExhausted — before any hashing on the throttled requests.
	var throttled bool
	for i := 0; i < 40; i++ {
		_, err := client.Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: "nobody@example.com", Password: "wrong-but-long-enough"}))
		if connect.CodeOf(err) == connect.CodeResourceExhausted {
			throttled = true
			break
		}
	}
	if !throttled {
		t.Error("repeated logins from one client were never rate-limited")
	}
}

func TestLoginRateLimitTrustsProxyForwardedFor(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	weak := crypto.Argon2Params{Memory: 8 * 1024, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 16}
	svc, err := auth.New(postgres.NewIdentityStore(pool), crypto.NewArgon2Hasher(weak, 4), crypto.NewCSRFProtector(loadTestKeyring(t)), postgres.NewAuditStore(pool), auth.Config{})
	if err != nil {
		t.Fatal(err)
	}

	// Trust the loopback proxy (the httptest peer) so X-Forwarded-For is honored.
	_, loopback, err := net.ParseCIDR("127.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	path, handler := portcullisv1connect.NewAuthHandler(
		connectapi.NewAuthService(svc),
		connect.WithInterceptors(connectapi.NewClientIPInterceptor([]*net.IPNet{loopback}), connectapi.NewRateLimitInterceptor(), connectapi.NewAuthInterceptor(svc)),
	)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	ts := httptest.NewTLSServer(mux)
	defer ts.Close()
	client := portcullisv1connect.NewAuthClient(ts.Client(), ts.URL)

	// Empty email skips the per-email limiter, isolating the per-IP dimension.
	login := func(forwardedFor string) error {
		req := connect.NewRequest(&portcullisv1.LoginRequest{Email: "", Password: "wrong-but-long-enough"})
		req.Header().Set("X-Forwarded-For", forwardedFor)
		_, err := client.Login(ctx, req)
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

func TestAuthE2E(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	weak := crypto.Argon2Params{Memory: 8 * 1024, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 16}
	svc, err := auth.New(postgres.NewIdentityStore(pool), crypto.NewArgon2Hasher(weak, 4), crypto.NewCSRFProtector(loadTestKeyring(t)), postgres.NewAuditStore(pool), auth.Config{})
	if err != nil {
		t.Fatal(err)
	}

	path, handler := portcullisv1connect.NewAuthHandler(
		connectapi.NewAuthService(svc),
		connect.WithInterceptors(connectapi.NewClientIPInterceptor(nil), connectapi.NewAuthInterceptor(svc)),
	)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	// TLS server so the cookie jar sends the Secure __Host- cookies back.
	ts := httptest.NewTLSServer(mux)
	defer ts.Close()

	jar, _ := cookiejar.New(nil)
	httpClient := ts.Client()
	httpClient.Jar = jar
	serverURL, _ := url.Parse(ts.URL)
	client := portcullisv1connect.NewAuthClient(httpClient, ts.URL)

	const email, password = "admin@example.com", "correct-horse-battery"

	if _, err := client.Bootstrap(ctx, connect.NewRequest(&portcullisv1.BootstrapRequest{Email: email, Password: password, DisplayName: "Admin"})); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if _, err := client.Bootstrap(ctx, connect.NewRequest(&portcullisv1.BootstrapRequest{Email: "x@y.z", Password: "another-valid-password", DisplayName: "X"})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("second Bootstrap code = %v, want FailedPrecondition", connect.CodeOf(err))
	}

	// Wrong password → generic Unauthenticated.
	if _, err := client.Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: email, Password: "wrong"})); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("bad login code = %v, want Unauthenticated", connect.CodeOf(err))
	}

	loginResp, err := client.Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: email, Password: password}))
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if loginResp.Msg.GetUser().GetEmail() != email {
		t.Errorf("login user = %v", loginResp.Msg.GetUser())
	}
	csrf := csrfFromJar(jar, serverURL)
	if csrf == "" {
		t.Fatal("Login did not set the CSRF cookie")
	}

	// Me without the X-CSRF-Token header → PermissionDenied (double-submit fails).
	if _, err := client.Me(ctx, connect.NewRequest(&portcullisv1.MeRequest{})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("Me without CSRF code = %v, want PermissionDenied", connect.CodeOf(err))
	}

	meReq := connect.NewRequest(&portcullisv1.MeRequest{})
	meReq.Header().Set("X-CSRF-Token", csrf)
	meResp, err := client.Me(ctx, meReq)
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if meResp.Msg.GetUser().GetEmail() != email {
		t.Errorf("Me user = %v", meResp.Msg.GetUser())
	}

	logoutReq := connect.NewRequest(&portcullisv1.LogoutRequest{})
	logoutReq.Header().Set("X-CSRF-Token", csrf)
	if _, err := client.Logout(ctx, logoutReq); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	// After logout the session cookie is cleared, so Me is Unauthenticated.
	meReq2 := connect.NewRequest(&portcullisv1.MeRequest{})
	meReq2.Header().Set("X-CSRF-Token", csrf)
	if _, err := client.Me(ctx, meReq2); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("Me after logout code = %v, want Unauthenticated", connect.CodeOf(err))
	}

	// The whole scenario left an audit trail: bootstrap, the failed login, the
	// successful login, and the logout — each attributed with a source IP.
	rows, err := pool.Query(ctx, `select action, outcome, metadata->>'source_ip' from audit_events order by occurred_at, action`)
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
