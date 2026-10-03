package result_test

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/aportcullis/portcullis/internal/app/result"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/google/uuid"
)

func TestTemporalSortPagesUseTimeValuesAndStableTies(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                          string
		logical                       query.LogicalType
		values, ascending, descending []string
	}{
		{"fractional instants", query.LogicalTimestamptz, []string{"2026-10-03T00:00:00.5Z", "2026-10-03T00:00:00Z", "2026-10-03T09:00:00+09:00", "infinity", "-infinity", "unsupported", ""}, []string{"4", "1", "2", "0", "3", "5", "6"}, []string{"3", "0", "1", "2", "4", "5", "6"}},
		{"offset instants", query.LogicalTimestamptz, []string{"2026-10-03T01:00:00+02:00", "2026-10-02T23:30:00Z", "2026-10-03T00:00:00Z"}, []string{"0", "1", "2"}, []string{"2", "1", "0"}},
		{"naive fractions", query.LogicalTimestamp, []string{"2026-10-03T00:00:00.5", "2026-10-03T00:00:00", "2026-10-03T00:00:00.50"}, []string{"1", "0", "2"}, []string{"0", "2", "1"}},
		{"calendar dates", query.LogicalDate, []string{"2026-02-01", "2026-01-31", "infinity", "-infinity", "0001-01-01 BC", ""}, []string{"3", "1", "0", "2", "4", "5"}, []string{"2", "0", "1", "3", "4", "5"}},
		{"time offsets", query.LogicalTime, []string{"01:00:00+02", "00:30:00+00", "00:00:00+00"}, []string{"0", "2", "1"}, []string{"1", "2", "0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			rows := make([][]query.CellValue, 0, len(tc.values))
			for index, value := range tc.values {
				cell := query.CellValue{Kind: query.CellTemporal, Text: value}
				if value == "" {
					cell.Kind = query.CellNull
				}
				rows = append(rows, []query.CellValue{cell, {Kind: query.CellString, Text: string(rune('0' + index))}})
			}
			ctx := context.Background()
			if _, err := service.Save(ctx, metadata, []query.Column{{Name: "when", Logical: tc.logical}, {Name: "original", Logical: query.LogicalString}}, rows, 4096); err != nil {
				t.Fatal(err)
			}
			for _, descending := range []bool{false, true} {
				page, err := service.Page(ctx, "org", "owner", metadata.ID, query.ResultPageQuery{SortColumn: 0, Descending: descending, FilterColumn: -1})
				if err != nil {
					t.Fatal(err)
				}
				expected := tc.ascending
				if descending {
					expected = tc.descending
				}
				for index, want := range expected {
					if page.Rows[index][1].Text != want {
						t.Fatalf("descending=%v position %d=%s want %s", descending, index, page.Rows[index][1].Text, want)
					}
				}
			}
			original, err := service.Page(ctx, "org", "owner", metadata.ID, query.ResultPageQuery{SortColumn: -1, FilterColumn: -1})
			if err != nil {
				t.Fatal(err)
			}
			for index, row := range original.Rows {
				if row[1].Text != string(rune('0'+index)) {
					t.Fatal("sorting mutated original snapshot")
				}
			}
		})
	}
}

func TestTypeAwareSortCoversTheWholeSnapshotBeforePaging(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		logical      query.LogicalType
		kind         query.CellKind
		values, want []string
	}{
		{"integers", query.LogicalInt, query.CellInt, []string{"10", "2", "9007199254740993", "9007199254740992"}, []string{"2", "10", "9007199254740992", "9007199254740993"}},
		{"exact decimals", query.LogicalDecimal, query.CellDecimal, []string{"3.0000000000000000001", "3", "2.5", "3.0"}, []string{"2.5", "3", "3.0", "3.0000000000000000001"}},
		{"numeric looking text", query.LogicalString, query.CellString, []string{"10", "2", "1"}, []string{"1", "10", "2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			rows := make([][]query.CellValue, 0, len(tc.values)*10)
			for _, value := range tc.values {
				for repeat := 0; repeat < 10; repeat++ {
					rows = append(rows, []query.CellValue{{Kind: tc.kind, Text: value}})
				}
			}
			ctx := context.Background()
			if _, err := service.Save(ctx, metadata, []query.Column{{Name: "value", Logical: tc.logical}}, rows, 8192); err != nil {
				t.Fatal(err)
			}
			for index, want := range tc.want {
				page, err := service.Page(ctx, "org", "owner", metadata.ID, query.ResultPageQuery{Page: index + 1, PageSize: 10, SortColumn: 0, FilterColumn: -1})
				if err != nil {
					t.Fatal(err)
				}
				if page.TotalCount != int64(len(rows)) || len(page.Rows) != 10 {
					t.Fatalf("incomplete snapshot page: %+v", page)
				}
				for _, row := range page.Rows {
					if row[0].Text != want {
						t.Fatalf("page %d value=%s want %s", index+1, row[0].Text, want)
					}
				}
			}
		})
	}
}
