package result_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"github.com/aportcullis/portcullis/internal/app/result"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/google/uuid"
	"math"
	"strings"
	"testing"
)

type resultRepository struct{ sealed query.SealedResult }

func (r *resultRepository) Put(_ context.Context, sealed query.SealedResult) error {
	r.sealed = sealed
	return nil
}

func TestResultSortOrdersNonfiniteFloatsAndPreservesTies(t *testing.T) {
	ring, err := crypto.LoadKeyring(base64.StdEncoding.EncodeToString(make([]byte, 32)), "")
	if err != nil {
		t.Fatal(err)
	}
	repository := &resultRepository{}
	service, err := result.New(repository, crypto.NewResultCodec(ring), 2)
	if err != nil {
		t.Fatal(err)
	}
	metadata := query.SnapshotMetadata{ID: uuid.NewString(), OrganizationID: "org", OwnerID: "owner"}
	rows := [][]query.CellValue{
		{{Kind: query.CellFloat, Float: math.NaN()}, {Kind: query.CellString, Text: "nan"}},
		{{Kind: query.CellFloat, Float: 1}, {Kind: query.CellString, Text: "first"}},
		{{Kind: query.CellFloat, Float: math.Inf(-1)}, {Kind: query.CellString, Text: "negative"}},
		{{Kind: query.CellFloat, Float: 1}, {Kind: query.CellString, Text: "second"}},
	}
	if _, err = service.Save(context.Background(), metadata, []query.Column{{Name: "float", Logical: query.LogicalFloat}, {Name: "tag", Logical: query.LogicalString}}, rows, 4096); err != nil {
		t.Fatal(err)
	}
	page, err := service.Page(context.Background(), "org", "owner", metadata.ID, query.ResultPageQuery{SortColumn: 0, FilterColumn: -1})
	if err != nil {
		t.Fatal(err)
	}
	for index, want := range []string{"negative", "first", "second", "nan"} {
		if page.Rows[index][1].Text != want {
			t.Fatalf("position %d=%s want %s", index, page.Rows[index][1].Text, want)
		}
	}
}
func (r *resultRepository) Get(_ context.Context, org identity.OrganizationID, owner identity.UserID, id string) (query.SealedResult, error) {
	if r.sealed.Metadata.ID != id || r.sealed.Metadata.OrganizationID != org || r.sealed.Metadata.OwnerID != owner {
		return query.SealedResult{}, query.ErrResultUnavailable
	}
	return r.sealed, nil
}
func (r *resultRepository) GetChunk(ctx context.Context, org identity.OrganizationID, owner identity.UserID, id string, index int) (query.SealedResultChunk, error) {
	if _, err := r.Get(ctx, org, owner, id); err != nil {
		return query.SealedResultChunk{}, err
	}
	for _, chunk := range r.sealed.Chunks {
		if chunk.Index == index {
			return chunk, nil
		}
	}
	return query.SealedResultChunk{}, query.ErrResultUnavailable
}

func TestResultPagesKeepExactNumbersNullOrderingAndSafeCSV(t *testing.T) {
	kr, err := crypto.LoadKeyring(base64.StdEncoding.EncodeToString(make([]byte, 32)), "")
	if err != nil {
		t.Fatal(err)
	}
	repo := &resultRepository{}
	svc, err := result.New(repo, crypto.NewResultCodec(kr), 2)
	if err != nil {
		t.Fatal(err)
	}
	metadata := query.SnapshotMetadata{ID: uuid.NewString(), OrganizationID: "org", OwnerID: "owner"}
	rows := [][]query.CellValue{
		{{Kind: query.CellInt, Text: "9007199254740993"}, {Kind: query.CellString, Text: "=1+1"}},
		{{Kind: query.CellInt, Text: "9007199254740992"}, {Kind: query.CellString, Text: "line\nnext"}},
		{{Kind: query.CellNull}, {Kind: query.CellString, Text: "null-row"}},
	}
	ctx := context.Background()
	if _, err := svc.Save(ctx, metadata, []query.Column{{Name: "number", Logical: query.LogicalInt}, {Name: "text", Logical: query.LogicalString}}, rows, 4096); err != nil {
		t.Fatal(err)
	}
	for _, descending := range []bool{false, true} {
		page, err := svc.Page(ctx, "org", "owner", metadata.ID, query.ResultPageQuery{Page: 1, PageSize: 10, SortColumn: 0, Descending: descending, FilterColumn: -1})
		if err != nil {
			t.Fatal(err)
		}
		want := "9007199254740992"
		if descending {
			want = "9007199254740993"
		}
		if page.Rows[0][0].Text != want || page.Rows[2][0].Kind != query.CellNull {
			t.Fatalf("numeric or NULL sort failed")
		}
	}
	var csv bytes.Buffer
	if err := svc.ExportCSV(ctx, "org", "owner", metadata.ID, &csv); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(csv.String(), "'=1+1") || !strings.Contains(csv.String(), "\"line\nnext\"") {
		t.Fatalf("unsafe or lossy CSV: %q", csv.String())
	}
}
