package connectapi_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect"
	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
	"github.com/aportcullis/portcullis/internal/app/auth"
	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/aportcullis/portcullis/internal/infra/dbtest"
	pg "github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/aportcullis/portcullis/internal/transport/connectapi"
)

type scriptedCapabilityMetadata struct {
	mutex    sync.Mutex
	snapshot authz.SessionCapabilities
	failure  error
}

// SessionInfo satisfies the compatibility port without supplying alternative authority.
func (*scriptedCapabilityMetadata) SessionInfo(context.Context, identity.User) ([]identity.Permission, string, error) {
	return nil, "", nil
}

// Capabilities supplies one atomic scripted metadata result through the real wire boundary.
func (provider *scriptedCapabilityMetadata) Capabilities(context.Context, identity.User) (authz.SessionCapabilities, error) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.snapshot, provider.failure
}

// replace simulates resolver recovery between completed RPCs.
func (provider *scriptedCapabilityMetadata) replace(snapshot authz.SessionCapabilities, failure error) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.snapshot = snapshot
	provider.failure = failure
}

func TestSessionMetadataFailureIsExplicitAndRecoverable(t *testing.T) {
	for _, scenario := range []string{"organization", "scoped", "conditional", "grantless", "snapshot unavailable", "binding overflow", "deadline", "cancelled", "private failure", "invalid revision"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			pool := dbtest.FreshPostgres(t)
			if err := pg.Migrate(ctx, pool); err != nil {
				t.Fatal(err)
			}
			store := pg.NewIdentityStore(pool)
			sessions, err := auth.New(store, crypto.NewArgon2Hasher(crypto.Argon2Params{Memory: 8 * 1024, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 16}, 4), crypto.NewCSRFProtector(loadTestKeyring(t)), pg.NewAuditStore(pool), auth.Config{})
			if err != nil {
				t.Fatal(err)
			}
			snapshot := authz.SessionCapabilities{Revision: 7, Capabilities: []identity.Permission{identity.PermissionRequestsList}}
			if scenario == "organization" {
				snapshot.Permissions = append([]identity.Permission(nil), snapshot.Capabilities...)
			}
			if scenario == "grantless" {
				snapshot.Capabilities = nil
			}
			if scenario == "scoped" || scenario == "conditional" {
				snapshot.Bindings = []authz.SessionBinding{{ID: "binding", RoleID: "role", RoleName: "Reader", ScopeType: "connection", ConnectionID: "connection", HasCondition: scenario == "conditional"}}
			}
			var failure error
			switch scenario {
			case "snapshot unavailable":
				failure = authz.ErrSnapshotUnavailable
			case "binding overflow":
				failure = authz.ErrBindingLimitExceeded
			case "deadline":
				failure = context.DeadlineExceeded
			case "cancelled":
				failure = context.Canceled
			case "private failure":
				failure = errors.New("private-metadata-marker")
			case "invalid revision":
				snapshot.Revision = 0
			}
			unavailable := failure != nil || scenario == "invalid revision"
			provider := &scriptedCapabilityMetadata{snapshot: snapshot, failure: failure}
			logs := &testLogBuffer{}
			service := connectapi.NewAuthService(sessions, provider).WithLogger(slog.New(slog.NewJSONHandler(logs, nil)))
			_, handler := portcullisv1connect.NewAuthHandler(service, connect.WithInterceptors(connectapi.NewClientIPInterceptor(nil), connectapi.NewAuthInterceptor(sessions), connectapi.NewActiveBoundaryInterceptor(grantlessAuthorization{}, sessions)))
			server := httptest.NewTLSServer(handler)
			defer server.Close()
			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			client := portcullisv1connect.NewAuthClient(&http.Client{Transport: server.Client().Transport, Jar: jar}, server.URL)
			if _, err := client.Bootstrap(ctx, connect.NewRequest(&portcullisv1.BootstrapRequest{SetupToken: mustIssueSetupToken(t, sessions), Email: "metadata@example.com", Password: "correct-horse-battery", DisplayName: "Metadata"})); err != nil {
				t.Fatal(err)
			}
			login, err := client.Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: "metadata@example.com", Password: "correct-horse-battery"}))
			if err != nil {
				t.Fatalf("metadata failure must not misreport established credentials: %v", err)
			}
			if login.Msg.GetAuthorizationUnavailable() != unavailable {
				t.Fatalf("Login silently converted metadata failure into grantless success: %+v", login.Msg)
			}
			if login.Header().Get("Cache-Control") != "no-store" || login.Msg.User == nil {
				t.Fatal("session-bearing response lost identity or no-store")
			}
			serverURL, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			csrf := csrfFromJar(jar, serverURL)
			if csrf == "" {
				t.Fatal("committed login lost its cookies")
			}
			me, err := client.Me(ctx, withCSRF(connect.NewRequest(&portcullisv1.MeRequest{}), csrf))
			if err != nil {
				t.Fatal(err)
			}
			if me.Msg.GetAuthorizationUnavailable() != unavailable || me.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Me failure status or cache contract lost: %+v", me.Msg)
			}
			if unavailable {
				if len(login.Msg.Permissions)+len(login.Msg.Capabilities)+len(login.Msg.Bindings) != 0 || login.Msg.AuthorizationRevision != 0 || login.Msg.RoleName != "" || len(me.Msg.Permissions)+len(me.Msg.Capabilities)+len(me.Msg.Bindings) != 0 || me.Msg.AuthorizationRevision != 0 || me.Msg.RoleName != "" {
					t.Fatal("partial or stale authorization data escaped a failed resolver")
				}
				if logs.String() == "" || strings.Contains(logs.String(), "private-metadata-marker") {
					t.Fatalf("missing safe diagnostic or private cause leaked: %s", logs.String())
				}
			} else if me.Msg.AuthorizationRevision != snapshot.Revision || len(me.Msg.Capabilities) != len(snapshot.Capabilities) || len(me.Msg.Permissions) != len(snapshot.Permissions) || len(me.Msg.Bindings) != len(snapshot.Bindings) {
				t.Fatalf("valid current metadata changed: %+v", me.Msg)
			}
			provider.replace(authz.SessionCapabilities{Revision: 8, Capabilities: []identity.Permission{identity.PermissionRequestsCreate}}, nil)
			recovered, err := client.Me(ctx, withCSRF(connect.NewRequest(&portcullisv1.MeRequest{}), csrf))
			if err != nil || recovered.Msg.GetAuthorizationUnavailable() || recovered.Msg.AuthorizationRevision != 8 {
				t.Fatalf("Me did not recover the established session: %+v %v", recovered, err)
			}
		})
	}
}
