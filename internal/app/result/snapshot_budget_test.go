package result_test

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/app/result"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/google/uuid"
)

// countingCodec counts how many times the snapshot is encrypted.
type countingCodec struct {
	*crypto.ResultCodec
	seals int
}

func (c *countingCodec) SealWithinBudget(metadata query.SnapshotMetadata, columns []query.Column, rows [][]query.CellValue, limitBytes int64) (query.SealedResult, error) {
	c.seals++
	return c.ResultCodec.SealWithinBudget(metadata, columns, rows, limitBytes)
}

// failingRepository refuses every snapshot write.
type failingRepository struct{ resultRepository }

var errSnapshotWrite = errors.New("snapshot write refused")

func (*failingRepository) Put(context.Context, query.SealedResult) error { return errSnapshotWrite }

func newBudgetCodec(t *testing.T) *countingCodec {
	t.Helper()
	ring, err := crypto.LoadKeyring(base64.StdEncoding.EncodeToString(make([]byte, 32)), "")
	if err != nil {
		t.Fatal(err)
	}
	return &countingCodec{ResultCodec: crypto.NewResultCodec(ring)}
}

func budgetRows(count, cellBytes int) [][]query.CellValue {
	rows := make([][]query.CellValue, count)
	for idx := range rows {
		rows[idx] = []query.CellValue{{Kind: query.CellInt, Text: strconv.Itoa(idx)}, {Kind: query.CellString, Text: strings.Repeat("x", cellBytes)}}
	}
	return rows
}

var budgetColumns = []query.Column{{Name: "id", Logical: query.LogicalInt}, {Name: "payload", Logical: query.LogicalString}}

// sealedByteCount measures the encrypted size of a row prefix with an independent codec call.
func sealedByteCount(t *testing.T, codec *crypto.ResultCodec, rows [][]query.CellValue) int64 {
	t.Helper()
	sealed, err := codec.Seal(query.SnapshotMetadata{ID: uuid.NewString(), OrganizationID: "org", OwnerID: "owner"}, budgetColumns, rows)
	if err != nil {
		t.Fatal(err)
	}
	return sealed.Metadata.ByteCount
}

func TestSnapshotSaveSealsOnceAndKeepsTheLongestFittingPrefix(t *testing.T) {
	reference := newBudgetCodec(t).ResultCodec
	wide := budgetRows(1200, 400)
	exact := budgetRows(250, 40)
	for _, scenario := range []struct {
		name          string
		rows          [][]query.CellValue
		limitBytes    int64
		wantRows      int
		wantTruncated bool
	}{
		{"fits under the limit", budgetRows(30, 10), 1 << 20, 30, false},
		{"exactly at the limit", exact, sealedByteCount(t, reference, exact), 250, false},
		{"over the limit across chunks", wide, sealedByteCount(t, reference, wide[:731]), 731, true},
		{"over the row ceiling", budgetRows(10_050, 1), query.MaxSnapshotBytes, 10_000, true},
		{"schema fits but no row does", budgetRows(3, 9000), sealedByteCount(t, reference, nil) + 100, 0, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			codec := newBudgetCodec(t)
			repository := &resultRepository{}
			service, err := result.New(repository, codec, 1)
			if err != nil {
				t.Fatal(err)
			}
			stored, err := service.Save(context.Background(), query.SnapshotMetadata{ID: uuid.NewString(), OrganizationID: "org", OwnerID: "owner"}, budgetColumns, scenario.rows, scenario.limitBytes)
			if err != nil {
				t.Fatal(err)
			}
			if codec.seals != 1 {
				t.Fatalf("snapshot sealed %d times, want once", codec.seals)
			}
			if stored.RowCount != int64(scenario.wantRows) || stored.Truncated != scenario.wantTruncated || stored.ByteCount > scenario.limitBytes {
				t.Fatalf("stored = rows %d truncated %v bytes %d, want rows %d truncated %v within %d", stored.RowCount, stored.Truncated, stored.ByteCount, scenario.wantRows, scenario.wantTruncated, scenario.limitBytes)
			}
			if scenario.wantRows < len(scenario.rows) && scenario.wantRows < 10_000 && sealedByteCount(t, reference, scenario.rows[:scenario.wantRows+1]) <= scenario.limitBytes {
				t.Fatalf("prefix of %d rows is not the longest that fits", scenario.wantRows)
			}
		})
	}
}

func TestSnapshotSaveRefusesBudgetsThatCannotHoldTheSnapshot(t *testing.T) {
	reference := newBudgetCodec(t).ResultCodec
	for _, scenario := range []struct {
		name       string
		repository result.Repository
		limitBytes int64
		want       error
	}{
		{"schema larger than the budget", &resultRepository{}, sealedByteCount(t, reference, nil) - 1, query.ErrResultStoreFull},
		{"zero budget", &resultRepository{}, 0, query.ErrInvalidResultQuery},
		{"budget above the snapshot ceiling", &resultRepository{}, query.MaxSnapshotBytes + 1, query.ErrInvalidResultQuery},
		{"repository refuses the write", &failingRepository{}, 1 << 20, errSnapshotWrite},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			codec := newBudgetCodec(t)
			service, err := result.New(scenario.repository, codec, 1)
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.Save(context.Background(), query.SnapshotMetadata{ID: uuid.NewString(), OrganizationID: "org", OwnerID: "owner"}, budgetColumns, budgetRows(5, 10), scenario.limitBytes)
			if !errors.Is(err, scenario.want) {
				t.Fatalf("Save = %v, want %v", err, scenario.want)
			}
			if codec.seals > 1 {
				t.Fatalf("refused snapshot sealed %d times", codec.seals)
			}
		})
	}
}
