package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/aportcullis/portcullis/internal/app/keyrotation"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/aportcullis/portcullis/internal/platform/config"
	"github.com/aportcullis/portcullis/internal/platform/logging"
)

// runKeyRotation eagerly rotates encrypted metadata using the restricted runtime login.
func runKeyRotation() error {
	cfg, err := config.Load()
	if err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("rotation configuration invalid")
		return err
	}
	logger := logging.New(cfg.LogLevel, cfg.LogFormat)
	ring, err := crypto.LoadVersionedKeyring(cfg.MasterKey, cfg.MasterKeyFile, cfg.MasterKeyPrevious, cfg.MasterKeyPreviousFile)
	if err != nil {
		logger.Error("rotation keyring invalid")
		return err
	}
	if ring.Active() < 2 {
		logger.Error("rotation requires historical and new active keys")
		return errors.New("rotation requires historical keys")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	pool, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("rotation database configuration invalid")
		return err
	}
	defer pool.Close()
	if err = postgres.VerifyRuntimeConnection(ctx, pool, cfg.RuntimeRole); err != nil {
		if !cfg.AllowPrivilegedRuntime || !errors.Is(err, postgres.ErrRuntimeInsecure) {
			logger.Error("rotation runtime verification failed")
			return err
		}
	}
	service, err := keyrotation.New(postgres.NewKeyRotationStore(pool), ring)
	if err != nil {
		return err
	}
	count, err := service.Rotate(ctx)
	if err != nil {
		logger.Error("rotation stopped; batches can be resumed", "committed_rows", count)
		return err
	}
	logger.Info("rotation complete", "active_version", ring.Active(), "rotated_rows", count, "rows_on_old_encryption_versions", 0, "retain_historical_digest_keys", true)
	return nil
}
