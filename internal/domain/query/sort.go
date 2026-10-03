package query

import (
	"bytes"
	"cmp"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// ResultSortKey holds one parsed value for stable type-aware snapshot ordering.
type ResultSortKey struct {
	cell     CellValue
	logical  LogicalType
	number   *big.Rat
	instant  time.Time
	rank     int
	fallback bool
}

// NewResultSortKey parses a cell once using its declared column type.
func NewResultSortKey(cell CellValue, logical LogicalType) ResultSortKey {
	key := ResultSortKey{cell: cell, logical: logical}
	if cell.Kind == CellNull {
		return key
	}
	switch logical {
	case LogicalInt, LogicalDecimal:
		switch cell.Text {
		case "-Infinity", "-Inf":
			key.rank = -1
		case "Infinity", "+Infinity", "Inf", "+Inf":
			key.rank = 1
		case "NaN":
			key.rank = 2
		default:
			var ok bool
			key.number, ok = new(big.Rat).SetString(cell.Text)
			key.fallback = !ok
		}
	case LogicalFloat:
		key.fallback = cell.Kind != CellFloat
		if math.IsNaN(cell.Float) {
			key.rank = 1
		}
	case LogicalBool:
		key.fallback = cell.Kind != CellBool
	case LogicalBytes:
		key.fallback = cell.Kind != CellBytes
	case LogicalDate, LogicalTime, LogicalTimestamp, LogicalTimestamptz:
		if cell.Text == "-infinity" {
			key.rank = -1
			return key
		}
		if cell.Text == "infinity" {
			key.rank = 1
			return key
		}
		key.instant, key.fallback = parseSortTemporal(cell.Text, logical)
	}
	return key
}

// Compare orders NULL and unsupported typed values last in either direction.
func (key ResultSortKey) Compare(other ResultSortKey, descending bool) int {
	if key.cell.Kind == CellNull || other.cell.Kind == CellNull {
		return compareSortFlags(key.cell.Kind == CellNull, other.cell.Kind == CellNull)
	}
	if key.fallback != other.fallback {
		return compareSortFlags(key.fallback, other.fallback)
	}
	comparison := 0
	if !key.fallback && !other.fallback && key.logical == other.logical {
		switch key.logical {
		case LogicalInt, LogicalDecimal:
			comparison = cmp.Compare(key.rank, other.rank)
			if comparison == 0 && key.rank == 0 {
				comparison = key.number.Cmp(other.number)
			}
		case LogicalFloat:
			comparison = cmp.Compare(key.rank, other.rank)
			if comparison == 0 && key.rank == 0 {
				comparison = cmp.Compare(key.cell.Float, other.cell.Float)
			}
		case LogicalBool:
			comparison = compareSortFlags(key.cell.Bool, other.cell.Bool)
		case LogicalBytes:
			comparison = bytes.Compare(key.cell.Bytes, other.cell.Bytes)
		case LogicalDate, LogicalTime, LogicalTimestamp, LogicalTimestamptz:
			comparison = cmp.Compare(key.rank, other.rank)
			if comparison == 0 && key.rank == 0 {
				comparison = key.instant.Compare(other.instant)
			}
		default:
			comparison = strings.Compare(sortCellText(key.cell), sortCellText(other.cell))
		}
	} else {
		comparison = strings.Compare(sortCellText(key.cell), sortCellText(other.cell))
	}
	if descending {
		return -comparison
	}
	return comparison
}

// parseSortTemporal returns a canonical time value or marks lossless text fallback.
func parseSortTemporal(value string, logical LogicalType) (time.Time, bool) {
	var layouts []string
	switch logical {
	case LogicalDate:
		layouts = []string{time.DateOnly}
	case LogicalTime:
		layouts = []string{"15:04:05Z07:00:00", "15:04:05Z07:00", "15:04:05Z07", "15:04:05"}
	case LogicalTimestamp:
		layouts = []string{"2006-01-02T15:04:05"}
	case LogicalTimestamptz:
		layouts = []string{time.RFC3339Nano}
	}
	for _, layout := range layouts {
		if instant, err := time.Parse(layout, value); err == nil {
			return instant, false
		}
	}
	return time.Time{}, true
}

// compareSortFlags places false before true.
func compareSortFlags(left, right bool) int {
	if left == right {
		return 0
	}
	if left {
		return 1
	}
	return -1
}

// sortCellText retains the lossless scalar representation for text ordering.
func sortCellText(cell CellValue) string {
	switch cell.Kind {
	case CellBool:
		return strconv.FormatBool(cell.Bool)
	case CellFloat:
		return strconv.FormatFloat(cell.Float, 'g', -1, 64)
	case CellBytes:
		return string(cell.Bytes)
	default:
		return cell.Text
	}
}
