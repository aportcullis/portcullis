package pgdialect

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgconn/ctxwatch"

	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/query"
)

// Execute runs one bound, classified statement on a dedicated connection
// (PRD §8.2): read inside BEGIN READ ONLY, write/ddl inside a transaction
// that commits only on clean, uncanceled completion and rolls back
// otherwise. The caller's ctx is the only execution bound; cancellation
// sends a server-side cancel request before the socket deadline fires.
// Outcome interpretation (outcome_unknown) is the caller's job.
func (d *Dialect) Execute(ctx context.Context, target connection.Target, mode connection.TLSMode, cred connection.Credential, exec query.Execution) (query.ResultStream, error) {
	if !exec.Class.Valid() {
		return nil, &query.Rejection{Reason: query.RejectNotAllowlisted}
	}
	args, err := encodeArgs(exec.Args)
	if err != nil {
		return nil, err
	}

	cfg, err := buildConfig(target, mode, cred, d.validateTimeout)
	if err != nil {
		return nil, &connection.TestError{Bucket: connection.TestBucketFailed}
	}
	// Attempt a driver cancel on ctx cancellation (PRD §8.2) instead of only
	// killing the socket (the pgconn default); the deadline is the fallback
	// when the out-of-band cancel connection itself hangs.
	cfg.BuildContextWatcherHandler = func(pc *pgconn.PgConn) ctxwatch.Handler {
		return &pgconn.CancelRequestContextWatcherHandler{Conn: pc, DeadlineDelay: 2 * time.Second}
	}
	// Deterministic renderings for the ADR-0005 canonical cell texts;
	// buildConfig cleared RuntimeParams, so these are the only session knobs.
	// client_encoding is pinned to UTF8 so text values are UTF-8 regardless of
	// the target database's server encoding (PG transcodes on the wire) — the
	// result grid and CSV/JSON export (ADR-0005) assume UTF-8.
	cfg.RuntimeParams = map[string]string{
		"client_encoding": "UTF8",
		"TimeZone":        "UTC",
		"DateStyle":       "ISO",
		"bytea_output":    "hex",
	}

	dialCtx, cancelDial := context.WithTimeout(ctx, d.validateTimeout)
	defer cancelDial()
	conn, err := pgconn.ConnectConfig(dialCtx, cfg)
	if err != nil {
		// Connection-phase failures leak no more than a connection test does.
		return nil, classify(err)
	}

	begin := "BEGIN"
	if exec.Class == query.ClassRead {
		begin = "BEGIN READ ONLY"
	}
	if err := runSimple(ctx, conn, begin); err != nil {
		closeConn(ctx, conn)
		return nil, redactExecError(ctx, err)
	}

	stream := &resultStream{ctx: ctx, conn: conn}
	stream.rr = conn.ExecParams(ctx, exec.SQL, args, nil, nil, nil)

	// Read ahead one row: pgconn learns the row description on the first
	// read, and an immediately failing statement concludes here rather than
	// handing the caller a stream that was never going to produce anything.
	if stream.rr.NextRow() {
		stream.columns = describeColumns(stream.rr.FieldDescriptions())
		stream.pending = decodeRow(stream.columns, stream.rr.Values())
		stream.hasPending = true
		return stream, nil
	}
	// No first row: the statement either produced no rows (write/ddl, or an
	// empty read) or failed before any row. Capture the row description
	// BEFORE closing the reader — pgconn's slice is only valid until Close
	// (it aliases a per-connection buffer). conclude then ends the
	// transaction; a failure detectable here is returned as the error — like
	// the dial/BEGIN paths — so a caller need not drain to learn the
	// statement was rejected. (Failures that only appear mid-iteration still
	// surface via ResultStream.Err, which is unavoidable.)
	stream.columns = describeColumns(stream.rr.FieldDescriptions())
	tag, rrErr := stream.rr.Close()
	stream.conclude(tag, rrErr)
	if stream.err != nil {
		return nil, stream.err
	}
	return stream, nil
}

// encodeArgs renders typed values as PostgreSQL text-format parameters;
// types are resolved by the server from context (casts in the bound SQL).
func encodeArgs(args []query.TypedValue) ([][]byte, error) {
	if len(args) == 0 {
		return nil, nil
	}
	out := make([][]byte, len(args))
	for i, arg := range args {
		if err := arg.Validate(); err != nil {
			return nil, err
		}
		if arg.Type == query.ParamNull {
			out[i] = nil
			continue
		}
		out[i] = []byte(arg.Text)
	}
	return out, nil
}

// runSimple executes one fixed protocol statement (BEGIN/COMMIT/ROLLBACK —
// never user SQL) over the simple protocol.
func runSimple(ctx context.Context, conn *pgconn.PgConn, sql string) error {
	_, err := conn.Exec(ctx, sql).ReadAll()
	return err
}

// closeConn closes the dedicated connection on a bounded context detached
// from the (possibly canceled) execution context. Closing with an open
// transaction rolls it back server-side.
func closeConn(ctx context.Context, conn *pgconn.PgConn) {
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	_ = conn.Close(closeCtx)
}
