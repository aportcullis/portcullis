package postgres

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// safeError marks an error whose entire message is crafted in this package — operator guidance with no wrapped driver error — so it can never echo the DSN and ErrorLogFields may log it verbatim.
type safeError struct{ err error }

func (e safeError) Error() string { return e.err.Error() }
func (e safeError) Unwrap() error { return e.err }

// isSafeError reports whether e is safe to interpolate into a safeError message: it is itself a safeError, or the package's own DSN-free sentinel. Any other error (a pgconn/connect error) can echo the DSN in its message.
func isSafeError(e error) bool {
	var se safeError
	return errors.As(e, &se) || errors.Is(e, ErrRuntimeInsecure)
}

// safeErrorf replaces unsafe error arguments with their Go types before formatting so crafted log messages cannot embed driver secrets.
func safeErrorf(format string, args ...any) error {
	for idx, a := range args {
		if e, ok := a.(error); ok && !isSafeError(e) {
			args[idx] = fmt.Sprintf("%T", e)
		}
	}
	return safeError{fmt.Errorf(format, args...)}
}

// ErrorLogFields logs safe application messages or PostgreSQL codes and structural identifiers. Other errors expose only their Go type because messages may leak credentials or row data.
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
