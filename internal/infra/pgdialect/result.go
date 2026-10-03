package pgdialect

import (
	"context"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/aportcullis/portcullis/internal/domain/query"
)

// resultStream adapts pgconn's ResultReader to query.ResultStream and owns the transaction end: COMMIT on clean, uncanceled exhaustion; ROLLBACK (or the server-side rollback implied by disconnecting) on failure, cancel, or abandonment.
type resultStream struct {
	ctx  context.Context
	conn *pgconn.PgConn
	rr   *pgconn.ResultReader

	columns    []query.Column
	current    []query.CellValue
	pending    []query.CellValue
	hasPending bool

	rowsAffected int64
	err          error
	maxRows      int
	maxBytes     int64
	rowCount     int
	byteCount    int64
	decodedBytes int64
	truncated    bool
	// stopAtCeiling ends a read at the snapshot ceiling; writes keep draining so their whole statement commits.
	stopAtCeiling bool
	finished      bool // transaction ended, connection closed (or abandoned via Close)
}

func (s *resultStream) Truncated() bool { return s.truncated }

func (s *resultStream) Columns() []query.Column { return s.columns }
func (s *resultStream) Row() []query.CellValue  { return s.current }
func (s *resultStream) Err() error              { return s.err }
func (s *resultStream) RowsAffected() int64     { return s.rowsAffected }

func (s *resultStream) Next() bool {
	// finished first: an abandoned (Closed) stream must never surface the buffered read-ahead row — its transaction was rolled back server-side.
	if s.finished {
		return false
	}
	if s.hasPending {
		s.current, s.pending, s.hasPending = s.pending, nil, false
		return true
	}
	for !s.isReadCeilingReached() && s.rr.NextRow() {
		if s.acceptRow(s.rr.Values()) {
			s.current = decodeRow(s.columns, s.rr.Values())
			return true
		}
	}
	if s.isReadCeilingReached() {
		s.endTruncatedRead()
		return false
	}
	tag, err := s.rr.Close()
	s.conclude(tag, err)
	return false
}

// isReadCeilingReached reports whether a read hit the snapshot ceiling and must stop reading the target.
func (s *resultStream) isReadCeilingReached() bool {
	return s.truncated && s.stopAtCeiling
}

// endTruncatedRead cancels a read-only statement on the server once the snapshot ceiling is reached; there is nothing to commit, and the delivered rows are the result.
func (s *resultStream) endTruncatedRead() {
	cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), 2*time.Second)
	defer cancel()
	_ = s.conn.CancelRequest(cancelCtx)
	s.rowsAffected = int64(s.rowCount)
	if ctxErr := s.ctx.Err(); ctxErr != nil {
		s.err = redactExecError(s.ctx, ctxErr)
	}
	closeConn(s.ctx, s.conn)
	s.finished = true
}

// conclude ends the statement's transaction once the result reader is done.
func (s *resultStream) conclude(tag pgconn.CommandTag, rrErr error) {
	s.rowsAffected = tag.RowsAffected()
	switch {
	case rrErr != nil:
		s.err = redactExecError(s.ctx, rrErr)
		s.rollback()
	case s.ctx.Err() != nil:
		// Never commit a canceled execution — the caller records outcome_unknown, and an unconfirmed commit would contradict it.
		s.err = redactExecError(s.ctx, s.ctx.Err())
		s.rollback()
	default:
		if err := runSimple(s.ctx, s.conn, "COMMIT"); err != nil {
			s.err = redactExecError(s.ctx, err)
		}
	}
	closeConn(s.ctx, s.conn)
	s.finished = true
}

// rollback is best-effort: if it fails, closing the connection rolls the transaction back server-side anyway.
func (s *resultStream) rollback() {
	rbCtx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), 5*time.Second)
	defer cancel()
	_, _ = s.conn.Exec(rbCtx, "ROLLBACK").ReadAll()
}

// Close is idempotent. Abandoning an unexhausted stream disconnects without COMMIT, so the open transaction rolls back.
func (s *resultStream) Close() error {
	if !s.finished {
		closeConn(s.ctx, s.conn)
		s.finished = true
	}
	return nil
}

// typeMap resolves OIDs to the built-in PostgreSQL type registry; used read-only, so shared across streams.
var typeMap = pgtype.NewMap()

// describeColumns maps the wire row description onto ADR-0005 ColumnMeta. Nullability is not on the wire, so every column reports nullable.
func describeColumns(fields []pgconn.FieldDescription) []query.Column {
	if len(fields) == 0 {
		return nil
	}
	cols := make([]query.Column, len(fields))
	for idx, f := range fields {
		cols[idx] = query.Column{
			Name:       f.Name,
			Logical:    logicalForOID(f.DataTypeOID),
			DBTypeName: dbTypeName(f.DataTypeOID),
			Nullable:   true,
		}
	}
	return cols
}

// logicalForOID is the ADR-0005 PostgreSQL mapping table. Extension types with dynamic OIDs (e.g. citext) cannot be recognized without a catalog lookup and fall to UNKNOWN — lossless text, never a wrong coercion.
func logicalForOID(oid uint32) query.LogicalType {
	switch oid {
	case pgtype.BoolOID:
		return query.LogicalBool
	case pgtype.Int2OID, pgtype.Int4OID, pgtype.Int8OID:
		return query.LogicalInt
	case pgtype.NumericOID:
		return query.LogicalDecimal
	case pgtype.Float4OID, pgtype.Float8OID:
		return query.LogicalFloat
	case pgtype.TextOID, pgtype.VarcharOID, pgtype.BPCharOID, pgtype.NameOID:
		return query.LogicalString
	case pgtype.ByteaOID:
		return query.LogicalBytes
	case pgtype.DateOID:
		return query.LogicalDate
	case pgtype.TimeOID, pgtype.TimetzOID:
		return query.LogicalTime
	case pgtype.TimestampOID:
		return query.LogicalTimestamp
	case pgtype.TimestamptzOID:
		return query.LogicalTimestamptz
	case pgtype.JSONOID, pgtype.JSONBOID:
		return query.LogicalJSON
	case pgtype.UUIDOID:
		return query.LogicalUUID
	}
	if t, ok := typeMap.TypeForOID(oid); ok {
		if _, isArray := t.Codec.(*pgtype.ArrayCodec); isArray {
			return query.LogicalArray
		}
	}
	return query.LogicalUnknown
}

func dbTypeName(oid uint32) string {
	if t, ok := typeMap.TypeForOID(oid); ok {
		return t.Name
	}
	return "oid:" + strconv.FormatUint(uint64(oid), 10)
}

// decodeRow converts one all-text wire row into ADR-0005 cells. Values that resist canonicalization (special temporals like "infinity", malformed floats) fall back to their lossless raw text rather than failing the row.
func decodeRow(cols []query.Column, values [][]byte) []query.CellValue {
	cells := make([]query.CellValue, len(values))
	for idx, raw := range values {
		if raw == nil {
			cells[idx] = query.CellValue{Kind: query.CellNull}
			continue
		}
		text := string(raw)
		logical := query.LogicalUnknown
		if idx < len(cols) {
			logical = cols[idx].Logical
		}
		switch logical {
		case query.LogicalBool:
			cells[idx] = query.CellValue{Kind: query.CellBool, Bool: text == "t"}
		case query.LogicalInt:
			cells[idx] = query.CellValue{Kind: query.CellInt, Text: text}
		case query.LogicalDecimal:
			cells[idx] = query.CellValue{Kind: query.CellDecimal, Text: text}
		case query.LogicalFloat:
			if f, err := strconv.ParseFloat(text, 64); err == nil {
				cells[idx] = query.CellValue{Kind: query.CellFloat, Float: f}
			} else {
				cells[idx] = query.CellValue{Kind: query.CellString, Text: text}
			}
		case query.LogicalBytes:
			if decoded, err := decodeByteaHex(text); err == nil {
				cells[idx] = query.CellValue{Kind: query.CellBytes, Bytes: decoded}
			} else {
				cells[idx] = query.CellValue{Kind: query.CellString, Text: text}
			}
		case query.LogicalDate, query.LogicalTime:
			cells[idx] = query.CellValue{Kind: query.CellTemporal, Text: text}
		case query.LogicalTimestamp:
			cells[idx] = query.CellValue{Kind: query.CellTemporal, Text: strings.Replace(text, " ", "T", 1)}
		case query.LogicalTimestamptz:
			cells[idx] = query.CellValue{Kind: query.CellTemporal, Text: canonicalTimestamptz(text)}
		default:
			// STRING, JSON, UUID, ARRAY, UNKNOWN: raw text is the contract.
			cells[idx] = query.CellValue{Kind: query.CellString, Text: text}
		}
	}
	return cells
}

func decodeByteaHex(text string) ([]byte, error) {
	rest, ok := strings.CutPrefix(text, `\x`)
	if !ok {
		return nil, strconv.ErrSyntax
	}
	return hex.DecodeString(rest)
}

// canonicalTimestamptz normalizes PG's ISO rendering (session pinned to UTC, so the offset is always +00) to RFC 3339 with Z (ADR-0005). Values Go cannot parse — "infinity", BC dates — pass through as raw text.
func canonicalTimestamptz(text string) string {
	for _, layout := range []string{
		"2006-01-02 15:04:05.999999999-07",
		"2006-01-02 15:04:05.999999999-07:00",
	} {
		if t, err := time.Parse(layout, text); err == nil {
			return t.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
		}
	}
	return text
}

// acceptRow checks raw wire sizes before allocating decoded cells.
func (s *resultStream) acceptRow(values [][]byte) bool {
	if s.truncated {
		return false
	}
	var byteCount int64
	for _, value := range values {
		byteCount += int64(len(value))
	}
	// Reserve cell storage separately from payload bytes: NULL and narrow cells still allocate structs. Include the executor's row copy.
	decodedBytes := int64(len(values))*int64(2*unsafe.Sizeof(query.CellValue{})) + int64(unsafe.Sizeof([]query.CellValue{}))
	if (s.maxRows > 0 && s.rowCount >= s.maxRows) || (s.maxBytes > 0 && (byteCount > s.maxBytes-s.byteCount || decodedBytes > s.maxBytes-s.decodedBytes)) {
		s.truncated = true
		return false
	}
	s.rowCount++
	s.byteCount += byteCount
	s.decodedBytes += decodedBytes
	return true
}
