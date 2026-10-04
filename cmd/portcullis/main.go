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
	"github.com/google/uuid"

	"github.com/aportcullis/portcullis/gen/portcullis/v1/portcullisv1connect"
	accessreq "github.com/aportcullis/portcullis/internal/app/accessrequest"
	auditapp "github.com/aportcullis/portcullis/internal/app/audit"
	"github.com/aportcullis/portcullis/internal/app/auth"
	"github.com/aportcullis/portcullis/internal/app/authz"
	connapp "github.com/aportcullis/portcullis/internal/app/connection"
	connpolicy "github.com/aportcullis/portcullis/internal/app/connectionpolicy"
	executionapp "github.com/aportcullis/portcullis/internal/app/execution"
	resultapp "github.com/aportcullis/portcullis/internal/app/result"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/aportcullis/portcullis/internal/infra/dialectregistry"
	"github.com/aportcullis/portcullis/internal/infra/executionguard"
	"github.com/aportcullis/portcullis/internal/infra/googleoidc"
	"github.com/aportcullis/portcullis/internal/infra/pgdialect"
	"github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/aportcullis/portcullis/internal/platform/config"
	"github.com/aportcullis/portcullis/internal/platform/logging"
	"github.com/aportcullis/portcullis/internal/transport/connectapi"
	"github.com/aportcullis/portcullis/internal/transport/server"
)

// migrate opens the owner DSN, verifies connectivity, applies the migrations, and closes the pool — the owner credential stays alive only for this window. Errors are logged with generic messages/classified fields only: the raw error can echo the DSN, which carries the password.
func migrate(ctx context.Context, logger *slog.Logger, ownerURL, runtimeRole string) error {
	pool, err := postgres.Open(ctx, ownerURL)
	if err != nil {
		logger.Error("database config invalid", "hint", "check PORTCULLIS_MIGRATE_DATABASE_URL / PORTCULLIS_DATABASE_URL")
		return err
	}
	defer pool.Close()
	// Force the first connection so a connectivity/auth failure is caught with a generic message; after this, Migrate runs on a verified connection.
	if err := pool.Ping(ctx); err != nil {
		logger.Error("database connect failed", "hint", "check the database URL and that the database is reachable")
		return err
	}
	if err := postgres.Migrate(ctx, pool, postgres.WithRuntimeRole(runtimeRole)); err != nil {
		// pgxpool acquires a connection per operation, so even after the Ping this can be a connect error that echoes the DSN — classify: a server SQL error logs its code + structural identifiers, anything else logs only its type.
		logger.Error("migration failed", postgres.ErrorLogFields(err)...)
		return err
	}
	return nil
}

func main() {
	// run/runMigrate return an error on any failure; main maps that to a non-zero exit. Cleanup lives in deferred calls inside them, which still run.
	var err error
	switch args := os.Args[1:]; {
	case len(args) == 0 || args[0] == "serve":
		err = run()
	case args[0] == "migrate":
		err = runMigrate()
	case len(args) == 2 && args[0] == "key" && args[1] == "rotate":
		err = runKeyRotation()
	default:
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("unknown command",
			"hint", "usage: portcullis [serve|migrate|key rotate]")
		os.Exit(2)
	}
	if err != nil {
		os.Exit(1)
	}
}

// runMigrate is the one-shot `portcullis migrate` command: apply the migrations on the owner DSN and exit. Running it as a separate short-lived process or container keeps owner credentials out of the serving process entirely — the application account must not own the schema (ADR-0009, OWASP Database Security Cheat Sheet).
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
	keyring, err := crypto.LoadVersionedKeyring(cfg.MasterKey, cfg.MasterKeyFile, cfg.MasterKeyPrevious, cfg.MasterKeyPreviousFile)
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

	// Apply migrations with the owner DSN and serve with the restricted runtime DSN (ADR-0009). A separate migrate process keeps owner credentials out of the server.
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
	// The boundary must hold for the connection the server ACTUALLY runs on, not just the configured role: an owner/superuser DSN or a drifted login user is refused (ADR-0009). The insecure dev flag downgrades ONLY over-privilege violations; a wrong/unmigrated database or a query failure is always fatal.
	if err := postgres.VerifyRuntimeConnection(startupCtx, pool, cfg.RuntimeRole); err != nil {
		if cfg.AllowPrivilegedRuntime && errors.Is(err, postgres.ErrRuntimeInsecure) {
			logger.Warn("runtime connection is over-privileged — allowed by PORTCULLIS_ALLOW_PRIVILEGED_RUNTIME (dev only, never production)", "reason", err.Error())
		} else {
			logger.Error("runtime connection failed verification",
				append(postgres.ErrorLogFields(err), "hint", "an unmigrated database fails this check — run `portcullis migrate` (owner DSN) first")...)
			return err
		}
	}

	// Cap request size: Connect defaults to unlimited, so bound both the per-message read and the whole request stream (auth/health messages are tiny).
	readLimit := connect.WithReadMaxBytes(server.MaxRequestBytes)
	// Recover panics into a clean CodeInternal (logged via slog, not a stderr stack dump); wired first so it wraps every interceptor and the handler.
	recoverOpt := connectapi.NewRecoverOption(logger)
	healthPath, healthHandler := portcullisv1connect.NewHealthHandler(connectapi.HealthService{}, recoverOpt, readLimit)

	// Identity vertical: inject the crypto and audit adapters into the auth use cases, then expose them as the Auth RPC behind the interceptor chain.
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

	// Create the configured first admin only on an empty installation; skip ErrAlreadyBootstrapped and fail startup on other errors. Keep email out of logs.
	if cfg.BootstrapAdminEnabled() {
		password, err := cfg.ResolveBootstrapAdminPassword()
		if err != nil {
			logger.Error("bootstrap admin password unavailable", "err", err)
			return err
		}
		switch _, err := authSvc.Bootstrap(startupCtx, cfg.BootstrapAdminEmail, password, cfg.BootstrapAdminDisplayName); {
		case err == nil:
			logger.Info("bootstrap admin created from config")
		case errors.Is(err, identity.ErrAlreadyBootstrapped):
			logger.Info("bootstrap admin skipped: users already exist")
		default:
			logger.Error("bootstrap admin creation failed", "err", err)
			return err
		}
	}

	// Authorization: load the seeded permission catalog once (ADR-0008) and refuse to boot if it is missing — an unseeded catalog means every has(permission) check would be undecidable, the same fail-fast stance as the keyring and the runtime-connection checks above.
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

	// Order interceptors as error logging → client IP → rate limiting → authentication, with panic recovery around the full chain (ADR-0010).
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

	// Connections vertical (ADR-0014): the postgres store, the keyring-backed credential codec, and the PostgreSQL dialect adapter (its ValidateConnection satisfies the ConnectionValidator port, PRD §5.3) behind the connections.* gated RPCs.
	pgDialect := pgdialect.New(pgdialect.Options{ValidateTimeout: cfg.ConnectionTestTimeout, LockTimeout: cfg.ExecutionLockTimeout})
	sqlDialects, err := dialectregistry.New(dialectregistry.Registration{Engine: connection.DBTypePostgreSQL, Adapter: pgDialect})
	if err != nil {
		return err
	}
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

	// Connection policies (ADR-0015): per-connection execution policy as immutable versions behind the policies.* gated RPCs.
	policySvc, err := connpolicy.New(postgres.NewConnectionPolicyStore(pool))
	if err != nil {
		logger.Error("connection policies init failed", "err", err)
		return err
	}
	policiesPath, policiesHandler := portcullisv1connect.NewConnectionPoliciesHandler(connectapi.NewConnectionPoliciesService(authzSvc, policySvc), recoverAndChain...)

	// Access requests vertical (ADR-0018): the state machine + approvals behind the requests.* gated RPCs. It reuses the dialect adapter (parse/classify/ bind/redact at submit) and a keyring-backed payload codec. The store satisfies both ports — request storage and the (separate, ISP-narrow) request-target listing — so one adapter covers both.
	requestStore := postgres.NewAccessRequestStore(pool)
	requestSvc, err := accessreq.NewWithDialects(
		requestStore,
		requestStore,
		crypto.NewAccessRequestPayloadCodec(keyring),
		sqlDialects,
		cfg.ApprovalValidity,
	)
	if err != nil {
		logger.Error("access requests init failed", "err", err)
		return err
	}
	requestsPath, requestsHandler := portcullisv1connect.NewAccessRequestsHandler(connectapi.NewAccessRequestsService(authzSvc, requestSvc), recoverAndChain...)

	resultStore := postgres.NewResultStore(pool)
	resultSvc, err := resultapp.New(resultStore, crypto.NewResultCodec(keyring), 2)
	if err != nil {
		return err
	}
	executionSvc, err := executionapp.NewWithDialects(requestStore, requestStore, postgres.NewConnectionStore(pool), crypto.NewAccessRequestPayloadCodec(keyring), crypto.NewConnectionCredentialCodec(keyring), sqlDialects, resultSvc, uuid.NewString(), 2)
	if err != nil {
		return err
	}
	executionSvc.WithAdmission(executionguard.New())
	// A failing attempt is logged and left for the next run; only a run that cannot list attempts at all stops startup.
	recoverySummary, recoveryErr := executionSvc.Reconcile(startupCtx)
	logExecutionRecovery(logger, recoverySummary, recoveryErr)
	if recoveryErr != nil {
		return recoveryErr
	}
	organizationID, err := requestStore.DefaultOrganizationID(startupCtx)
	if err != nil {
		return err
	}
	if err = resultStore.PurgeExpired(startupCtx, organizationID); err != nil {
		logger.Error("result startup cleanup failed")
		return err
	}
	queryOptions := append([]connect.HandlerOption(nil), recoverAndChain...)
	queryOptions = append(queryOptions, connect.WithCodec(connectapi.ExecutionJSONCodec{}))
	queryOptions = append(queryOptions, connect.WithInterceptors(connectapi.NewStreamSecurityInterceptor(authSvc, cfg.TrustedProxyNets())))
	executionsPath, executionsHandler := portcullisv1connect.NewQueryExecutionsHandler(connectapi.NewQueryExecutionsService(authzSvc, executionSvc, resultSvc), queryOptions...)

	mounts := []server.Mount{
		{Pattern: healthPath, Handler: http.MaxBytesHandler(healthHandler, server.MaxRequestBytes)},
		{Pattern: authPath, Handler: http.MaxBytesHandler(authHandler, server.MaxRequestBytes)},
		{Pattern: auditPath, Handler: http.MaxBytesHandler(auditHandler, server.MaxRequestBytes)},
		{Pattern: connsPath, Handler: http.MaxBytesHandler(connsHandler, server.MaxRequestBytes)},
		{Pattern: policiesPath, Handler: http.MaxBytesHandler(policiesHandler, server.MaxRequestBytes)},
		{Pattern: requestsPath, Handler: http.MaxBytesHandler(requestsHandler, server.MaxRequestBytes)},
		{Pattern: executionsPath, Handler: http.MaxBytesHandler(connectapi.BoundCSVWrites(executionsHandler), server.MaxRequestBytes)},
	}

	// Google login (ADR-0007) mounts only when configured: provider discovery must succeed at boot (fail-fast, like the keyring), and when disabled the routes simply don't exist. The redirect flow is plain HTTP, not Connect.
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
	go func() {
		ticker := time.NewTicker(access.ExecutionReconcileInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				maintenanceCtx, cancel := context.WithTimeout(ctx, access.ExecutionReconcileTimeout)
				summary, err := executionSvc.Reconcile(maintenanceCtx)
				logExecutionRecovery(logger, summary, err)
				org, err := requestStore.DefaultOrganizationID(maintenanceCtx)
				if err == nil {
					err = resultStore.PurgeExpired(maintenanceCtx, org)
				}
				if err != nil {
					logger.Error("result cleanup failed")
				}
				cancel()
			}
		}
	}()

	// A bind/serve failure must terminate the process with a non-zero exit, not fall through to the normal shutdown path.
	serveErr := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		// Restore default signal handling now that the first signal is being handled, so a SECOND SIGINT/SIGTERM force-quits during a long drain instead of being swallowed by NotifyContext (os/signal: stop as soon as the first signal is handled). The deferred stop() still covers the serveErr path.
		stop()
		logger.Info("shutting down")
	case err := <-serveErr:
		logger.Error("server error", "err", err)
		return err
	}

	// Background, not a timeout: the drain delay and the shutdown timeout are sequential budgets (ADR-0010) — Server.Shutdown applies the timeout to the drain of in-flight requests only, after the delay has fully elapsed.
	executionSvc.StopAdmission()
	if err := srv.Shutdown(context.Background(), cfg.ShutdownTimeout); err != nil {
		logger.Error("graceful shutdown failed", "err", err)
		return err
	}
	return nil
}

// logExecutionRecovery logs a reconciliation run with classified causes only, since raw database errors can echo row data.
func logExecutionRecovery(logger *slog.Logger, summary access.ReconcileSummary, err error) {
	if err != nil {
		logger.Error("execution recovery failed", append([]any{"recovered", summary.Recovered}, postgres.ErrorLogFields(err)...)...)
		return
	}
	if summary.Failed > 0 {
		logger.Warn("execution recovery left failing attempts for the next run", append([]any{"recovered", summary.Recovered, "skipped", summary.Skipped, "failed", summary.Failed}, postgres.ErrorLogFields(summary.FirstFailure)...)...)
		return
	}
	if summary.Recovered > 0 {
		logger.Info("execution recovery recorded unknown outcomes", "recovered", summary.Recovered, "skipped", summary.Skipped)
	}
}
