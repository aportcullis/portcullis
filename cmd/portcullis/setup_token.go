package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"

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

// writeSetupTokenFile writes the token to an owner-only temporary file beside path and renames it into place, so the path is replaced atomically, readers never see a partial token, and a symlink planted at path is replaced rather than written through.
func writeSetupTokenFile(path, token string) (err error) {
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	temporaryPath := file.Name()
	defer func() {
		if err != nil {
			_ = file.Close()
			_ = os.Remove(temporaryPath)
		}
	}()
	if err = file.Chmod(setupTokenFileMode); err != nil {
		return err
	}
	if _, err = file.WriteString(token + "\n"); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
