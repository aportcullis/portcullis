// Package keyrotation coordinates resumable eager encryption-key rotation.
package keyrotation

import (
	"context"
	"errors"
	"time"
)

// Service rotates batches through injected persistence and crypto adapters.
type Service struct {
	repository Repository
	codec      Codec
}

// New constructs the rotation use case.
func New(repository Repository, codec Codec) (*Service, error) {
	if repository == nil || codec == nil {
		return nil, errors.New("keyrotation: dependencies required")
	}
	return &Service{repository: repository, codec: codec}, nil
}

// Rotate eagerly replaces old envelopes until no batch remains, preserving historical digests.
func (s *Service) Rotate(ctx context.Context) (int, error) {
	total := 0
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		count, err := s.repository.RotateBatch(ctx, s.codec.ActiveVersion(), s.codec.RotateRecord)
		if err != nil {
			return total, err
		}
		total += count
		if count == 0 {
			remaining, err := s.repository.RemainingEncryptionRows(ctx, s.codec.ActiveVersion())
			if err != nil {
				return total, err
			}
			if remaining != 0 {
				return total, ErrIncomplete
			}
			return total, nil
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return total, ctx.Err()
		case <-timer.C:
		}
	}
}
