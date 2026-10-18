package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// ResolvePasswordSetup keeps unrelated auth fixtures password-setup incapable.
func (f *fakeRepo) ResolvePasswordSetup(context.Context, []byte) (identity.PasswordSetup, error) {
	return identity.PasswordSetup{}, identity.ErrPasswordSetupInvalid
}

// CompletePasswordSetup refuses setup in unrelated auth fixtures.
func (f *fakeRepo) CompletePasswordSetup(context.Context, []byte, string, audit.Event) error {
	return identity.ErrPasswordSetupInvalid
}

type setupRepository struct {
	*fakeRepo
	resolvedError  error
	completedError error
	completed      bool
	digest         []byte
	passwordHash   string
	event          audit.Event
}

// ResolvePasswordSetup captures only the digest supplied by the use case.
func (repo *setupRepository) ResolvePasswordSetup(_ context.Context, digest []byte) (identity.PasswordSetup, error) {
	repo.digest = append([]byte(nil), digest...)
	return identity.PasswordSetup{UserID: "member", OrganizationID: "org"}, repo.resolvedError
}

// CompletePasswordSetup captures the atomic credential command, never a plaintext password.
func (repo *setupRepository) CompletePasswordSetup(_ context.Context, _ []byte, hash string, event audit.Event) error {
	repo.completed = true
	repo.passwordHash = hash
	repo.event = event
	return repo.completedError
}

func TestPasswordSetupUsesExistingCredentialPolicyWithoutStartingSession(t *testing.T) {
	for _, password := range []string{"correct-horse-battery", "valid spaced password  ", "e\u0301-normalized-password", strings.Repeat("a", 1024)} {
		t.Run(password[:min(12, len(password))], func(t *testing.T) {
			repo := &setupRepository{fakeRepo: newFake()}
			service := newService(t, repo)
			token := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
			if err := service.CompletePasswordSetup(context.Background(), token, password); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256([]byte(token))
			if string(repo.digest) != string(digest[:]) || !repo.completed || repo.passwordHash == password || repo.event.Action != audit.ActionUserPasswordSet || len(repo.sessions) != 0 {
				t.Fatalf("incomplete credential-only command: %+v", repo)
			}
		})
	}
}

func TestPasswordSetupRejectsInvalidInputAndConcurrentRefusal(t *testing.T) {
	token := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	for _, scenario := range []struct {
		name, token, password string
		resolve, complete     error
		want                  error
	}{
		{"missing token", "", "correct-horse-battery", nil, nil, identity.ErrPasswordSetupInvalid},
		{"malformed token", "not-a-token", "correct-horse-battery", nil, nil, identity.ErrPasswordSetupInvalid},
		{"weak password", token, "short", nil, nil, identity.ErrWeakPassword},
		{"expired link", token, "correct-horse-battery", identity.ErrPasswordSetupInvalid, nil, identity.ErrPasswordSetupInvalid},
		{"revoked during hashing", token, "correct-horse-battery", nil, identity.ErrPasswordSetupInvalid, identity.ErrPasswordSetupInvalid},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			repo := &setupRepository{fakeRepo: newFake(), resolvedError: scenario.resolve, completedError: scenario.complete}
			err := newService(t, repo).CompletePasswordSetup(context.Background(), scenario.token, scenario.password)
			if err == nil || scenario.want != nil && !errors.Is(err, scenario.want) {
				t.Fatalf("refusal=%v", err)
			}
			if scenario.complete == nil && repo.completed {
				t.Fatal("invalid request reached credential writer")
			}
		})
	}
}
