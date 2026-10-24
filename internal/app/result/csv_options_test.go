package result_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/google/uuid"
)

// TestCSVExportOptionsPreserveSafeDefaultsAndExplicitRawValues qualifies safety choices without changing stored values.
func TestCSVExportOptionsPreserveSafeDefaultsAndExplicitRawValues(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name string
		text string
		raw  bool
		want string
	}{
		{name: "default protects formulas and headers", text: "=1+1", want: "'=1+1"},
		{name: "raw preserves formulas and headers", text: "=1+1", raw: true, want: "=1+1"},
		{name: "raw preserves quoted multiline fields", text: "\t=1,\"two\"\nnext", raw: true, want: "\t=1,\"two\"\nnext"},
		{name: "safe preserves ordinary exact text", text: "9007199254740993.00000001", want: "9007199254740993.00000001"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			service, _ := newIntegrityService(t)
			metadata := query.SnapshotMetadata{ID: uuid.NewString(), OrganizationID: "org", OwnerID: "owner"}
			columns := []query.Column{{Name: scenario.text, Logical: query.LogicalString}, {Name: "exact", Logical: query.LogicalInt}}
			rows := [][]query.CellValue{{{Kind: query.CellString, Text: scenario.text}, {Kind: query.CellInt, Text: "9007199254740993"}}}
			if _, err := service.Save(context.Background(), metadata, columns, rows, 4096); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if err := service.ExportCSVWithOptions(context.Background(), "org", "owner", metadata.ID, query.CSVExportOptions{Raw: scenario.raw}, &output); err != nil {
				t.Fatal(err)
			}
			records, err := csv.NewReader(strings.NewReader(output.String())).ReadAll()
			if err != nil || len(records) != 2 || records[0][0] != scenario.want || records[1][0] != scenario.want || records[1][1] != "9007199254740993" {
				t.Fatalf("CSV representation or precision changed: %q, %v", records, err)
			}
			page, err := service.Page(context.Background(), "org", "owner", metadata.ID, query.ResultPageQuery{SortColumn: -1, FilterColumn: -1})
			if err != nil || page.Columns[0].Name != scenario.text || page.Rows[0][0].Text != scenario.text {
				t.Fatalf("export mutated the snapshot: %+v, %v", page, err)
			}
		})
	}
}

// TestCSVExportOptionsPreserveRefusalAndCancellation keeps raw output behind the same ownership and failure boundaries.
func TestCSVExportOptionsPreserveRefusalAndCancellation(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"foreign organization", "foreign owner", "missing snapshot", "cancelled caller", "failed destination", "tampered snapshot"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			for _, raw := range []bool{false, true} {
				service, repository := newIntegrityService(t)
				metadata := saveNumberedResult(t, service, 3, 4096)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				organization, owner, identifier := identity.OrganizationID("org"), identity.UserID("owner"), metadata.ID
				var output bytes.Buffer
				var destination io.Writer = &output
				want := query.ErrResultUnavailable
				switch scenario {
				case "foreign organization":
					organization = "other"
				case "foreign owner":
					owner = "other"
				case "missing snapshot":
					identifier = uuid.NewString()
				case "cancelled caller":
					cancel()
					want = context.Canceled
				case "failed destination":
					destination = failingCSVDestination{}
					want = io.ErrClosedPipe
				case "tampered snapshot":
					repository.sealed.Metadata.RowCount++
				}
				err := service.ExportCSVWithOptions(ctx, organization, owner, identifier, query.CSVExportOptions{Raw: raw}, destination)
				if !errors.Is(err, want) {
					t.Fatalf("%s raw=%t: got %v, want %v", scenario, raw, err, want)
				}
			}
		})
	}
}

type failingCSVDestination struct{}

// Write models a destination that refuses every output byte.
func (failingCSVDestination) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
