package query

// LogicalType is the engine-agnostic type bucket a column maps to (ADR-0005). It drives grid rendering, sort/filter semantics, and CSV serialization; the raw engine type is preserved in Column.DBTypeName.
type LogicalType string

const (
	LogicalString      LogicalType = "string"
	LogicalBool        LogicalType = "bool"
	LogicalInt         LogicalType = "int"
	LogicalDecimal     LogicalType = "decimal"
	LogicalFloat       LogicalType = "float"
	LogicalBytes       LogicalType = "bytes"
	LogicalDate        LogicalType = "date"
	LogicalTime        LogicalType = "time"
	LogicalTimestamp   LogicalType = "timestamp"
	LogicalTimestamptz LogicalType = "timestamptz"
	LogicalJSON        LogicalType = "json"
	LogicalUUID        LogicalType = "uuid"
	LogicalArray       LogicalType = "array"
	LogicalUnknown     LogicalType = "unknown"
)

// Column describes one result column (ADR-0005 ColumnMeta).
type Column struct {
	Name       string
	Logical    LogicalType
	DBTypeName string
	// Nullable is true when nullability is unknown: the wire protocol does not carry it, so the execution path reports every column as nullable.
	Nullable bool
}

// CellKind selects which CellValue field carries the value (ADR-0005 CellValue oneof).
type CellKind uint8

const (
	CellNull CellKind = iota
	CellString
	CellBool
	CellInt
	CellDecimal
	CellFloat
	CellBytes
	CellTemporal
)

// CellValue is one result cell. Int, Decimal, and Temporal values travel in Text (int64 crosses the JS precision boundary, decimal must stay exact, temporals are canonical ISO-8601 — ADR-0005); Unknown types travel as the engine's lossless text rendering with Kind CellString.
type CellValue struct {
	Kind  CellKind
	Bool  bool
	Text  string
	Float float64
	Bytes []byte
}

// ResultStream iterates one execution's result. Row is valid only until the next call to Next. RowsAffected is meaningful after Next has returned false with a nil Err, for write/ddl statements. Close is idempotent and must be called; it releases the dedicated connection.
type ResultStream interface {
	Columns() []Column
	Next() bool
	Row() []CellValue
	Err() error
	RowsAffected() int64
	Close() error
}
