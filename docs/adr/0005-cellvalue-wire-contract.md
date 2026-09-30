# ADR-0005: CellValue / ColumnMeta wire contract

- **Status:** Accepted — wire contract and sort/filter/CSV rules fixed.
  (Amended 2026-07-04: per-engine scan-type → LogicalType mapping tables and the NULL-ordering / tie-breaker rules are pinned; adapter work pins them with fixtures, it no longer designs them.)
- **Date:** 2026-06-27 (amended 2026-07-04)

## Context
The result grid is a core differentiator: it streams arbitrary result cells from three engines (PostgreSQL, MySQL, SQLite) to a browser client over Connect RPC (protobuf), and then sorts, filters, paginates, and exports them as CSV — all over a single cached snapshot.
This requires a faithful, lossless, type-aware representation of any cell.
The contract must be fixed **before** the API is frozen, because every downstream behavior (grid rendering, sort/filter semantics, CSV serialization) depends on it.

Hard requirements:
- Represent `null`, `string`, `bool`, `bytes`.
- **64-bit integers cross a JavaScript precision boundary** (`Number` loses precision past 2^53) → carry as string.
- **`decimal` must never become a float** → carry as string, exact.
- **Distinguish `date` from a timezone-aware `timestamp`**, and from a naive timestamp.
- `JSON`, arrays, and DB-specific types need a defined fallback.
- Per-type **sort / filter / CSV serialization** rules, including spreadsheet **formula-injection** escaping for CSV.

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
- `LogicalType` is what the grid uses; `db_type_name` is preserved for display and for the redacted audit/EXPLAIN context.
  Unknown engine types map to `UNKNOWN` and travel as `string_value` (never silently coerced).
- A row is a repeated `CellValue`; column identity comes from the parallel `ColumnMeta` list.

### Sort / filter semantics (server-side over the snapshot)
- `INT`/`DECIMAL` compare numerically on the exact string; `FLOAT` numerically as double.
- `STRING`/`UUID`/`JSON`/`ARRAY`/`UNKNOWN` compare lexically (byte order, UTF-8).
- Temporal types compare on the normalized instant/value.
- `BOOL` false < true.
  `BYTES` lexicographic.
- **NULL ordering is fixed: nulls sort last in both directions** (ASC and DESC), not configurable in the MVP — one rule, no per-request surface.
- **Pagination tie-breaker: the snapshot row ordinal** (the row's position in the original result order, stored per row), ascending, appended to every sort.
  Page boundaries are therefore stable across identical requests, and "unsorted" is exactly the original result order.

### CSV serialization
- Each cell → text: `is_null` → empty field; `bytes` → base64; `JSON`/`ARRAY` → raw text; temporal → the canonical ISO string; numbers → their string form.
- **Formula-injection escape:** any field whose first character is `=`, `+`, `-`, `@`, tab, or CR is prefixed with a single quote by default.
  A raw (un-escaped) export is a separate explicit option with a warning.
- RFC-4180 quoting; UTF-8 encoding; embedded newlines preserved inside quoted fields.

## Consequences
- Pin protobuf-go, Connect Go, and protobuf-es generator versions in `buf.gen.yaml` to their corresponding runtime dependencies.
  Regeneration must not implicitly upgrade plugins; review runtime, generator, and output changes together.
  See [Buf remote plugin versioning](https://buf.build/docs/configuration/v2/buf-gen-yaml/#plugins).
- The proto is generated once into Go and TS, giving end-to-end types.
  Adapters map each engine's driver values into `CellValue` at execution time; the grid and CSV paths consume only the logical type, so they are engine-agnostic.
- Lossless-by-default: anything not confidently typed becomes `UNKNOWN`+string rather than a lossy cast, matching the fail-closed posture elsewhere.

## Per-engine mapping tables (normative; fixture-pinned alongside the classification suite)

Anything not listed maps to `UNKNOWN` and travels as the driver's text rendering in `string_value` — never a coerced native type.

### PostgreSQL (pgx — by type OID / name)
| PG type | LogicalType | CellValue kind |
|---|---|---|
| `bool` | BOOL | bool_value |
| `int2`, `int4`, `int8` | INT | int_value (decimal string) |
| `numeric` | DECIMAL | decimal_value (exact text) |
| `float4`, `float8` | FLOAT | double_value |
| `text`, `varchar`, `bpchar`, `name`, `citext` | STRING | string_value |
| `bytea` | BYTES | bytes_value |
| `date` | DATE | temporal_value `YYYY-MM-DD` |
| `time`, `timetz` | TIME | temporal_value `HH:MM:SS[.ffffff]` (timetz keeps its offset) |
| `timestamp` | TIMESTAMP | temporal_value, naive ISO-8601 |
| `timestamptz` | TIMESTAMPTZ | temporal_value, UTC-normalized with offset |
| `json`, `jsonb` | JSON | string_value (raw text) |
| `uuid` | UUID | string_value (canonical lowercase) |
| any array type (typelem ≠ 0) | ARRAY | string_value (PG text rendering, e.g. `{1,2}`) |
| `oid`, `xid`, `interval`, ranges, geo, everything else | UNKNOWN | string_value |

### MySQL (go-sql-driver — by `ColumnType.DatabaseTypeName()`)
| MySQL type | LogicalType | Notes |
|---|---|---|
| `TINYINT`, `SMALLINT`, `MEDIUMINT`, `INT`, `BIGINT` (signed or `UNSIGNED`) | INT | int_value; `TINYINT(1)` stays INT (display width is not a type — no bool guessing) |
| `YEAR` | INT | |
| `DECIMAL` | DECIMAL | exact text |
| `FLOAT`, `DOUBLE` | FLOAT | |
| `CHAR`, `VARCHAR`, `TEXT`/`TINYTEXT`/`MEDIUMTEXT`/`LONGTEXT`, `ENUM`, `SET` | STRING | |
| `BINARY`, `VARBINARY`, `BLOB`/`TINYBLOB`/`MEDIUMBLOB`/`LONGBLOB`, `BIT` | BYTES | |
| `DATE` | DATE | |
| `TIME` | TIME | |
| `DATETIME` | TIMESTAMP | naive — MySQL stores it as-entered |
| `TIMESTAMP` | TIMESTAMPTZ | MySQL converts through the session tz; adapter fixes the session to UTC and emits UTC |
| `JSON` | JSON | raw text |
| `GEOMETRY` etc. | UNKNOWN | |

### SQLite (dynamic typing — two-level rule)
- **`ColumnMeta.logical_type` comes from the declared type** (decltype) via SQLite's affinity rules: INTEGER affinity → INT, REAL → FLOAT, TEXT → STRING, BLOB (or no decltype) → BYTES, NUMERIC affinity → DECIMAL; expression columns without a decltype → UNKNOWN.
- **Each `CellValue` follows the cell's actual storage class** (`sqlite3_column_type`): INTEGER → int_value, FLOAT → double_value, TEXT → string_value, BLOB → bytes_value, NULL → is_null.
  A cell whose storage class contradicts the column's logical type is legal in SQLite and travels by its storage class — the grid renders by cell, sorts by the rules above per cell.
- SQLite has no native date/time/uuid/json types; such columns surface as their storage class (typically STRING) — no format sniffing.
