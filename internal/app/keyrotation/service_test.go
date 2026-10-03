package keyrotation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aportcullis/portcullis/internal/app/keyrotation"
	"github.com/aportcullis/portcullis/internal/domain/encryption"
)

type rotationRepository struct {
	counts  []int
	calls   int
	failure error
}

func (r *rotationRepository) RotateBatch(_ context.Context, active uint32, rotate func(encryption.Record) (encryption.Record, error)) (int, error) {
	r.calls++
	if active != 2 {
		return 0, errors.New("wrong active key")
	}
	if len(r.counts) == 0 {
		return 0, r.failure
	}
	if _, err := rotate(encryption.Record{KeyVersion: 1}); err != nil {
		return 0, err
	}
	count := r.counts[0]
	r.counts = r.counts[1:]
	return count, nil
}

type rotationCodec struct{}

func (rotationCodec) ActiveVersion() uint32 { return 2 }
func (rotationCodec) RotateRecord(r encryption.Record) (encryption.Record, error) {
	r.KeyVersion = 2
	return r, nil
}

func TestRotationStopsBeforeAdmittingWorkAfterCancellation(t *testing.T) {
	repository := &rotationRepository{}
	service, err := keyrotation.New(repository, rotationCodec{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	count, err := service.Rotate(ctx)
	if count != 0 || !errors.Is(err, context.Canceled) || repository.calls != 0 {
		t.Fatalf("cancelled rotation: count=%d error=%v batches=%d", count, err, repository.calls)
	}
}

func TestRotationReportsCommittedBatchesAndStopsOnFailure(t *testing.T) {
	failure := errors.New("batch rolled back")
	repository := &rotationRepository{counts: []int{100, 3}, failure: failure}
	service, err := keyrotation.New(repository, rotationCodec{})
	if err != nil {
		t.Fatal(err)
	}
	count, err := service.Rotate(context.Background())
	if count != 103 || !errors.Is(err, failure) || repository.calls != 3 {
		t.Fatalf("partial rotation: count=%d error=%v batches=%d", count, err, repository.calls)
	}
	repository.failure = nil
	count, err = service.Rotate(context.Background())
	if count != 0 || err != nil {
		t.Fatalf("resume: count=%d error=%v", count, err)
	}
}
