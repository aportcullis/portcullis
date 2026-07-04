package postgres

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// safeError marks an error whose entire message is crafted in this package —
// operator guidance with no wrapped driver error — so it can never echo the DSN
// and ErrorLogFields may log it verbatim.
type safeError struct{ err error }

func (e safeError) Error() string { return e.err.Error() }
func (e safeError) Unwrap() error { return e.err }

// isSafeError reports whether e is safe to interpolate into a safeError message: it
// is itself a safeError, or the package's own DSN-free sentinel. Any other error (a
// pgconn/connect error) can echo the DSN in its message.
func isSafeError(e error) bool {
	var se safeError
	return errors.As(e, &se) || errors.Is(e, ErrRuntimeInsecure)
}

// safeErrorf builds a crafted, DSN-free error that ErrorLogFields logs verbatim.
// The DSN-safety is enforced structurally, not by convention: any error argument
// that is not itself safe (a raw query/connect error whose message can carry the
// DSN) is flattened to its Go type before formatting, so a stray `%w`/`%s`/`%v` of
// such an error can never leak into the logs.
func safeErrorf(format string, args ...any) error {
	for i, a := range args {
		if e, ok := a.(error); ok && !isSafeError(e) {
			args[i] = fmt.Sprintf("%T", e)
		}
	}
	return safeError{fmt.Errorf(format, args...)}
}

// ErrorLogFields turns a database-layer error into slog key/values that are safe
// to log. Errors this package crafted (safeErrorf) carry actionable operator
// guidance and no DSN, so their message is logged as "reason". For a
// *pgconn.PgError it logs the SQLSTATE code plus the structural identifier
// fields (schema/table/column/constraint/datatype) — never Message or Detail,
// which are free, localizable text that can echo input values, existing rows, or a
// RAISE payload (e.g. a unique_violation Detail is "Key (email)=(secret) ...").
// PostgreSQL supplies those identifiers separately so callers don't parse the text,
// and recommends branching on the code. Any other error (a *pgconn.ConnectError,
// a network or context error) can echo the DSN/host, so only its Go type is logged.
func ErrorLogFields(err error) []any {
	var safe safeError
	if errors.As(err, &safe) {
		return []any{"reason", safe.Error()}
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return []any{"error_type", fmt.Sprintf("%T", err)}
	}
	fields := []any{"pg_code", pgErr.Code}
	for _, f := range []struct {
		key, val string
	}{
		{"schema", pgErr.SchemaName},
		{"table", pgErr.TableName},
		{"column", pgErr.ColumnName},
		{"constraint", pgErr.ConstraintName},
		{"datatype", pgErr.DataTypeName},
	} {
		if f.val != "" {
			fields = append(fields, f.key, f.val)
		}
	}
	return fields
}
