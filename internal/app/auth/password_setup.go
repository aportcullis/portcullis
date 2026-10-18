package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// PasswordSetupRepository resolves a digest and commits initial credentials, consumption, session revocation and audit atomically.
type PasswordSetupRepository interface {
	ResolvePasswordSetup(context.Context, []byte) (identity.PasswordSetup, error)
	CompletePasswordSetup(context.Context, []byte, string, audit.Event) error
}

// CompletePasswordSetup registers an initial password without issuing a session or changing grants.
func (s *Service) CompletePasswordSetup(ctx context.Context, token, password string) (err error) {
	defer func() {
		if err != nil {
			s.recordAudit(ctx, newEvent(ctx, audit.ActionUserPasswordSet, audit.OutcomeFailed))
		}
	}()
	password = normalizePassword(password)
	if err := validatePassword(password); err != nil {
		return err
	}
	if len(token) != 43 {
		return identity.ErrPasswordSetupInvalid
	}
	decoded, decodeError := base64.RawURLEncoding.DecodeString(token)
	if decodeError != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != token {
		return identity.ErrPasswordSetupInvalid
	}
	digest := sha256.Sum256([]byte(token))
	if _, err := s.repo.ResolvePasswordSetup(ctx, digest[:]); err != nil {
		return err
	}
	passwordHash, err := s.hasher.Hash(ctx, password)
	if err != nil {
		return err
	}
	return s.repo.CompletePasswordSetup(ctx, digest[:], passwordHash, newEvent(ctx, audit.ActionUserPasswordSet, audit.OutcomeSucceeded))
}
