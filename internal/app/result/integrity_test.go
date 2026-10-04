package result_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/aportcullis/portcullis/internal/app/result"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
)

// newIntegrityService builds a result service over an in-memory repository and the real snapshot codec.
func newIntegrityService(t *testing.T) (*result.Service, *resultRepository) {
	t.Helper()
	ring, err := crypto.LoadKeyring(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)), "")
	if err != nil {
		t.Fatal(err)
	}
	repository := &resultRepository{}
	service, err := result.New(repository, crypto.NewResultCodec(ring), 2)
	if err != nil {
		t.Fatal(err)
	}
	return service, repository
}

// numberedRows returns count single-column rows holding their own index.
func numberedRows(count int) [][]query.CellValue {
	rows := make([][]query.CellValue, count)
	for idx := range rows {
		rows[idx] = []query.CellValue{{Kind: query.CellString, Text: strconv.Itoa(idx)}}
	}
	return rows
}

// saveNumberedResult stores count numbered rows under limitBytes and returns the saved metadata.
func saveNumberedResult(t *testing.T, service *result.Service, count int, limitBytes int64) query.SnapshotMetadata {
	t.Helper()
	metadata := query.SnapshotMetadata{ID: uuid.NewString(), OrganizationID: "org", OwnerID: "owner"}
	saved, err := service.Save(context.Background(), metadata, []query.Column{{Name: "position", Logical: query.LogicalString}}, numberedRows(count), limitBytes)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	return saved
}

// readWholeResult returns the first page and the CSV export of a stored result.
func readWholeResult(service *result.Service, id string) (query.ResultPage, string, error) {
	page, err := service.Page(context.Background(), "org", "owner", id, query.ResultPageQuery{PageSize: 100, SortColumn: -1, FilterColumn: -1})
	if err != nil {
		return query.ResultPage{}, "", err
	}
	var exported strings.Builder
	if err := service.ExportCSV(context.Background(), "org", "owner", id, &exported); err != nil {
		return query.ResultPage{}, "", err
	}
	return page, exported.String(), nil
}

func TestResultMetadataReadsBackWhenUntouched(t *testing.T) {
	cases := []struct {
		name          string
		rowCount      int
		limitBytes    int64
		wantRows      int64
		wantTruncated bool
	}{
		{"single chunk", 3, query.MaxSnapshotBytes, 3, false},
		{"several chunks", 250, query.MaxSnapshotBytes, 250, false},
		{"row cap truncation", 10_050, query.MaxSnapshotBytes, 10_000, true},
		{"byte budget truncation", 400, 4096, -1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, _ := newIntegrityService(t)
			saved := saveNumberedResult(t, service, tc.rowCount, tc.limitBytes)
			page, exported, err := readWholeResult(service, saved.ID)
			if err != nil {
				t.Fatalf("read untouched result: %v", err)
			}
			if page.Truncated != tc.wantTruncated || saved.Truncated != tc.wantTruncated {
				t.Errorf("truncated page=%v saved=%v, want %v", page.Truncated, saved.Truncated, tc.wantTruncated)
			}
			if tc.wantRows >= 0 && page.TotalCount != tc.wantRows {
				t.Errorf("total rows = %d, want %d", page.TotalCount, tc.wantRows)
			}
			if lines := strings.Count(exported, "\n"); int64(lines) != page.TotalCount+1 {
				t.Errorf("CSV lines = %d, want header plus %d rows", lines, page.TotalCount)
			}
		})
	}
}

func TestTamperedResultMetadataIsUnavailable(t *testing.T) {
	cases := []struct {
		name   string
		rows   int
		tamper func(*query.SealedResult)
	}{
		{"row count lowered to hide rows", 250, func(sealed *query.SealedResult) { sealed.Metadata.RowCount = 120 }},
		{"row count raised", 250, func(sealed *query.SealedResult) { sealed.Metadata.RowCount = 251 }},
		{"complete result marked truncated", 250, func(sealed *query.SealedResult) { sealed.Metadata.Truncated = true }},
		{"truncated result marked complete", 10_050, func(sealed *query.SealedResult) { sealed.Metadata.Truncated = false }},
		{"last chunk dropped with a matching row count", 250, func(sealed *query.SealedResult) {
			sealed.Chunks = sealed.Chunks[:len(sealed.Chunks)-1]
			sealed.Metadata.RowCount = 200
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, repository := newIntegrityService(t)
			saved := saveNumberedResult(t, service, tc.rows, query.MaxSnapshotBytes)
			tc.tamper(&repository.sealed)
			if _, _, err := readWholeResult(service, saved.ID); !errors.Is(err, query.ErrResultUnavailable) {
				t.Errorf("read tampered result = %v, want ErrResultUnavailable", err)
			}
		})
	}
}
