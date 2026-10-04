package main

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/aportcullis/portcullis/internal/app/auth"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// setupTokenFileMode keeps the delivered token readable only by the runtime user.
const setupTokenFileMode os.FileMode = 0o600

// setupTokenIssuer is the slice of the auth service that mints the first-run setup token.
type setupTokenIssuer interface {
	IssueSetupToken(ctx context.Context) (string, error)
}

// deliverFirstRunSetupToken issues a fresh setup token while no user exists and hands it to the operator exactly once, through the configured file or else the startup log (ADR-0052).
func deliverFirstRunSetupToken(ctx context.Context, logger *slog.Logger, issuer setupTokenIssuer, tokenFile string) error {
	token, err := issuer.IssueSetupToken(ctx)
	if errors.Is(err, identity.ErrAlreadyBootstrapped) {
		return nil
	}
	if err != nil {
		logger.Error("setup token issuance failed", "err", err)
		return err
	}
	if tokenFile != "" {
		if err := writeSetupTokenFile(tokenFile, token); err != nil {
			logger.Error("setup token file unwritable", "path", tokenFile, "err", err)
			return err
		}
		logger.Warn("first-run setup token written — enter it on /bootstrap to create the first admin", "path", tokenFile, "expires_in", auth.SetupTokenTTL.String())
		return nil
	}
	logger.Warn("first-run setup token issued — enter it on /bootstrap to create the first admin; it is logged only at this start, a restart replaces it", "setup_token", token, "expires_in", auth.SetupTokenTTL.String())
	return nil
}

// writeSetupTokenFile replaces the file's contents with the token and narrows its mode to owner read/write, including when the file already existed with broader permissions.
func writeSetupTokenFile(path, token string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, setupTokenFileMode)
	if err != nil {
		return err
	}
	if err := file.Chmod(setupTokenFileMode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.WriteString(token + "\n"); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
