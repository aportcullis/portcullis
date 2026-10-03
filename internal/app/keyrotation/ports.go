package keyrotation

import (
	"context"
	"github.com/aportcullis/portcullis/internal/domain/encryption"
)

// Repository rotates one locked batch and appends its audit evidence atomically.
type Repository interface {
	RotateBatch(context.Context, uint32, func(encryption.Record) (encryption.Record, error)) (int, error)
	RemainingEncryptionRows(context.Context, uint32) (int64, error)
}

// Codec authenticates old envelopes and prepares their active-version replacements.
type Codec interface {
	ActiveVersion() uint32
	RotateRecord(encryption.Record) (encryption.Record, error)
}
