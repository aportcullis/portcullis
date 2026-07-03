package postgres

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrorLogFields turns a pgx error into slog key/values that are safe to log.
// For a *pgconn.PgError it logs the SQLSTATE code plus the structural identifier
// fields (schema/table/column/constraint/datatype) — never Message or Detail,
// which are free, localizable text that can echo input values, existing rows, or a
// RAISE payload (e.g. a unique_violation Detail is "Key (email)=(secret) ...").
// PostgreSQL supplies those identifiers separately so callers don't parse the text,
// and recommends branching on the code. Any other error (a *pgconn.ConnectError,
// a network or context error) can echo the DSN/host, so only its Go type is logged.
func ErrorLogFields(err error) []any {
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
