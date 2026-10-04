package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/auth"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

const googleIssuer = "https://accounts.google.com"

type fakeProvider struct {
	authURLCalls int
	lastState    string
	lastNonce    string
	lastVerifier string

	exchanges    int
	exchangeCode string
	exchangeVerf string
	claims       identity.OIDCClaims
	exchangeErr  error
}

func (p *fakeProvider) AuthCodeURL(state, nonce, verifier string) string {
	p.authURLCalls++
	p.lastState, p.lastNonce, p.lastVerifier = state, nonce, verifier
	return googleIssuer + "/authorize?state=" + state
}

func (p *fakeProvider) Exchange(_ context.Context, code, verifier string) (identity.OIDCClaims, error) {
	p.exchanges++
	p.exchangeCode, p.exchangeVerf = code, verifier
	if p.exchangeErr != nil {
		return identity.OIDCClaims{}, p.exchangeErr
	}
	return p.claims, nil
}

// linkRaceRepo simulates the concurrent first-login race: the subject lookup misses, but by the time we link, another user has claimed the identity.
type linkRaceRepo struct{ *fakeRepo }

func (r linkRaceRepo) LinkIdentityAndRotateSession(context.Context, identity.OIDCIdentity, identity.Session, []byte, audit.Event) (identity.Session, error) {
	return identity.Session{}, identity.ErrIdentityLinkedToAnotherUser
}

func newOIDCService(t *testing.T, repo auth.Repository, rec auth.AuditRecorder, p auth.OIDCProvider) *auth.Service {
	t.Helper()
	svc, err := auth.New(repo, fakeHasher{}, fakeCSRF{}, rec, auth.Config{})
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	return svc.WithOIDCProvider(p)
}

func bootstrapUser(t *testing.T, svc *auth.Service) identity.User {
	t.Helper()
	u, err := svc.ProvisionBootstrapAdmin(context.Background(), "admin@example.com", "hunter2-secretz", "Admin")
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	return u
}

func startFlow(t *testing.T, svc *auth.Service, p *fakeProvider) auth.OIDCPending {
	t.Helper()
	_, pending, err := svc.StartGoogleLogin(context.Background())
	if err != nil {
		t.Fatalf("StartGoogleLogin: %v", err)
	}
	p.claims.Nonce = pending.Nonce
	return pending
}

func authoritativeClaims(email string) identity.OIDCClaims {
	return identity.OIDCClaims{Issuer: googleIssuer, Subject: "sub-1", Email: email, EmailVerified: true, EmailAuthoritative: true}
}

func TestStartGoogleLoginMintsPendingState(t *testing.T) {
	t.Parallel()
	p := &fakeProvider{}
	base := time.Now()
	svc := newOIDCService(t, newFake(), &capturingRecorder{}, p).WithClock(func() time.Time { return base })

	url, pending, err := svc.StartGoogleLogin(context.Background())
	if err != nil {
		t.Fatalf("StartGoogleLogin: %v", err)
	}
	if url == "" || p.authURLCalls != 1 {
		t.Errorf("auth URL = %q (provider called %d times), want the provider's URL exactly once", url, p.authURLCalls)
	}
	// state, nonce, and verifier are three independent secrets — sharing any two would let one leaked value stand in for another.
	if pending.State == "" || pending.Nonce == "" || pending.Verifier == "" {
		t.Fatalf("pending has empty fields: %+v", pending)
	}
	if pending.State == pending.Nonce || pending.State == pending.Verifier || pending.Nonce == pending.Verifier {
		t.Errorf("state/nonce/verifier must be distinct: %+v", pending)
	}
	// The provider must be handed exactly the minted values (the URL carries the S256 challenge derived from this verifier).
	if p.lastState != pending.State || p.lastNonce != pending.Nonce || p.lastVerifier != pending.Verifier {
		t.Errorf("provider saw (%q,%q,%q), want the minted pending values", p.lastState, p.lastNonce, p.lastVerifier)
	}

	if want := base.Add(10 * time.Minute); !pending.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", pending.ExpiresAt, want)
	}

	_, second, err := svc.StartGoogleLogin(context.Background())
	if err != nil {
		t.Fatalf("second StartGoogleLogin: %v", err)
	}
	if second.State == pending.State || second.Nonce == pending.Nonce || second.Verifier == pending.Verifier {
		t.Errorf("second flow reused a secret from the first")
	}
}

func TestGoogleLoginWithoutProviderRefused(t *testing.T) {
	t.Parallel()
	svc := newService(t, newFake())
	if _, _, err := svc.StartGoogleLogin(context.Background()); !errors.Is(err, auth.ErrOIDCNotConfigured) {
		t.Errorf("StartGoogleLogin = %v, want ErrOIDCNotConfigured", err)
	}
	if _, err := svc.LoginWithGoogle(context.Background(), "s", "c", auth.OIDCPending{}); !errors.Is(err, auth.ErrOIDCNotConfigured) {
		t.Errorf("LoginWithGoogle = %v, want ErrOIDCNotConfigured", err)
	}
}

func TestGoogleLoginLinkedSubjectSignsIn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := newFake()
	p := &fakeProvider{}
	svc := newOIDCService(t, repo, &capturingRecorder{}, p)
	u := bootstrapUser(t, svc)
	if err := repo.linkIdentity(identity.OIDCIdentity{UserID: u.ID, Issuer: googleIssuer, Subject: "sub-1"}); err != nil {
		t.Fatal(err)
	}
	p.claims = authoritativeClaims("admin@example.com")
	pending := startFlow(t, svc, p)

	sess, err := svc.LoginWithGoogle(ctx, pending.State, "code-1", pending)
	if err != nil {
		t.Fatalf("LoginWithGoogle: %v", err)
	}
	if sess.Token == "" || sess.CSRF == "" || sess.User.ID != u.ID {
		t.Errorf("session = %+v, want tokens for %v", sess, u.ID)
	}
	if p.exchangeCode != "code-1" || p.exchangeVerf != pending.Verifier {
		t.Errorf("exchange saw (code=%q, verifier=%q), want the callback code + pending verifier", p.exchangeCode, p.exchangeVerf)
	}

	if _, _, err := svc.Authenticate(ctx, sess.Token); err != nil {
		t.Errorf("Authenticate after Google login: %v", err)
	}

	last := repo.txEvents[len(repo.txEvents)-1]
	if last.Action != audit.ActionAuthLogin || last.Outcome != audit.OutcomeSucceeded {
		t.Fatalf("last tx event = %s/%s, want AUTH_LOGIN/SUCCEEDED", last.Action, last.Outcome)
	}
	if got := last.Metadata["method"]; got != "google" {
		t.Errorf("success event metadata method = %v, want google", got)
	}
	if a := last.ActorUserID; a == nil || *a != u.ID {
		t.Errorf("success event actor = %v, want %v", a, u.ID)
	}
}

func TestGoogleFirstLoginRollsBackIdentityLinkWhenSessionCommitFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := newFake()
	repo.failOIDCComplete = true
	p := &fakeProvider{claims: authoritativeClaims("admin@example.com")}
	svc := newOIDCService(t, repo, &capturingRecorder{}, p)
	u := bootstrapUser(t, svc)
	pending := startFlow(t, svc, p)

	if _, err := svc.LoginWithGoogle(ctx, pending.State, "code-1", pending); err == nil {
		t.Fatal("LoginWithGoogle succeeded despite the atomic OIDC completion failure")
	}
	if _, err := repo.FindUserBySubject(ctx, googleIssuer, "sub-1"); !errors.Is(err, identity.ErrNoLinkedAccount) {
		t.Errorf("first-login identity link survived failed session commit: %v", err)
	}
	for _, e := range repo.txEvents {
		if e.Action == audit.ActionAuthLogin && e.Outcome == audit.OutcomeSucceeded && e.ActorUserID != nil && *e.ActorUserID == u.ID {
			t.Fatal("successful AUTH_LOGIN audit event survived failed atomic completion")
		}
	}
}

func TestGoogleLoginIgnoresLockout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := newFake()
	p := &fakeProvider{}
	svc := newOIDCService(t, repo, &capturingRecorder{}, p)
	u := bootstrapUser(t, svc)
	if err := repo.linkIdentity(identity.OIDCIdentity{UserID: u.ID, Issuer: googleIssuer, Subject: "sub-1"}); err != nil {
		t.Fatal(err)
	}
	lockedUntil := time.Now().Add(10 * time.Minute)
	repo.backoff[u.ID] = identity.LoginBackoff{FailureCount: 7, LockedUntil: &lockedUntil}

	p.claims = authoritativeClaims("admin@example.com")
	pending := startFlow(t, svc, p)
	sess, err := svc.LoginWithGoogle(ctx, pending.State, "code-1", pending)
	if err != nil {
		t.Fatalf("LoginWithGoogle while password-locked = %v, want success", err)
	}
	if sess.Token == "" {
		t.Error("no session issued")
	}
	if repo.failureWrites != 0 {
		t.Errorf("Google login touched the password failure counter (%d writes)", repo.failureWrites)
	}
	if b := repo.backoff[u.ID]; b.FailureCount != 7 || b.LockedUntil == nil {
		t.Errorf("Google login mutated the password backoff state: %+v", b)
	}
}

func TestGoogleLoginLinksAuthoritativeEmailOnFirstLogin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := newFake()
	p := &fakeProvider{}
	svc := newOIDCService(t, repo, &capturingRecorder{}, p)
	u := bootstrapUser(t, svc)
	// The provider asserts a differently-cased email — it must match the stored (normalized) address.
	p.claims = authoritativeClaims("ADMIN@Example.com")
	pending := startFlow(t, svc, p)

	sess, err := svc.LoginWithGoogle(ctx, pending.State, "code-1", pending)
	if err != nil {
		t.Fatalf("LoginWithGoogle: %v", err)
	}
	if sess.User.ID != u.ID {
		t.Errorf("linked to %v, want %v", sess.User.ID, u.ID)
	}
	if got := repo.oidc[googleIssuer+"|sub-1"]; got != u.ID {
		t.Errorf("identity not linked: oidc[%q] = %v, want %v", googleIssuer+"|sub-1", got, u.ID)
	}

	p.claims = authoritativeClaims("renamed@example.com")
	p.claims.EmailAuthoritative = false
	pending = startFlow(t, svc, p)
	sess, err = svc.LoginWithGoogle(ctx, pending.State, "code-2", pending)
	if err != nil {
		t.Fatalf("second LoginWithGoogle: %v", err)
	}
	if sess.User.ID != u.ID {
		t.Errorf("subject login resolved %v, want %v", sess.User.ID, u.ID)
	}
}

func TestGoogleLoginRejectsUnverifiedEmailWithoutLinking(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := newFake()
	p := &fakeProvider{}
	svc := newOIDCService(t, repo, &capturingRecorder{}, p)
	bootstrapUser(t, svc)
	p.claims = authoritativeClaims("admin@example.com")
	p.claims.EmailVerified = false
	pending := startFlow(t, svc, p)

	if _, err := svc.LoginWithGoogle(ctx, pending.State, "code-1", pending); !errors.Is(err, auth.ErrOIDCEmailUnverified) {
		t.Errorf("unverified email = %v, want ErrOIDCEmailUnverified", err)
	}
	if len(repo.oidc) != 0 {
		t.Errorf("unverified email must not link: %v", repo.oidc)
	}
}

func TestGoogleLoginRejectsUnknownEmail(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := newFake()
	p := &fakeProvider{}
	svc := newOIDCService(t, repo, &capturingRecorder{}, p)
	bootstrapUser(t, svc)
	p.claims = authoritativeClaims("stranger@example.com")
	pending := startFlow(t, svc, p)

	if _, err := svc.LoginWithGoogle(ctx, pending.State, "code-1", pending); !errors.Is(err, identity.ErrNoLinkedAccount) {
		t.Errorf("unknown email = %v, want ErrNoLinkedAccount", err)
	}
	if len(repo.users) != 1 {
		t.Errorf("login created a user: %d users, want 1", len(repo.users))
	}
	if len(repo.oidc) != 0 {
		t.Errorf("unknown email must not link: %v", repo.oidc)
	}
}

func TestGoogleLoginRejectsDisabledUser(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	disable := func(repo *fakeRepo, email string) {
		id := repo.emails[email]
		u := repo.users[id]
		u.Status = identity.StatusDisabled
		repo.users[id] = u
	}

	t.Run("linked subject", func(t *testing.T) {
		t.Parallel()
		repo := newFake()
		p := &fakeProvider{}
		svc := newOIDCService(t, repo, &capturingRecorder{}, p)
		u := bootstrapUser(t, svc)
		if err := repo.linkIdentity(identity.OIDCIdentity{UserID: u.ID, Issuer: googleIssuer, Subject: "sub-1"}); err != nil {
			t.Fatal(err)
		}
		disable(repo, "admin@example.com")
		p.claims = authoritativeClaims("admin@example.com")
		pending := startFlow(t, svc, p)
		if _, err := svc.LoginWithGoogle(ctx, pending.State, "code-1", pending); !errors.Is(err, identity.ErrUserDisabled) {
			t.Errorf("disabled linked user = %v, want ErrUserDisabled", err)
		}
	})

	t.Run("email path must not link", func(t *testing.T) {
		t.Parallel()
		repo := newFake()
		p := &fakeProvider{}
		svc := newOIDCService(t, repo, &capturingRecorder{}, p)
		bootstrapUser(t, svc)
		disable(repo, "admin@example.com")
		p.claims = authoritativeClaims("admin@example.com")
		pending := startFlow(t, svc, p)
		if _, err := svc.LoginWithGoogle(ctx, pending.State, "code-1", pending); !errors.Is(err, identity.ErrUserDisabled) {
			t.Errorf("disabled email match = %v, want ErrUserDisabled", err)
		}
		// The identity must NOT be linked to a disabled account — re-enabling the user later must not silently activate an unapproved link.
		if len(repo.oidc) != 0 {
			t.Errorf("disabled user was linked: %v", repo.oidc)
		}
	})
}

func TestGoogleLoginRejectsBadPendingBeforeExchange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		name    string
		mutate  func(p *auth.OIDCPending, state *string, clock *time.Time)
		wantErr error
	}{
		{"missing pending (zero value)", func(p *auth.OIDCPending, _ *string, _ *time.Time) {
			*p = auth.OIDCPending{}
		}, auth.ErrOIDCPendingInvalid},
		{"expired pending", func(p *auth.OIDCPending, _ *string, clock *time.Time) {
			*clock = p.ExpiresAt.Add(time.Second)
		}, auth.ErrOIDCPendingInvalid},
		{"state mismatch", func(_ *auth.OIDCPending, state *string, _ *time.Time) {
			*state = "attacker-forged-state"
		}, auth.ErrOIDCPendingInvalid},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := newFake()
			p := &fakeProvider{}
			clock := time.Now()
			svc := newOIDCService(t, repo, &capturingRecorder{}, p).WithClock(func() time.Time { return clock })
			u := bootstrapUser(t, svc)
			if err := repo.linkIdentity(identity.OIDCIdentity{UserID: u.ID, Issuer: googleIssuer, Subject: "sub-1"}); err != nil {
				t.Fatal(err)
			}
			p.claims = authoritativeClaims("admin@example.com")
			pending := startFlow(t, svc, p)
			state := pending.State
			tc.mutate(&pending, &state, &clock)

			if _, err := svc.LoginWithGoogle(ctx, state, "code-1", pending); !errors.Is(err, tc.wantErr) {
				t.Errorf("LoginWithGoogle = %v, want %v", err, tc.wantErr)
			}
			// The pending checks gate the network call: a forged or stale callback must never spend a code exchange against Google.
			if p.exchanges != 0 {
				t.Errorf("exchange ran %d times, want 0 (pending checks come first)", p.exchanges)
			}
		})
	}
}

func TestGoogleLoginRejectsNonceMismatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := newFake()
	p := &fakeProvider{}
	svc := newOIDCService(t, repo, &capturingRecorder{}, p)
	u := bootstrapUser(t, svc)
	if err := repo.linkIdentity(identity.OIDCIdentity{UserID: u.ID, Issuer: googleIssuer, Subject: "sub-1"}); err != nil {
		t.Fatal(err)
	}
	p.claims = authoritativeClaims("admin@example.com")
	pending := startFlow(t, svc, p)

	p.claims.Nonce = "replayed-nonce"

	if _, err := svc.LoginWithGoogle(ctx, pending.State, "code-1", pending); !errors.Is(err, auth.ErrOIDCNonceMismatch) {
		t.Errorf("nonce mismatch = %v, want ErrOIDCNonceMismatch", err)
	}

	if len(repo.sessions) != 0 {
		t.Errorf("rejected login left %d sessions", len(repo.sessions))
	}
}

func TestGoogleLoginRejectsExchangeError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := &fakeProvider{exchangeErr: errors.New("provider: exchange failed")}
	svc := newOIDCService(t, newFake(), &capturingRecorder{}, p)
	pending := startFlow(t, svc, p)

	if _, err := svc.LoginWithGoogle(ctx, pending.State, "bad-code", pending); err == nil {
		t.Error("LoginWithGoogle with failing exchange = nil, want error")
	}
}

func TestGoogleLoginRejectsLinkRace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := newFake()
	p := &fakeProvider{}
	svc := newOIDCService(t, linkRaceRepo{repo}, &capturingRecorder{}, p)
	bootstrapUser(t, svc)
	p.claims = authoritativeClaims("admin@example.com")
	pending := startFlow(t, svc, p)

	if _, err := svc.LoginWithGoogle(ctx, pending.State, "code-1", pending); !errors.Is(err, identity.ErrIdentityLinkedToAnotherUser) {
		t.Errorf("link race = %v, want ErrIdentityLinkedToAnotherUser", err)
	}
}

func TestGoogleLoginEveryFailureEmitsOneAuditEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		name string
		run  func(t *testing.T, rec auth.AuditRecorder) error
	}{
		{"missing pending", func(t *testing.T, rec auth.AuditRecorder) error {
			p := &fakeProvider{}
			svc := newOIDCService(t, newFake(), rec, p)
			_, err := svc.LoginWithGoogle(ctx, "state", "code", auth.OIDCPending{})
			return err
		}},
		{"state mismatch", func(t *testing.T, rec auth.AuditRecorder) error {
			p := &fakeProvider{}
			svc := newOIDCService(t, newFake(), rec, p)
			pending := startFlow(t, svc, p)
			_, err := svc.LoginWithGoogle(ctx, "forged", "code", pending)
			return err
		}},
		{"nonce mismatch", func(t *testing.T, rec auth.AuditRecorder) error {
			p := &fakeProvider{}
			svc := newOIDCService(t, newFake(), rec, p)
			p.claims = authoritativeClaims("admin@example.com")
			pending := startFlow(t, svc, p)
			p.claims.Nonce = "other"
			_, err := svc.LoginWithGoogle(ctx, pending.State, "code", pending)
			return err
		}},
		{"exchange error", func(t *testing.T, rec auth.AuditRecorder) error {
			p := &fakeProvider{exchangeErr: errors.New("boom")}
			svc := newOIDCService(t, newFake(), rec, p)
			pending := startFlow(t, svc, p)
			_, err := svc.LoginWithGoogle(ctx, pending.State, "code", pending)
			return err
		}},
		{"unverified email", func(t *testing.T, rec auth.AuditRecorder) error {
			p := &fakeProvider{}
			svc := newOIDCService(t, newFake(), rec, p)
			bootstrapUser(t, svc)
			p.claims = authoritativeClaims("admin@example.com")
			p.claims.EmailVerified = false
			pending := startFlow(t, svc, p)
			_, err := svc.LoginWithGoogle(ctx, pending.State, "code", pending)
			return err
		}},
		{"unknown email", func(t *testing.T, rec auth.AuditRecorder) error {
			p := &fakeProvider{}
			svc := newOIDCService(t, newFake(), rec, p)
			p.claims = authoritativeClaims("ghost@example.com")
			pending := startFlow(t, svc, p)
			_, err := svc.LoginWithGoogle(ctx, pending.State, "code", pending)
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := &capturingRecorder{}
			if err := tc.run(t, rec); err == nil {
				t.Fatal("scenario must fail")
			}
			var got []audit.Event
			for _, e := range rec.all() {
				if e.Action == audit.ActionAuthLogin && e.Outcome == audit.OutcomeFailed {
					got = append(got, e)
				}
			}
			if len(got) != 1 {
				t.Fatalf("AUTH_LOGIN/FAILED events = %d, want exactly 1: %+v", len(got), got)
			}
			if m := got[0].Metadata["method"]; m != "google" {
				t.Errorf("failure event metadata method = %v, want google", m)
			}
		})
		t.Run(tc.name+"/audit store down", func(t *testing.T) {
			t.Parallel()
			// The login error must surface unchanged even when the recorder fails.
			if err := tc.run(t, errRecorder{}); err == nil {
				t.Error("scenario must still fail with a broken audit store")
			}
		})
	}
}

func TestGoogleVerifiedThirdPartyEmailCannotAttachAnExistingAccount(t *testing.T) {
	t.Parallel()
	repo := newFake()
	recorder := &capturingRecorder{}
	provider := &fakeProvider{}
	service := newOIDCService(t, repo, recorder, provider)
	bootstrapUser(t, service)
	provider.claims = authoritativeClaims("admin@example.com")
	provider.claims.EmailAuthoritative = false
	pending := startFlow(t, service, provider)
	if _, err := service.LoginWithGoogle(context.Background(), pending.State, "code", pending); !errors.Is(err, identity.ErrNoLinkedAccount) {
		t.Fatalf("historically verified email acquired an account: %v", err)
	}
	if len(repo.oidc) != 0 {
		t.Fatal("refused subject was linked")
	}
	failures := 0
	for _, event := range recorder.all() {
		if event.Action == audit.ActionAuthLogin {
			if event.Outcome != audit.OutcomeFailed {
				t.Fatal("refused login emitted success")
			}
			failures++
		}
	}
	if failures != 1 {
		t.Fatalf("login failure evidence=%d, want 1", failures)
	}
}
