package pgdialect

import (
	"context"
	"io"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgconn/ctxwatch"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/query"
)

// Execute uses a dedicated transaction, READ ONLY for reads, and commits only on clean uncancelled completion. Cancellation sends a server-side cancel; callers classify uncertain outcomes (PRD §8.2).
func (d *Dialect) Execute(ctx context.Context, target connection.Target, mode connection.TLSMode, cred connection.Credential, exec query.Execution) (query.ResultStream, error) {
	if !exec.Class.Valid() {
		return nil, &query.Rejection{Reason: query.RejectNotAllowlisted}
	}
	var parsed query.Statement
	if exec.Governed {
		var parseErr error
		parsed, parseErr = d.ParseSingle(exec.SQL)
		if parseErr != nil {
			return nil, parseErr
		}
		class, classErr := d.Classify(parsed)
		if classErr != nil {
			return nil, classErr
		}
		if class != exec.Class || exec.MaxRows < 1 || exec.MaxRows > 10000 || exec.MaxResultBytes < 1 || exec.MaxResultBytes > query.MaxSnapshotBytes || exec.TimeoutSeconds < 1 || exec.TimeoutSeconds > 300 {
			return nil, &query.Rejection{Reason: query.RejectNotAllowlisted}
		}
	}
	args, err := encodeArgs(exec.Args)
	if err != nil {
		return nil, err
	}

	cfg, err := buildConfig(target, mode, cred, d.validateTimeout)
	if err != nil {
		return nil, &connection.TestError{Bucket: connection.TestBucketFailed}
	}
	// Attempt a driver cancel on ctx cancellation (PRD §8.2) instead of only killing the socket (the pgconn default); the deadline is the fallback when the out-of-band cancel connection itself hangs.
	cfg.BuildContextWatcherHandler = func(pc *pgconn.PgConn) ctxwatch.Handler {
		return &pgconn.CancelRequestContextWatcherHandler{Conn: pc, DeadlineDelay: 2 * time.Second}
	}
	// Deterministic renderings for the ADR-0005 canonical cell texts; buildConfig cleared RuntimeParams, so these are the only session knobs. client_encoding is pinned to UTF8 so text values are UTF-8 regardless of the target database's server encoding (PG transcodes on the wire) — the result grid and CSV/JSON export (ADR-0005) assume UTF-8.
	cfg.RuntimeParams = map[string]string{
		"client_encoding": "UTF8",
		"TimeZone":        "UTC",
		"DateStyle":       "ISO",
		"bytea_output":    "hex",
	}

	if exec.Governed {
		cfg.RuntimeParams["search_path"] = "pg_catalog,public"
		cfg.RuntimeParams["statement_timeout"] = strconv.Itoa(exec.TimeoutSeconds * 1000)
		cfg.RuntimeParams["idle_in_transaction_session_timeout"] = "60000"
		cfg.BuildFrontend = func(reader io.Reader, writer io.Writer) *pgproto3.Frontend {
			frontend := pgproto3.NewFrontend(reader, writer)
			frontend.SetMaxBodyLen(int(exec.MaxResultBytes) + 65536)
			return frontend
		}
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

	if exec.Governed {
		if err := validateCatalog(ctx, conn, parsed); err != nil {
			closeConn(ctx, conn)
			return nil, err
		}
	}
	stream := &resultStream{ctx: ctx, conn: conn, maxRows: exec.MaxRows, maxBytes: exec.MaxResultBytes}
	stream.rr = conn.ExecParams(ctx, exec.SQL, args, parameterOIDs(exec.Args), nil, nil)

	// Read ahead one row: pgconn learns the row description on the first read, and an immediately failing statement concludes here rather than handing the caller a stream that was never going to produce anything.
	if stream.rr.NextRow() {
		stream.columns = describeColumns(stream.rr.FieldDescriptions())
		if stream.acceptRow(stream.rr.Values()) {
			stream.pending = decodeRow(stream.columns, stream.rr.Values())
			stream.hasPending = true
		}
		return stream, nil
	}
	// Copy column descriptions before closing pgconn’s reader, which owns their backing buffer. Return early failures directly; mid-stream failures remain on ResultStream.Err.
	stream.columns = describeColumns(stream.rr.FieldDescriptions())
	tag, rrErr := stream.rr.Close()
	stream.conclude(tag, rrErr)
	if stream.err != nil {
		return nil, stream.err
	}
	return stream, nil
}

// encodeArgs renders text-format parameters; parameterOIDs supplies their types.
func encodeArgs(args []query.TypedValue) ([][]byte, error) {
	if len(args) == 0 {
		return nil, nil
	}
	out := make([][]byte, len(args))
	for idx, arg := range args {
		if err := arg.Validate(); err != nil {
			return nil, err
		}
		if arg.Type == query.ParamNull {
			out[idx] = nil
			continue
		}
		out[idx] = []byte(arg.Text)
	}
	return out, nil
}

// parameterOIDs preserves declared types, leaving null types to PostgreSQL inference.
func parameterOIDs(args []query.TypedValue) []uint32 {
	if len(args) == 0 {
		return nil
	}
	oids := make([]uint32, len(args))
	for idx, arg := range args {
		switch arg.Type {
		case query.ParamString:
			oids[idx] = pgtype.TextOID
		case query.ParamInteger:
			oids[idx] = pgtype.Int8OID
		case query.ParamDecimal:
			oids[idx] = pgtype.NumericOID
		case query.ParamBoolean:
			oids[idx] = pgtype.BoolOID
		case query.ParamDate:
			oids[idx] = pgtype.DateOID
		case query.ParamTimestamp:
			oids[idx] = pgtype.TimestamptzOID
		case query.ParamUUID:
			oids[idx] = pgtype.UUIDOID
		}
	}
	return oids
}

// runSimple executes one fixed protocol statement (BEGIN/COMMIT/ROLLBACK — never user SQL) over the simple protocol.
func runSimple(ctx context.Context, conn *pgconn.PgConn, sql string) error {
	_, err := conn.Exec(ctx, sql).ReadAll()
	return err
}

// closeConn closes the dedicated connection on a bounded context detached from the (possibly canceled) execution context. Closing with an open transaction rolls it back server-side.
func closeConn(ctx context.Context, conn *pgconn.PgConn) {
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	_ = conn.Close(closeCtx)
}
