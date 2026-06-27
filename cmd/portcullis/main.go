// Command portcullis is the single-binary server entrypoint.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/aportcullis/portcullis/internal/platform/config"
	"github.com/aportcullis/portcullis/internal/platform/logging"
	"github.com/aportcullis/portcullis/internal/transport/connectapi"
	"github.com/aportcullis/portcullis/internal/transport/server"
)

func main() {
	// run returns an error on any startup or runtime failure; main maps that to a
	// non-zero exit. Cleanup lives in deferred calls inside run, which still run.
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("config load failed", "err", err)
		return err
	}

	logger := logging.New(cfg.LogLevel, cfg.LogFormat)

	// Refuse to start without a valid master key — encryption is mandatory.
	keyring, err := crypto.LoadKeyring(cfg.MasterKey, cfg.MasterKeyFile)
	if err != nil {
		logger.Error("master key required",
			"err", err,
			"hint", "set PORTCULLIS_MASTER_KEY (base64 of 32 bytes) or PORTCULLIS_MASTER_KEY_FILE; generate one with `make devkey`")
		return err
	}
	logger.Info("crypto ready", "key_version", keyring.Active())

	if cfg.DatabaseURL == "" {
		logger.Error("database required", "hint", "set PORTCULLIS_DATABASE_URL")
		return errors.New("database url required")
	}

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelStartup()

	pool, err := postgres.Open(startupCtx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database connect failed", "err", err)
		return err
	}
	defer pool.Close()

	if err := postgres.Migrate(startupCtx, pool); err != nil {
		logger.Error("migration failed", "err", err)
		return err
	}
	logger.Info("metadata schema ready")

	healthPath, healthHandler := portcullisv1connect.NewHealthHandler(connectapi.HealthService{})

	srv := server.New(cfg.Addr, logger, cfg.DrainDelay,
		server.Mount{Pattern: healthPath, Handler: healthHandler},
	)
	srv.Health().Register("metadata-db", func(ctx context.Context) error { return pool.Ping(ctx) })

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// A bind/serve failure must terminate the process with a non-zero exit, not
	// fall through to the normal shutdown path.
	serveErr := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutting down")
	case err := <-serveErr:
		logger.Error("server error", "err", err)
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "err", err)
		return err
	}
	return nil
}
