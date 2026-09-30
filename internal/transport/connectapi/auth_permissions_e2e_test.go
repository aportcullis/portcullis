package connectapi_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"

	"connectrpc.com/connect"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
	"github.com/aportcullis/portcullis/internal/app/auth"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	"github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/aportcullis/portcullis/internal/transport/connectapi"
)

type failingLister struct{}

func (failingLister) SessionInfo(context.Context, identity.User) ([]identity.Permission, string, error) {
	return nil, "", errors.New("authz store unavailable")
}

func TestLoginAndMeSurvivePermissionResolverFailure(t *testing.T) {
	pool := dbtest.FreshPostgres(t)
	ctx := context.Background()
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := postgres.NewIdentityStore(pool)
	weak := crypto.Argon2Params{Memory: 8 * 1024, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 16}
	authSvc, err := auth.New(store, crypto.NewArgon2Hasher(weak, 4), crypto.NewCSRFProtector(loadTestKeyring(t)), postgres.NewAuditStore(pool), auth.Config{})
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}

	chain := connect.WithInterceptors(
		connectapi.NewClientIPInterceptor(nil),
		connectapi.NewAuthInterceptor(authSvc),
	)
	mux := http.NewServeMux()
	authPath, authHandler := portcullisv1connect.NewAuthHandler(connectapi.NewAuthService(authSvc, failingLister{}), chain)
	mux.Handle(authPath, authHandler)
	ts := httptest.NewTLSServer(mux)
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Transport: ts.Client().Transport, Jar: jar}
	authC := portcullisv1connect.NewAuthClient(hc, ts.URL)

	const email, password = "admin@example.com", "correct-horse-battery"
	if _, err := authC.Bootstrap(ctx, connect.NewRequest(&portcullisv1.BootstrapRequest{Email: email, Password: password, DisplayName: "Admin"})); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	login, err := authC.Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: email, Password: password}))
	if err != nil {
		t.Fatalf("Login must succeed despite the permission resolver failing: %v", err)
	}
	if got := login.Msg.GetPermissions(); len(got) != 0 {
		t.Errorf("Login.permissions = %v, want empty on resolver failure", got)
	}

	serverURL, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	csrf := csrfFromJar(jar, serverURL)
	if csrf == "" {
		t.Fatal("login did not deliver the CSRF cookie — cookies must be set even when permissions fail")
	}
	me, err := authC.Me(ctx, withCSRF(connect.NewRequest(&portcullisv1.MeRequest{}), csrf))
	if err != nil {
		t.Fatalf("Me must succeed despite the permission resolver failing: %v", err)
	}
	if got := me.Msg.GetPermissions(); len(got) != 0 {
		t.Errorf("Me.permissions = %v, want empty on resolver failure", got)
	}
}
