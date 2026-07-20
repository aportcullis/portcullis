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

	"connectrpc.com/connect"

	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
	auditapp "github.com/aportcullis/portcullis/internal/app/audit"
	"github.com/aportcullis/portcullis/internal/app/auth"
	"github.com/aportcullis/portcullis/internal/app/authz"
	connapp "github.com/aportcullis/portcullis/internal/app/connection"
	connpolicy "github.com/aportcullis/portcullis/internal/app/connectionpolicy"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/aportcullis/portcullis/internal/infra/googleoidc"
	"github.com/aportcullis/portcullis/internal/infra/pgdialect"
	"github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/aportcullis/portcullis/internal/platform/config"
	"github.com/aportcullis/portcullis/internal/platform/logging"
	"github.com/aportcullis/portcullis/internal/transport/connectapi"
	"github.com/aportcullis/portcullis/internal/transport/server"
)

// migrate opens the owner DSN, verifies connectivity, applies the migrations,
// and closes the pool — the owner credential stays alive only for this window.
// Errors are logged with generic messages/classified fields only: the raw error
// can echo the DSN, which carries the password.
func migrate(ctx context.Context, logger *slog.Logger, ownerURL, runtimeRole string) error {
	pool, err := postgres.Open(ctx, ownerURL)
	if err != nil {
		logger.Error("database config invalid", "hint", "check PORTCULLIS_MIGRATE_DATABASE_URL / PORTCULLIS_DATABASE_URL")
		return err
	}
	defer pool.Close()
	// Force the first connection so a connectivity/auth failure is caught with a
	// generic message; after this, Migrate runs on a verified connection.
	if err := pool.Ping(ctx); err != nil {
		logger.Error("database connect failed", "hint", "check the database URL and that the database is reachable")
		return err
	}
	if err := postgres.Migrate(ctx, pool, postgres.WithRuntimeRole(runtimeRole)); err != nil {
		// pgxpool acquires a connection per operation, so even after the Ping this can
		// be a connect error that echoes the DSN — classify: a server SQL error logs
		// its code + structural identifiers, anything else logs only its type.
		logger.Error("migration failed", postgres.ErrorLogFields(err)...)
		return err
	}
	return nil
}

func main() {
	// run/runMigrate return an error on any failure; main maps that to a non-zero
	// exit. Cleanup lives in deferred calls inside them, which still run.
	var err error
	switch args := os.Args[1:]; {
	case len(args) == 0 || args[0] == "serve":
		err = run()
	case args[0] == "migrate":
		err = runMigrate()
	default:
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("unknown command",
			"command", args[0], "hint", "usage: portcullis [serve|migrate]")
		os.Exit(2)
	}
	if err != nil {
		os.Exit(1)
	}
}

// runMigrate is the one-shot `portcullis migrate` command: apply the migrations
// on the owner DSN and exit. Running it as a separate short-lived process or
// container keeps owner credentials out of the serving process entirely — the
// application account must not own the schema (ADR-0009, OWASP Database
// Security Cheat Sheet).
func runMigrate() error {
	cfg, err := config.Load()
	if err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("config load failed", "err", err)
		return err
	}
	logger := logging.New(cfg.LogLevel, cfg.LogFormat)
	if cfg.OwnerDSN() == "" {
		logger.Error("database required", "hint", "set PORTCULLIS_MIGRATE_DATABASE_URL (owner DSN); PORTCULLIS_DATABASE_URL is used when it is the same single-role login")
		return errors.New("database url required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrate(ctx, logger, cfg.OwnerDSN(), cfg.RuntimeRole); err != nil {
		return err
	}
	logger.Info("metadata schema ready")
	return nil
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

	// Migrations run as the schema OWNER on a short-lived pool; the server then
	// runs on the (least-privilege) runtime DSN — the permission boundary that
	// keeps audit_events append-only even against the application (ADR-0009).
	// The recommended production shape runs them as a one-shot `portcullis
	// migrate` instead, so the serving process never holds owner credentials;
	// startup migration stays for a server-held owner DSN (compatibility) and
	// the single-role dev posture.
	if cfg.StartupMigrationEnabled() {
		if err := migrate(startupCtx, logger, cfg.OwnerDSN(), cfg.RuntimeRole); err != nil {
			return err
		}
		logger.Info("metadata schema ready")
	} else {
		logger.Info("startup migration skipped — schema is managed by the one-shot `portcullis migrate` command")
	}

	pool, err := postgres.Open(startupCtx, cfg.DatabaseURL)
	if err != nil {
		// Open only parses the DSN (pgxpool.New is lazy); a malformed config lands here.
		logger.Error("database config invalid", "hint", "check PORTCULLIS_DATABASE_URL")
		return err
	}
	defer pool.Close()
	if err := pool.Ping(startupCtx); err != nil {
		logger.Error("database connect failed", "hint", "check PORTCULLIS_DATABASE_URL and that the database is reachable")
		return err
	}
	// The boundary must hold for the connection the server ACTUALLY runs on, not
	// just the configured role: an owner/superuser DSN or a drifted login user is
	// refused (ADR-0009). The insecure dev flag downgrades ONLY over-privilege
	// violations; a wrong/unmigrated database or a query failure is always fatal.
	if err := postgres.VerifyRuntimeConnection(startupCtx, pool, cfg.RuntimeRole); err != nil {
		if cfg.AllowPrivilegedRuntime && errors.Is(err, postgres.ErrRuntimeInsecure) {
			logger.Warn("runtime connection is over-privileged — allowed by PORTCULLIS_ALLOW_PRIVILEGED_RUNTIME (dev only, never production)", "reason", err.Error())
		} else {
			logger.Error("runtime connection failed verification",
				append(postgres.ErrorLogFields(err), "hint", "an unmigrated database fails this check — run `portcullis migrate` (owner DSN) first")...)
			return err
		}
	}

	// Cap request size: Connect defaults to unlimited, so bound both the per-message
	// read and the whole request stream (auth/health messages are tiny).
	readLimit := connect.WithReadMaxBytes(server.MaxRequestBytes)
	// Recover panics into a clean CodeInternal (logged via slog, not a stderr stack
	// dump); wired first so it wraps every interceptor and the handler.
	recoverOpt := connectapi.NewRecoverOption(logger)
	healthPath, healthHandler := portcullisv1connect.NewHealthHandler(connectapi.HealthService{}, recoverOpt, readLimit)

	// Identity vertical: inject the crypto and audit adapters into the auth use
	// cases, then expose them as the Auth RPC behind the interceptor chain.
	store := postgres.NewIdentityStore(pool)
	authSvc, err := auth.New(store, crypto.NewArgon2Hasher(crypto.DefaultArgon2Params, cfg.Argon2MaxConcurrent), crypto.NewCSRFProtector(keyring), postgres.NewAuditStore(pool), auth.Config{
		BackoffThreshold: cfg.LoginBackoffThreshold,
		BackoffBase:      cfg.LoginBackoffBase,
		BackoffCap:       cfg.LoginBackoffCap,
	})
	if err != nil {
		logger.Error("auth init failed", "err", err)
		return err
	}
	authSvc.WithLogger(logger)

	// Authorization: load the seeded permission catalog once (ADR-0008) and refuse
	// to boot if it is missing — an unseeded catalog means every has(permission)
	// check would be undecidable, the same fail-fast stance as the keyring and the
	// runtime-connection checks above.
	catalog, err := authz.LoadCatalog(startupCtx, store)
	if err != nil {
		logger.Error("permission catalog unavailable", "err", err, "hint", "apply migrations (`portcullis migrate`) — 0002 seeds the permission catalog")
		return err
	}
	authzSvc, err := authz.New(store, catalog)
	if err != nil {
		logger.Error("authz init failed", "err", err)
		return err
	}
	logger.Info("authorization ready", "permissions", len(catalog))

	auditReader, err := auditapp.New(postgres.NewAuditStore(pool))
	if err != nil {
		logger.Error("audit read init failed", "err", err)
		return err
	}

	// One interceptor chain shared by every authenticated RPC surface: recover wraps
	// the whole chain; the error logger is outermost of the interceptors so it records
	// any server-fault error from the inner interceptors or the handler (returned
	// errors are otherwise invisible — recover only catches panics); client IP
	// resolves next (rate limiting and audit consume it), rate-limit sheds floods
	// before auth, then auth injects the user (ADR-0006/0010).
	recoverAndChain := []connect.HandlerOption{
		recoverOpt,
		connect.WithInterceptors(
			connectapi.NewErrorLogInterceptor(logger),
			connectapi.NewClientIPInterceptor(cfg.TrustedProxyNets()),
			connectapi.NewRateLimitInterceptor(),
			connectapi.NewAuthInterceptor(authSvc),
		),
		readLimit,
	}
	authPath, authHandler := portcullisv1connect.NewAuthHandler(connectapi.NewAuthService(authSvc, authzSvc).WithLogger(logger), recoverAndChain...)
	// Audit.List is gated by the audit.list permission inside the handler (ADR-0008).
	auditPath, auditHandler := portcullisv1connect.NewAuditHandler(connectapi.NewAuditService(authzSvc, auditReader), recoverAndChain...)

	// Connections vertical (ADR-0014): the postgres store, the keyring-backed
	// credential codec, and the PostgreSQL dialect adapter (its
	// ValidateConnection satisfies the ConnectionValidator port, PRD §5.3)
	// behind the connections.* gated RPCs.
	pgDialect := pgdialect.New(pgdialect.Options{ValidateTimeout: cfg.ConnectionTestTimeout})
	connSvc, err := connapp.New(
		postgres.NewConnectionStore(pool),
		pgDialect,
		crypto.NewConnectionCredentialCodec(keyring),
		postgres.NewAuditStore(pool),
	)
	if err != nil {
		logger.Error("connections init failed", "err", err)
		return err
	}
	connSvc.WithLogger(logger)
	connsPath, connsHandler := portcullisv1connect.NewConnectionsHandler(connectapi.NewConnectionsService(authzSvc, connSvc), recoverAndChain...)

	// Connection policies (ADR-0015): per-connection execution policy as
	// immutable versions behind the policies.* gated RPCs.
	policySvc, err := connpolicy.New(postgres.NewConnectionPolicyStore(pool))
	if err != nil {
		logger.Error("connection policies init failed", "err", err)
		return err
	}
	policiesPath, policiesHandler := portcullisv1connect.NewConnectionPoliciesHandler(connectapi.NewConnectionPoliciesService(authzSvc, policySvc), recoverAndChain...)

	mounts := []server.Mount{
		{Pattern: healthPath, Handler: http.MaxBytesHandler(healthHandler, server.MaxRequestBytes)},
		{Pattern: authPath, Handler: http.MaxBytesHandler(authHandler, server.MaxRequestBytes)},
		{Pattern: auditPath, Handler: http.MaxBytesHandler(auditHandler, server.MaxRequestBytes)},
		{Pattern: connsPath, Handler: http.MaxBytesHandler(connsHandler, server.MaxRequestBytes)},
		{Pattern: policiesPath, Handler: http.MaxBytesHandler(policiesHandler, server.MaxRequestBytes)},
	}

	// Google login (ADR-0007) mounts only when configured: provider discovery must
	// succeed at boot (fail-fast, like the keyring), and when disabled the routes
	// simply don't exist. The redirect flow is plain HTTP, not Connect.
	if cfg.GoogleEnabled() {
		secret, err := cfg.ResolveGoogleClientSecret()
		if err != nil {
			logger.Error("google login config invalid", "err", err)
			return err
		}
		provider, err := googleoidc.New(startupCtx, googleoidc.GoogleIssuer, cfg.GoogleClientID, secret, cfg.GoogleRedirectURL)
		if err != nil {
			logger.Error("google provider discovery failed", "err", err, "hint", "google login requires reachability to accounts.google.com at startup")
			return err
		}
		authSvc.WithOIDCProvider(provider)
		orgID, err := store.DefaultOrganizationID(startupCtx)
		if err != nil {
			logger.Error("default organization unavailable", "err", err)
			return err
		}
		oidcHandler := connectapi.NewOIDCHandler(authSvc, crypto.NewOIDCPendingCodec(keyring, string(orgID)), cfg.TrustedProxyNets(), logger)
		mounts = append(mounts,
			server.Mount{Pattern: connectapi.OIDCStartPattern, Handler: oidcHandler.Start()},
			server.Mount{Pattern: connectapi.OIDCCallbackPattern, Handler: oidcHandler.Callback()},
		)
		logger.Info("google login enabled")
	}

	srv := server.New(cfg.Addr, logger, cfg.DrainDelay, mounts...)
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
		// Restore default signal handling now that the first signal is being handled,
		// so a SECOND SIGINT/SIGTERM force-quits during a long drain instead of being
		// swallowed by NotifyContext (os/signal: stop as soon as the first signal is
		// handled). The deferred stop() still covers the serveErr path.
		stop()
		logger.Info("shutting down")
	case err := <-serveErr:
		logger.Error("server error", "err", err)
		return err
	}

	// Background, not a timeout: the drain delay and the shutdown timeout are
	// sequential budgets (ADR-0010) — Server.Shutdown applies the timeout to the
	// drain of in-flight requests only, after the delay has fully elapsed.
	if err := srv.Shutdown(context.Background(), cfg.ShutdownTimeout); err != nil {
		logger.Error("graceful shutdown failed", "err", err)
		return err
	}
	return nil
}
