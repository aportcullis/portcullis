package connectapi_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
)

func bootstrapRequestWithToken(setupToken, email string) *connect.Request[portcullisv1.BootstrapRequest] {
	return connect.NewRequest(&portcullisv1.BootstrapRequest{SetupToken: setupToken, Email: email, Password: "correct-horse-battery", DisplayName: "Admin"})
}

func needsBootstrap(t *testing.T, env *authTestEnv) bool {
	t.Helper()
	config, err := env.raw.GetConfig(context.Background(), connect.NewRequest(&portcullisv1.GetConfigRequest{}))
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	return config.Msg.GetNeedsBootstrap()
}

func TestBootstrapRPCAdmitsTheSetupTokenHolderE2E(t *testing.T) {
	ctx := context.Background()

	t.Run("issued token claims the fresh installation", func(t *testing.T) {
		env := newAuthTestEnv(t, authEnvOptions{})
		if !needsBootstrap(t, env) {
			t.Fatal("fresh installation must need bootstrap")
		}
		response, err := env.raw.Bootstrap(ctx, bootstrapRequestWithToken(mustIssueSetupToken(t, env.setupTokens), "admin@example.com"))
		if err != nil || response.Msg.GetUser().GetEmail() != "admin@example.com" {
			t.Fatalf("Bootstrap = %v, %v; want the admin", response, err)
		}
		if needsBootstrap(t, env) {
			t.Error("GetConfig still needs bootstrap after a token claim")
		}
	})

	t.Run("pasted token with a trailing newline", func(t *testing.T) {
		env := newAuthTestEnv(t, authEnvOptions{})
		if _, err := env.raw.Bootstrap(ctx, bootstrapRequestWithToken(mustIssueSetupToken(t, env.setupTokens)+"\n", "admin@example.com")); err != nil {
			t.Fatalf("Bootstrap(pasted) = %v", err)
		}
	})

	t.Run("token from the latest restart", func(t *testing.T) {
		env := newAuthTestEnv(t, authEnvOptions{})
		mustIssueSetupToken(t, env.setupTokens)
		latest := mustIssueSetupToken(t, env.setupTokens)
		if _, err := env.raw.Bootstrap(ctx, bootstrapRequestWithToken(latest, "admin@example.com")); err != nil {
			t.Fatalf("Bootstrap(latest) = %v", err)
		}
	})

	t.Run("claimed admin signs in", func(t *testing.T) {
		env := newAuthTestEnv(t, authEnvOptions{})
		if csrf := env.bootstrapAndLogin(t, "admin@example.com", "correct-horse-battery"); csrf == "" {
			t.Fatal("login after token bootstrap issued no CSRF token")
		}
	})
}

func TestBootstrapRPCRefusesInvalidSetupTokensE2E(t *testing.T) {
	ctx := context.Background()

	refusals := []struct {
		name  string
		token func(t *testing.T, env *authTestEnv) string
	}{
		{name: "missing token", token: func(t *testing.T, env *authTestEnv) string {
			mustIssueSetupToken(t, env.setupTokens)
			return ""
		}},
		{name: "guessed token", token: func(t *testing.T, env *authTestEnv) string {
			mustIssueSetupToken(t, env.setupTokens)
			return "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		}},
		{name: "token replaced by a restart", token: func(t *testing.T, env *authTestEnv) string {
			replaced := mustIssueSetupToken(t, env.setupTokens)
			mustIssueSetupToken(t, env.setupTokens)
			return replaced
		}},
		{name: "expired token", token: func(t *testing.T, env *authTestEnv) string {
			token := mustIssueSetupToken(t, env.setupTokens)
			if _, err := env.pool.Exec(context.Background(), `update setup_tokens set created_at = now() - interval '2 days', expires_at = now() - interval '1 second'`); err != nil {
				t.Fatalf("expire setup token: %v", err)
			}
			return token
		}},
	}
	for _, refusal := range refusals {
		t.Run(refusal.name, func(t *testing.T) {
			env := newAuthTestEnv(t, authEnvOptions{})
			_, err := env.raw.Bootstrap(ctx, bootstrapRequestWithToken(refusal.token(t, env), "attacker@example.com"))
			if connect.CodeOf(err) != connect.CodePermissionDenied {
				t.Fatalf("Bootstrap code = %v (%v), want PermissionDenied", connect.CodeOf(err), err)
			}
			var connectErr *connect.Error
			if !errors.As(err, &connectErr) || connectErr.Message() != "invalid setup token" {
				t.Errorf("Bootstrap error = %v, want the uniform invalid setup token message", err)
			}
			if !needsBootstrap(t, env) {
				t.Error("refused claim bootstrapped the installation")
			}
			var failures int
			if err := env.pool.QueryRow(ctx, `select count(*) from audit_events where action = 'AUTH_BOOTSTRAP' and outcome = 'FAILED'`).Scan(&failures); err != nil {
				t.Fatalf("count failed bootstrap events: %v", err)
			}
			if failures != 1 {
				t.Errorf("failed AUTH_BOOTSTRAP events = %d, want 1", failures)
			}
		})
	}

	t.Run("replayed token after the claim", func(t *testing.T) {
		env := newAuthTestEnv(t, authEnvOptions{})
		token := mustIssueSetupToken(t, env.setupTokens)
		if _, err := env.raw.Bootstrap(ctx, bootstrapRequestWithToken(token, "admin@example.com")); err != nil {
			t.Fatalf("first Bootstrap = %v", err)
		}
		if _, err := env.raw.Bootstrap(ctx, bootstrapRequestWithToken(token, "attacker@example.com")); connect.CodeOf(err) != connect.CodeFailedPrecondition {
			t.Fatalf("replayed Bootstrap code = %v, want FailedPrecondition", connect.CodeOf(err))
		}
	})

	t.Run("concurrent claims with one token admit exactly one admin", func(t *testing.T) {
		env := newAuthTestEnv(t, authEnvOptions{})
		token := mustIssueSetupToken(t, env.setupTokens)
		claimCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		const claimants = 6
		codes := make([]connect.Code, claimants)
		successes := make([]bool, claimants)
		var wait sync.WaitGroup
		for idx := range claimants {
			wait.Go(func() {
				_, err := env.raw.Bootstrap(claimCtx, bootstrapRequestWithToken(token, fmt.Sprintf("racer-%d@example.com", idx)))
				successes[idx] = err == nil
				codes[idx] = connect.CodeOf(err)
			})
		}
		wait.Wait()
		admitted := 0
		for idx := range claimants {
			switch {
			case successes[idx]:
				admitted++
			case codes[idx] == connect.CodeFailedPrecondition, codes[idx] == connect.CodePermissionDenied:
			default:
				t.Errorf("claimant %d code = %v, want FailedPrecondition or PermissionDenied", idx, codes[idx])
			}
		}
		if admitted != 1 {
			t.Errorf("admitted claimants = %d, want exactly 1", admitted)
		}
		var users int
		if err := env.pool.QueryRow(ctx, `select count(*) from users`).Scan(&users); err != nil || users != 1 {
			t.Errorf("users = %d (%v), want 1", users, err)
		}
	})
}
