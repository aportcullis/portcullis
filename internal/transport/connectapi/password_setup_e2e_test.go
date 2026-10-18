package connectapi_test

import (
	"context"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
)

func TestPasswordSetupAPICompletesWithoutSessionOrIdentityDisclosure(t *testing.T) {
	for _, password := range []string{"correct-horse-battery", "e\u0301quivalent-long-password", "long valid spaced password  ", "a different initial password"} {
		t.Run(password[:8], func(t *testing.T) {
			environment := newAdministrationAPIEnv(t)
			ctx := context.Background()
			created, err := environment.users.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateUserRequest{Email: "setup-api@example.com", DisplayName: "Setup member"}), environment.csrf))
			if err != nil {
				t.Fatal(err)
			}
			client := portcullisv1connect.NewAuthClient(&http.Client{Transport: environment.httpClient.Transport}, environment.serverURL)
			response, err := client.CompletePasswordSetup(ctx, connect.NewRequest(&portcullisv1.CompletePasswordSetupRequest{Token: created.Msg.SetupLink.Token, Password: password}))
			if err != nil {
				t.Fatal(err)
			}
			if response.Header().Get("Set-Cookie") != "" || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("setup started a session or allowed caching")
			}
			if _, err := client.Me(ctx, connect.NewRequest(&portcullisv1.MeRequest{})); connect.CodeOf(err) != connect.CodeUnauthenticated {
				t.Fatalf("automatic authentication: %v", err)
			}
			if _, err := client.Login(ctx, connect.NewRequest(&portcullisv1.LoginRequest{Email: "setup-api@example.com", Password: password})); err != nil {
				t.Fatalf("normal login: %v", err)
			}
		})
	}
}

func TestPasswordSetupAPIUniformlyRefusesUnusableTokens(t *testing.T) {
	for _, scenario := range []string{"unknown", "expired", "reused", "revoked", "disabled", "replaced"} {
		t.Run(scenario, func(t *testing.T) {
			environment := newAdministrationAPIEnv(t)
			ctx := context.Background()
			created, err := environment.users.Create(ctx, withCSRF(connect.NewRequest(&portcullisv1.CreateUserRequest{Email: "refused-setup@example.com", DisplayName: "Setup member"}), environment.csrf))
			if err != nil {
				t.Fatal(err)
			}
			client := portcullisv1connect.NewAuthClient(&http.Client{Transport: environment.httpClient.Transport}, environment.serverURL)
			token := created.Msg.SetupLink.Token
			switch scenario {
			case "unknown":
				token = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
			case "expired":
				if _, err := environment.pool.Exec(ctx, `update public.password_setup_tokens set created_at=clock_timestamp()-interval '2 days',expires_at=clock_timestamp()-interval '1 day' where user_id=$1`, created.Msg.User.Id); err != nil {
					t.Fatal(err)
				}
			case "reused":
				if _, err := client.CompletePasswordSetup(ctx, connect.NewRequest(&portcullisv1.CompletePasswordSetupRequest{Token: token, Password: "correct-horse-battery"})); err != nil {
					t.Fatal(err)
				}
			case "revoked":
				if _, err := environment.pool.Exec(ctx, `update public.password_setup_tokens set revoked_at=clock_timestamp() where user_id=$1`, created.Msg.User.Id); err != nil {
					t.Fatal(err)
				}
			case "disabled":
				if _, err := environment.users.Disable(ctx, withCSRF(connect.NewRequest(&portcullisv1.DisableUserRequest{Id: created.Msg.User.Id}), environment.csrf)); err != nil {
					t.Fatal(err)
				}
			case "replaced":
				if _, err := environment.users.IssueSetupLink(ctx, withCSRF(connect.NewRequest(&portcullisv1.IssueUserSetupLinkRequest{Id: created.Msg.User.Id}), environment.csrf)); err != nil {
					t.Fatal(err)
				}
			}
			response, err := client.CompletePasswordSetup(ctx, connect.NewRequest(&portcullisv1.CompletePasswordSetupRequest{Token: token, Password: "correct-horse-battery"}))
			var message string
			if connected, ok := err.(*connect.Error); ok {
				message = connected.Message()
			}
			if response != nil || connect.CodeOf(err) != connect.CodePermissionDenied || message != "invalid or expired password setup link" {
				t.Fatalf("nonuniform refusal: %v", err)
			}
		})
	}
}
