package auth_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

const (
	setupAdminEmail    = "admin@example.com"
	setupAdminPassword = "hunter2-secretz"
	setupAdminName     = "Admin"
)

func TestSetupTokenAdmitsTheHolderToBootstrap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("issued token creates the first admin", func(t *testing.T) {
		t.Parallel()
		svc := newService(t, newFake())
		token, err := svc.IssueSetupToken(ctx)
		if err != nil {
			t.Fatalf("IssueSetupToken: %v", err)
		}
		if len(token) < 43 {
			t.Fatalf("setup token length = %d, want at least 43 base64url characters (256 bits)", len(token))
		}
		user, err := svc.BootstrapWithSetupToken(ctx, token, setupAdminEmail, setupAdminPassword, setupAdminName)
		if err != nil || user.Email != setupAdminEmail {
			t.Fatalf("BootstrapWithSetupToken = %+v, %v; want the admin", user, err)
		}
	})

	t.Run("reissued token replaces its predecessor", func(t *testing.T) {
		t.Parallel()
		svc := newService(t, newFake())
		if _, err := svc.IssueSetupToken(ctx); err != nil {
			t.Fatalf("first IssueSetupToken: %v", err)
		}
		latest, err := svc.IssueSetupToken(ctx)
		if err != nil {
			t.Fatalf("second IssueSetupToken: %v", err)
		}
		if _, err := svc.BootstrapWithSetupToken(ctx, latest, setupAdminEmail, setupAdminPassword, setupAdminName); err != nil {
			t.Fatalf("BootstrapWithSetupToken(latest) = %v", err)
		}
	})

	t.Run("pasted token with surrounding whitespace", func(t *testing.T) {
		t.Parallel()
		svc := newService(t, newFake())
		token, err := svc.IssueSetupToken(ctx)
		if err != nil {
			t.Fatalf("IssueSetupToken: %v", err)
		}
		if _, err := svc.BootstrapWithSetupToken(ctx, " "+token+"\n", setupAdminEmail, setupAdminPassword, setupAdminName); err != nil {
			t.Fatalf("BootstrapWithSetupToken(pasted) = %v", err)
		}
	})

	t.Run("token redeemed just before expiry", func(t *testing.T) {
		t.Parallel()
		repo := newFake()
		issuedAt := time.Now()
		repo.clock = func() time.Time { return issuedAt }
		svc := newService(t, repo)
		token, err := svc.IssueSetupToken(ctx)
		if err != nil {
			t.Fatalf("IssueSetupToken: %v", err)
		}
		repo.clock = func() time.Time { return issuedAt.Add(24*time.Hour - time.Second) }
		if _, err := svc.BootstrapWithSetupToken(ctx, token, setupAdminEmail, setupAdminPassword, setupAdminName); err != nil {
			t.Fatalf("BootstrapWithSetupToken before expiry = %v", err)
		}
	})

	t.Run("operator provisioning needs no token", func(t *testing.T) {
		t.Parallel()
		svc := newService(t, newFake())
		if _, err := svc.ProvisionBootstrapAdmin(ctx, setupAdminEmail, setupAdminPassword, setupAdminName); err != nil {
			t.Fatalf("ProvisionBootstrapAdmin = %v", err)
		}
	})
}

func TestSetupTokenRefusesEveryOtherClaim(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	refusals := []struct {
		name         string
		claimToken   func(t *testing.T, issued, predecessor string) string
		advanceClock time.Duration
	}{
		{name: "missing token", claimToken: func(*testing.T, string, string) string { return "" }},
		{name: "wrong token", claimToken: func(*testing.T, string, string) string { return "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" }},
		{name: "rotated-away token", claimToken: func(_ *testing.T, _, predecessor string) string { return predecessor }},
		{name: "oversized token", claimToken: func(_ *testing.T, issued, _ string) string { return issued + strings.Repeat("A", 512) }},
		{name: "expired token", claimToken: func(_ *testing.T, issued, _ string) string { return issued }, advanceClock: 24*time.Hour + time.Second},
	}
	for _, refusal := range refusals {
		t.Run(refusal.name, func(t *testing.T) {
			t.Parallel()
			repo := newFake()
			issuedAt := time.Now()
			repo.clock = func() time.Time { return issuedAt }
			recorder := &capturingRecorder{}
			svc := newServiceWithRecorder(t, repo, recorder)
			predecessor, err := svc.IssueSetupToken(ctx)
			if err != nil {
				t.Fatalf("IssueSetupToken: %v", err)
			}
			issued, err := svc.IssueSetupToken(ctx)
			if err != nil {
				t.Fatalf("IssueSetupToken: %v", err)
			}
			repo.clock = func() time.Time { return issuedAt.Add(refusal.advanceClock) }
			_, err = svc.BootstrapWithSetupToken(ctx, refusal.claimToken(t, issued, predecessor), setupAdminEmail, setupAdminPassword, setupAdminName)
			if !errors.Is(err, identity.ErrSetupTokenInvalid) {
				t.Fatalf("BootstrapWithSetupToken = %v, want ErrSetupTokenInvalid", err)
			}
			if count, _ := repo.CountUsers(ctx); count != 0 {
				t.Errorf("refused claim created %d users", count)
			}
			failures := 0
			for _, event := range recorder.all() {
				if event.Action == audit.ActionAuthBootstrap && event.Outcome == audit.OutcomeFailed {
					failures++
				}
			}
			if failures != 1 {
				t.Errorf("failed AUTH_BOOTSTRAP events = %d, want 1", failures)
			}
		})
	}

	t.Run("replayed token after a successful bootstrap", func(t *testing.T) {
		t.Parallel()
		svc := newService(t, newFake())
		token, err := svc.IssueSetupToken(ctx)
		if err != nil {
			t.Fatalf("IssueSetupToken: %v", err)
		}
		if _, err := svc.BootstrapWithSetupToken(ctx, token, setupAdminEmail, setupAdminPassword, setupAdminName); err != nil {
			t.Fatalf("first BootstrapWithSetupToken = %v", err)
		}
		if _, err := svc.BootstrapWithSetupToken(ctx, token, "second@example.com", setupAdminPassword, setupAdminName); !errors.Is(err, identity.ErrAlreadyBootstrapped) {
			t.Fatalf("replayed BootstrapWithSetupToken = %v, want ErrAlreadyBootstrapped", err)
		}
	})

	t.Run("no token is issued once an admin exists", func(t *testing.T) {
		t.Parallel()
		svc := newService(t, newFake())
		if _, err := svc.ProvisionBootstrapAdmin(ctx, setupAdminEmail, setupAdminPassword, setupAdminName); err != nil {
			t.Fatalf("ProvisionBootstrapAdmin = %v", err)
		}
		if token, err := svc.IssueSetupToken(ctx); !errors.Is(err, identity.ErrAlreadyBootstrapped) || token != "" {
			t.Fatalf("IssueSetupToken after bootstrap = %q, %v; want no token and ErrAlreadyBootstrapped", token, err)
		}
	})
}
