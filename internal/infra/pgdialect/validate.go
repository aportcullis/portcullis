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

// defaultTestTimeout is the safety net when the wired config value is absent;
// the real default and bounds live in platform/config (ADR-0014: 10s, [1s,60s]).
const defaultTestTimeout = 10 * time.Second

// ValidateConnection dials the target, authenticates, and runs one ping
// round-trip, then disconnects — the connection app service's
// ConnectionValidator port (PRD §5.3, ADR-0014). Every failure is classified
// into a caller-safe bucket (connerrors.go) — raw driver errors never leave
// this package (PRD §8.1). A nil return means the target accepted the
// credential and the database exists.
func (d *Dialect) ValidateConnection(ctx context.Context, target connection.Target, mode connection.TLSMode, cred connection.Credential) error {
	cfg, err := buildConfig(target, mode, cred, d.validateTimeout)
	if err != nil {
		// Config assembly failed before any dial; nothing target-specific to
		// classify, and the raw error must not leak.
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

// buildConfig assembles the pgconn config structurally (ADR-0014): the parsed
// connection string carries only the non-secret coordinates and sslmode — pgx
// owns the sslmode→TLS mapping, verified against its v5 source — while the
// credential is injected directly into the config fields, so no parse error,
// log line, or driver message can ever embed it.
//
// A test must depend on NOTHING but the stored descriptor and credential:
// ParseConfig merges PG* environment variables (and, via PGSERVICE, an external
// service file) for any setting the connection string does not carry, which
// would let the server process's environment silently change — or fail — what a
// "connection test" means (a PGCHANNELBINDING=require or a dangling
// PGSSLROOTCERT alone flips a good target to a failure). The connection string
// wins the settings merge over both env and service files, so every
// result-affecting key is pinned here to an explicit safe default; the few
// leftovers that have no connection-string key (application_name, options,
// timezone → RuntimeParams; target_session_attrs → ValidateConnect) are cleared
// on the parsed struct. Nothing outside this function can alter the outcome.
func buildConfig(target connection.Target, mode connection.TLSMode, cred connection.Credential, timeout time.Duration) (*pgconn.Config, error) {
	values := url.Values{
		// sslmode drives the TLS decision (ADR-0014); the rest of the TLS
		// material is pinned empty so no env/service file supplies a cert.
		"sslmode":        []string{string(mode)},
		"sslrootcert":    []string{""},
		"sslcert":        []string{""},
		"sslkey":         []string{""},
		"sslpassword":    []string{""},
		"sslsni":         []string{"1"},
		"sslnegotiation": []string{"postgres"},
		// Authentication negotiation: accept any method the server offers,
		// pgx's default channel-binding preference, and the baseline protocol
		// — the test asks only "does this credential reach this database".
		"channel_binding":      []string{"prefer"},
		"require_auth":         []string{""},
		"target_session_attrs": []string{"any"},
		"min_protocol_version": []string{"3.0"},
		"max_protocol_version": []string{"3.0"},
		// connect_timeout is pinned so an invalid PGCONNECT_TIMEOUT cannot fail
		// the parse (the connection string wins the merge before the value is
		// parsed — pgx v5 source); ConnectTimeout is re-set precisely below.
		"connect_timeout": []string{strconv.Itoa(int(math.Ceil(timeout.Seconds())))},
		// ParseConfig OPENS the passfile unconditionally (the password checks
		// only gate whether the result is used — pgx v5 source), and config
		// assembly runs before any timeout context: an unreadable or blocking
		// PGPASSFILE (or ~/.pgpass default) must never reach it. The credential
		// is injected directly below, so the passfile can contribute nothing.
		"passfile": []string{os.DevNull},
	}
	// PGSERVICE cannot be pinned by VALUE: ParseConfig triggers the service
	// lookup whenever the merged settings CONTAIN the key (presence check, pgx
	// v5 source), so a dangling service/servicefile in the environment fails
	// the parse of an otherwise perfect connection string — and building the
	// Config by hand is not an option (ConnectConfig only accepts a ParseConfig
	// product). Isolation instead: point servicefile (connection string wins
	// over PGSERVICEFILE) at a generated file containing just an empty section
	// for that service name — the lookup succeeds and contributes nothing.
	if svc := os.Getenv("PGSERVICE"); svc != "" {
		path, cleanup, err := emptyServiceFile(svc)
		if err != nil {
			// A service name an INI section cannot express (or a tempdir
			// failure): the environment cannot be neutralized — fail closed as
			// before (documented limitation, ADR-0014).
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
	// Take host/port/database from the VALIDATED target, not from the URI's
	// parsed values: pgx splits a comma-bearing host into multiple hosts and
	// strips leading slashes from the database path, so a stored descriptor
	// like "a,b" or "/db" would otherwise dial a different target than the one
	// recorded and audited (ADR-0014; pgx v5 parseURLSettings). Setting the
	// fields directly makes the actual connection exactly the stored descriptor.
	cfg.Host = target.Host
	cfg.Port = target.Port
	cfg.Database = target.DatabaseName
	// The accepted modes (ADR-0014) have no TLS fallback chain; drop anything a
	// PG* env var could have smuggled in so the chosen mode is exactly what runs.
	cfg.Fallbacks = nil
	// PGOPTIONS/PGAPPNAME/PGTZ land here; the test needs no session parameters.
	cfg.RuntimeParams = map[string]string{}
	// Defensive: target_session_attrs=any above already leaves this nil.
	cfg.ValidateConnect = nil
	return cfg, nil
}

// emptyServiceFile writes a throwaway pg service file whose only content is an
// empty INI section for name, so a PGSERVICE lookup resolves without
// contributing a single setting. The caller removes it after ParseConfig.
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

// errServiceNameNotNeutralizable marks a PGSERVICE name an INI section cannot
// express; the tester fails closed rather than let the environment's service
// file decide the outcome.
var errServiceNameNotNeutralizable = errors.New("pgdialect: PGSERVICE name cannot be neutralized")
