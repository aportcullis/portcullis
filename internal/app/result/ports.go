package result

import (
	"context"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
)

// Repository stores encrypted snapshots and rechecks owner and TTL for every chunk.
type Repository interface {
	Put(context.Context, query.SealedResult) error
	Get(context.Context, identity.OrganizationID, identity.UserID, string) (query.SealedResult, error)
	GetChunk(context.Context, identity.OrganizationID, identity.UserID, string, int) (query.SealedResultChunk, error)
}

// Codec uses a single per-result key and binds every chunk to its identity.
type Codec interface {
	// SealWithinBudget seals the longest row prefix whose encrypted size fits the byte budget and reports it in RowCount.
	SealWithinBudget(query.SnapshotMetadata, []query.Column, [][]query.CellValue, int64) (query.SealedResult, error)
	OpenColumns(query.SealedResult, query.SealedResultChunk) ([]query.Column, error)
	OpenRows(query.SealedResult, query.SealedResultChunk) ([][]query.CellValue, error)
}
