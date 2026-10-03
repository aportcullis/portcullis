package query

import (
	"errors"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// Result-cache bounds follow PRD §7.1 and ADR-0011.
const (
	ResultTTL                 = 15 * time.Minute
	MaxSnapshotBytes    int64 = 25 << 20
	UserSnapshotQuota   int64 = 64 << 20
	GlobalSnapshotQuota int64 = 512 << 20
	ResultChunkRows           = 100
)

// Result errors reveal cache availability without exposing another owner's data.
var (
	ErrResultUnavailable  = errors.New("query: result unavailable")
	ErrResultStoreFull    = errors.New("query: result store full")
	ErrResultBusy         = errors.New("query: result processing busy")
	ErrInvalidResultQuery = errors.New("query: invalid result query")
)

// SnapshotMetadata is the plaintext result handle and accounting evidence.
type SnapshotMetadata struct {
	ID             string
	OrganizationID identity.OrganizationID
	OwnerID        identity.UserID
	RowCount       int64
	ByteCount      int64
	CreatedAt      time.Time
	ExpiresAt      time.Time
	Truncated      bool
}

// SealedResult stores one wrapped snapshot key and authenticated chunks.
type SealedResult struct {
	Metadata   SnapshotMetadata
	KeyVersion uint32
	WrappedDEK []byte
	Chunks     []SealedResultChunk
}

// SealedResultChunk authenticates the schema or one row range by index.
type SealedResultChunk struct {
	Index      int
	Nonce      []byte
	Ciphertext []byte
}

// ResultPageQuery chooses a stable result page and optional column processing.
type ResultPageQuery struct {
	Page         int
	PageSize     int
	SortColumn   int
	Descending   bool
	FilterColumn int
	Filter       string
}

// ResultPage exposes only the requested slice of a cached execution.
type ResultPage struct {
	Columns    []Column
	Rows       [][]CellValue
	Page       int
	PageSize   int
	TotalCount int64
	TotalPages int
	Truncated  bool
	ExpiresAt  time.Time
}
