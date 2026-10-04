package pgdialect

import (
	"context"
	"errors"
	"math"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/aportcullis/portcullis/internal/domain/connection"
)

// defaultTestTimeout is the safety net when the wired config value is absent; the real default and bounds live in platform/config (ADR-0014: 10s, [1s,60s]).
const defaultTestTimeout = 10 * time.Second

// ValidateConnection authenticates and pings the stored target, then disconnects. Failures expose only safe classifications (ADR-0014).
func (d *Dialect) ValidateConnection(ctx context.Context, target connection.Target, mode connection.TLSMode, cred connection.Credential) error {
	cfg, err := d.buildGuardedConfig(target, mode, cred)
	if err != nil {
		// Config assembly failed before any dial; nothing target-specific to classify, and the raw error must not leak.
		return &connection.TestError{Bucket: connection.TestBucketFailed}
	}
	ctx, cancel := context.WithTimeout(ctx, d.validateTimeout)
	defer cancel()
	conn, err := pgconn.ConnectConfig(ctx, cfg)
	if err != nil {
		return classify(err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer closeCancel()
		_ = conn.Close(closeCtx)
	}()
	if err := conn.Ping(ctx); err != nil {
		return classify(err)
	}
	return nil
}

// buildConfig pins connection settings against PG* environment and service-file overrides, then injects credentials outside the connection string (ADR-0014).
func buildConfig(target connection.Target, mode connection.TLSMode, cred connection.Credential, timeout time.Duration) (*pgconn.Config, error) {
	values := url.Values{
		// sslmode drives the TLS decision (ADR-0014); the rest of the TLS material is pinned empty so no env/service file supplies a cert.
		"sslmode":        []string{string(mode)},
		"sslrootcert":    []string{""},
		"sslcert":        []string{""},
		"sslkey":         []string{""},
		"sslpassword":    []string{""},
		"sslsni":         []string{"1"},
		"sslnegotiation": []string{"postgres"},
		// Authentication negotiation: accept any method the server offers, pgx's default channel-binding preference, and the baseline protocol — the test asks only "does this credential reach this database".
		"channel_binding":      []string{"prefer"},
		"require_auth":         []string{""},
		"target_session_attrs": []string{"any"},
		"min_protocol_version": []string{"3.0"},
		"max_protocol_version": []string{"3.0"},
		// connect_timeout is pinned so an invalid PGCONNECT_TIMEOUT cannot fail the parse (the connection string wins the merge before the value is parsed — pgx v5 source); ConnectTimeout is re-set precisely below.
		"connect_timeout": []string{strconv.Itoa(int(math.Ceil(timeout.Seconds())))},
		// ParseConfig OPENS the passfile unconditionally (the password checks only gate whether the result is used — pgx v5 source), and config assembly runs before any timeout context: an unreadable or blocking PGPASSFILE (or ~/.pgpass default) must never reach it. The credential is injected directly below, so the passfile can contribute nothing.
		"passfile": []string{os.DevNull},
	}
	// An empty service section neutralizes PGSERVICE overrides because pgx triggers lookup on key presence, even for an empty value.
	if svc := os.Getenv("PGSERVICE"); svc != "" {
		path, cleanup, err := emptyServiceFile(svc)
		if err != nil {
			// A service name an INI section cannot express (or a tempdir failure): the environment cannot be neutralized — fail closed as before (documented limitation, ADR-0014).
			return nil, err
		}
		defer cleanup()
		values["servicefile"] = []string{path}
	}
	u := url.URL{
		Scheme:   "postgres",
		Host:     net.JoinHostPort(target.Host, strconv.Itoa(int(target.Port))),
		Path:     "/" + target.DatabaseName,
		RawQuery: values.Encode(),
	}
	cfg, err := pgconn.ParseConfig(u.String())
	if err != nil {
		return nil, err
	}
	cfg.User = cred.User
	cfg.Password = cred.Password
	cfg.ConnectTimeout = timeout
	// Use validated target fields directly because pgx URI parsing can split comma-bearing hosts or strip database-path slashes.
	cfg.Host = target.Host
	cfg.Port = target.Port
	cfg.Database = target.DatabaseName
	// The accepted modes (ADR-0014) have no TLS fallback chain; drop anything a PG* env var could have smuggled in so the chosen mode is exactly what runs.
	cfg.Fallbacks = nil
	// PGOPTIONS/PGAPPNAME/PGTZ land here; the test needs no session parameters.
	cfg.RuntimeParams = map[string]string{}
	// Defensive: target_session_attrs=any above already leaves this nil.
	cfg.ValidateConnect = nil
	return cfg, nil
}

// emptyServiceFile writes a throwaway pg service file whose only content is an empty INI section for name, so a PGSERVICE lookup resolves without contributing a single setting. The caller removes it after ParseConfig.
func emptyServiceFile(name string) (path string, cleanup func(), _ error) {
	if strings.ContainsAny(name, "[]\n\r") {
		return "", nil, errServiceNameNotNeutralizable
	}
	f, err := os.CreateTemp("", "portcullis-pgservice-*.conf")
	if err != nil {
		return "", nil, err
	}
	if _, err := f.WriteString("[" + name + "]\n"); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", nil, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", nil, err
	}
	return f.Name(), func() { _ = os.Remove(f.Name()) }, nil
}

// errServiceNameNotNeutralizable marks a PGSERVICE name an INI section cannot express; the tester fails closed rather than let the environment's service file decide the outcome.
var errServiceNameNotNeutralizable = errors.New("pgdialect: PGSERVICE name cannot be neutralized")
