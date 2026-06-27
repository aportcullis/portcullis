# ADR-0005: CellValue / ColumnMeta wire contract

- **Status:** Accepted — wire contract and sort/filter/CSV rules fixed. Per-engine scan-type → LogicalType mapping tables filled and fixture-pinned during adapter work.
- **Date:** 2026-06-27

## Context
The result grid is a core differentiator: it streams arbitrary result cells from three engines
(PostgreSQL, MySQL, SQLite) to a browser client over Connect RPC (protobuf), and then sorts,
filters, paginates, and exports them as CSV — all over a single cached snapshot. This requires a
faithful, lossless, type-aware representation of any cell. The contract must be fixed **before**
the API is frozen, because every downstream behavior (grid rendering, sort/filter semantics, CSV
serialization) depends on it.

Hard requirements:
- Represent `null`, `string`, `bool`, `bytes`.
- **64-bit integers cross a JavaScript precision boundary** (`Number` loses precision past 2^53)
  → carry as string.
- **`decimal` must never become a float** → carry as string, exact.
- **Distinguish `date` from a timezone-aware `timestamp`**, and from a naive timestamp.
- `JSON`, arrays, and DB-specific types need a defined fallback.
- Per-type **sort / filter / CSV serialization** rules, including spreadsheet **formula-injection**
  escaping for CSV.

## Decision

### Proto shape
```proto
message ColumnMeta {
  string name = 1;
  LogicalType logical_type = 2;   // engine-agnostic bucket used for sort/filter/render
  string db_type_name = 3;        // raw engine type, e.g. "numeric", "jsonb", "bigint unsigned"
  bool nullable = 4;
}

enum LogicalType {
  LOGICAL_TYPE_UNSPECIFIED = 0;
  STRING = 1; BOOL = 2; INT = 3; DECIMAL = 4; FLOAT = 5; BYTES = 6;
  DATE = 7; TIME = 8; TIMESTAMP = 9; TIMESTAMPTZ = 10;
  JSON = 11; UUID = 12; ARRAY = 13; UNKNOWN = 14;
}

message CellValue {
  oneof kind {
    bool   is_null      = 1;   // always true when set
    string string_value = 2;   // STRING, UUID, JSON(raw text), ARRAY(raw text), UNKNOWN
    bool   bool_value   = 3;
    string int_value    = 4;   // INT (int64/uint64) as decimal string — JS-safe
    string decimal_value= 5;   // DECIMAL exact, as string
    double double_value = 6;   // FLOAT (float64 fits JS Number)
    bytes  bytes_value  = 7;   // BYTES
    string temporal_value = 8; // DATE/TIME/TIMESTAMP/TIMESTAMPTZ as canonical ISO-8601 text;
                               // TIMESTAMPTZ normalized to UTC with offset, DATE as YYYY-MM-DD
  }
}
```
- `LogicalType` is what the grid uses; `db_type_name` is preserved for display and for the
  redacted audit/EXPLAIN context. Unknown engine types map to `UNKNOWN` and travel as
  `string_value` (never silently coerced).
- A row is a repeated `CellValue`; column identity comes from the parallel `ColumnMeta` list.

### Sort / filter semantics (server-side over the snapshot)
- `INT`/`DECIMAL` compare numerically on the exact string; `FLOAT` numerically as double.
- `STRING`/`UUID`/`JSON`/`ARRAY`/`UNKNOWN` compare lexically (byte order, UTF-8).
- Temporal types compare on the normalized instant/value.
- `BOOL` false < true. `BYTES` lexicographic.
- **NULL ordering is explicit and stable** (nulls last by default) and combined with the
  pagination tie-breaker so page boundaries are stable.

### CSV serialization
- Each cell → text: `is_null` → empty field; `bytes` → base64; `JSON`/`ARRAY` → raw text;
  temporal → the canonical ISO string; numbers → their string form.
- **Formula-injection escape:** any field whose first character is `=`, `+`, `-`, `@`, tab, or CR
  is prefixed with a single quote by default. A raw (un-escaped) export is a separate explicit
  option with a warning.
- RFC-4180 quoting; UTF-8 encoding; embedded newlines preserved inside quoted fields.

## Consequences
- The proto is generated once into Go and TS, giving end-to-end types. Adapters map each engine's
  driver values into `CellValue` at execution time; the grid and CSV paths consume only the
  logical type, so they are engine-agnostic.
- Lossless-by-default: anything not confidently typed becomes `UNKNOWN`+string rather than a lossy
  cast, matching the fail-closed posture elsewhere.
- Open item: per-engine mapping tables (driver scan type → `LogicalType`) are filled during the
  adapter work and pinned with fixtures alongside the classification suite.
