package result

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"errors"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"io"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Service processes encrypted snapshots within a bounded worker pool.
type Service struct {
	repository Repository
	codec      Codec
	workers    chan struct{}
}

// New builds the result service with a positive processing concurrency.
func New(repository Repository, codec Codec, workerCount int) (*Service, error) {
	if repository == nil || codec == nil || workerCount < 1 {
		return nil, errors.New("result: invalid dependencies")
	}
	return &Service{repository: repository, codec: codec, workers: make(chan struct{}, workerCount)}, nil
}

// Save encrypts and admits one bounded execution result.
func (s *Service) Save(ctx context.Context, metadata query.SnapshotMetadata, columns []query.Column, rows [][]query.CellValue, limitBytes int64) (query.SnapshotMetadata, error) {
	if limitBytes < 1 || limitBytes > query.MaxSnapshotBytes {
		return query.SnapshotMetadata{}, query.ErrInvalidResultQuery
	}
	count := min(len(rows), 10000)
	metadata.CreatedAt = time.Now().UTC()
	metadata.ExpiresAt = metadata.CreatedAt.Add(query.ResultTTL)
	metadata.RowCount = int64(count)
	sealed, err := s.codec.Seal(metadata, columns, rows[:count])
	if err != nil {
		return query.SnapshotMetadata{}, err
	}
	if sealed.Metadata.ByteCount > limitBytes {
		low, high := 0, count
		for low < high {
			mid := (low + high + 1) / 2
			metadata.RowCount = int64(mid)
			candidate, err := s.codec.Seal(metadata, columns, rows[:mid])
			if err != nil {
				return query.SnapshotMetadata{}, err
			}
			if candidate.Metadata.ByteCount <= limitBytes {
				low = mid
			} else {
				high = mid - 1
			}
		}
		count = low
		metadata.RowCount = int64(count)
		metadata.Truncated = true
		sealed, err = s.codec.Seal(metadata, columns, rows[:count])
		if err != nil {
			return query.SnapshotMetadata{}, err
		}
	}
	if sealed.Metadata.ByteCount > limitBytes {
		return query.SnapshotMetadata{}, query.ErrResultStoreFull
	}
	if count < len(rows) {
		sealed.Metadata.Truncated = true
	}
	if err := s.repository.Put(ctx, sealed); err != nil {
		return query.SnapshotMetadata{}, err
	}
	stored, err := s.repository.Get(ctx, metadata.OrganizationID, metadata.OwnerID, metadata.ID)
	if err != nil {
		return query.SnapshotMetadata{}, err
	}
	stored.Metadata.Truncated = sealed.Metadata.Truncated
	return stored.Metadata, nil
}

// Page processes a stable page from the same cached snapshot.
func (s *Service) Page(ctx context.Context, org identity.OrganizationID, owner identity.UserID, id string, request query.ResultPageQuery) (query.ResultPage, error) {
	if err := s.acquireWorker(ctx); err != nil {
		return query.ResultPage{}, err
	}
	defer s.releaseWorker()
	sealed, columns, err := s.openSchema(ctx, org, owner, id)
	if err != nil {
		return query.ResultPage{}, err
	}
	if request.SortColumn < -1 || request.SortColumn >= len(columns) || request.FilterColumn < -1 || request.FilterColumn >= len(columns) || len(request.Filter) > 1000 {
		return query.ResultPage{}, query.ErrInvalidResultQuery
	}
	pageSize := request.PageSize
	if pageSize != 10 && pageSize != 20 && pageSize != 50 && pageSize != 100 {
		pageSize = 20
	}
	page := max(request.Page, 1)
	processed := request.SortColumn >= 0 || request.Filter != ""
	var rows [][]query.CellValue
	count := sealed.Metadata.RowCount
	if processed {
		rows, err = s.readRows(ctx, sealed, 0, int(count))
		if err != nil {
			return query.ResultPage{}, err
		}
		if request.Filter != "" {
			filtered := make([][]query.CellValue, 0, len(rows))
			for _, row := range rows {
				if request.FilterColumn < 0 {
					for _, cell := range row {
						if strings.Contains(cellText(cell), request.Filter) {
							filtered = append(filtered, row)
							break
						}
					}
				} else if request.FilterColumn < len(row) && strings.Contains(cellText(row[request.FilterColumn]), request.Filter) {
					filtered = append(filtered, row)
				}
			}
			rows = filtered
		}
		if request.SortColumn >= 0 {
			column := request.SortColumn
			sort.SliceStable(rows, func(left, right int) bool {
				if column >= len(rows[left]) || column >= len(rows[right]) {
					return false
				}
				a, b := rows[left][column], rows[right][column]
				if a.Kind == query.CellNull {
					return false
				}
				if b.Kind == query.CellNull {
					return true
				}
				comparison := compareCells(a, b, columns[column].Logical)
				if request.Descending {
					return comparison > 0
				}
				return comparison < 0
			})
		}
		count = int64(len(rows))
	}
	totalPages := max(1, int((count+int64(pageSize)-1)/int64(pageSize)))
	page = min(page, totalPages)
	start := (page - 1) * pageSize
	end := min(start+pageSize, int(count))
	if processed {
		rows = rows[start:end]
	} else {
		rows, err = s.readRows(ctx, sealed, start, end)
		if err != nil {
			return query.ResultPage{}, err
		}
	}
	return query.ResultPage{Columns: columns, Rows: rows, Page: page, PageSize: pageSize, TotalCount: count, TotalPages: totalPages, Truncated: sealed.Metadata.Truncated, ExpiresAt: sealed.Metadata.ExpiresAt}, nil
}

// ExportCSV streams schema and row chunks without retaining the whole result.
func (s *Service) ExportCSV(ctx context.Context, org identity.OrganizationID, owner identity.UserID, id string, writer io.Writer) error {
	if err := s.acquireWorker(ctx); err != nil {
		return err
	}
	defer s.releaseWorker()
	sealed, columns, err := s.openSchema(ctx, org, owner, id)
	if err != nil {
		return err
	}
	csvWriter := csv.NewWriter(writer)
	header := make([]string, len(columns))
	for idx, column := range columns {
		header[idx] = escapeCSV(column.Name)
	}
	if err := csvWriter.Write(header); err != nil {
		return err
	}
	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		return err
	}
	for offset := 0; offset < int(sealed.Metadata.RowCount); offset += query.ResultChunkRows {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := s.readRows(ctx, sealed, offset, min(offset+query.ResultChunkRows, int(sealed.Metadata.RowCount)))
		if err != nil {
			return err
		}
		for _, row := range rows {
			fields := make([]string, len(row))
			for idx, cell := range row {
				fields[idx] = escapeCSV(cellText(cell))
			}
			if err := csvWriter.Write(fields); err != nil {
				return err
			}
		}
		csvWriter.Flush()
		if err := csvWriter.Error(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) openSchema(ctx context.Context, org identity.OrganizationID, owner identity.UserID, id string) (query.SealedResult, []query.Column, error) {
	sealed, err := s.repository.Get(ctx, org, owner, id)
	if err != nil {
		return query.SealedResult{}, nil, err
	}
	chunk, err := s.repository.GetChunk(ctx, org, owner, id, 0)
	if err != nil {
		return query.SealedResult{}, nil, err
	}
	columns, err := s.codec.OpenColumns(sealed, chunk)
	return sealed, columns, err
}

func (s *Service) readRows(ctx context.Context, sealed query.SealedResult, start, end int) ([][]query.CellValue, error) {
	rows := make([][]query.CellValue, 0, max(0, end-start))
	for offset := start; offset < end; {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		index := offset/query.ResultChunkRows + 1
		chunk, err := s.repository.GetChunk(ctx, sealed.Metadata.OrganizationID, sealed.Metadata.OwnerID, sealed.Metadata.ID, index)
		if err != nil {
			return nil, err
		}
		decoded, err := s.codec.OpenRows(sealed, chunk)
		if err != nil {
			return nil, query.ErrResultUnavailable
		}
		within := offset % query.ResultChunkRows
		count := min(len(decoded)-within, end-offset)
		if count <= 0 {
			return nil, query.ErrResultUnavailable
		}
		rows = append(rows, decoded[within:within+count]...)
		offset += count
	}
	return rows, nil
}

func (s *Service) acquireWorker(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case s.workers <- struct{}{}:
		return nil
	default:
		return query.ErrResultBusy
	}
}
func (s *Service) releaseWorker() { <-s.workers }

func compareCells(left, right query.CellValue, logical query.LogicalType) int {
	if logical == query.LogicalInt || logical == query.LogicalDecimal {
		leftRank, rightRank := numericSpecialRank(left.Text), numericSpecialRank(right.Text)
		if leftRank != rightRank {
			if leftRank < rightRank {
				return -1
			}
			return 1
		}
		a, okA := new(big.Rat).SetString(left.Text)
		b, okB := new(big.Rat).SetString(right.Text)
		if okA && okB {
			return a.Cmp(b)
		}
	}
	if left.Kind == query.CellFloat && right.Kind == query.CellFloat {
		if math.IsNaN(left.Float) {
			if math.IsNaN(right.Float) {
				return 0
			}
			return 1
		}
		if math.IsNaN(right.Float) {
			return -1
		}
		if left.Float < right.Float {
			return -1
		}
		if left.Float > right.Float {
			return 1
		}
		return 0
	}
	if left.Kind == query.CellBool && right.Kind == query.CellBool {
		if left.Bool == right.Bool {
			return 0
		}
		if !left.Bool {
			return -1
		}
		return 1
	}
	if left.Kind == query.CellBytes && right.Kind == query.CellBytes {
		return strings.Compare(string(left.Bytes), string(right.Bytes))
	}
	return strings.Compare(cellText(left), cellText(right))
}

func numericSpecialRank(value string) int {
	switch value {
	case "-Infinity", "-Inf":
		return -1
	case "Infinity", "+Infinity", "Inf", "+Inf":
		return 1
	case "NaN":
		return 2
	default:
		return 0
	}
}

func cellText(cell query.CellValue) string {
	switch cell.Kind {
	case query.CellNull:
		return ""
	case query.CellBool:
		return strconv.FormatBool(cell.Bool)
	case query.CellFloat:
		return strconv.FormatFloat(cell.Float, 'g', -1, 64)
	case query.CellBytes:
		return base64.StdEncoding.EncodeToString(cell.Bytes)
	default:
		return cell.Text
	}
}

func escapeCSV(value string) string {
	if value != "" && strings.ContainsRune("=+-@\t\r", rune(value[0])) {
		return "'" + value
	}
	return value
}
