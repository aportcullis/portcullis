package result_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/app/result"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/google/uuid"
)

// TestCSVExportProtectsHeadersAndCellsWithoutChangingTheSnapshot exercises the public export boundary.
func TestCSVExportProtectsHeadersAndCellsWithoutChangingTheSnapshot(t *testing.T) {
	t.Parallel()
	scenarios := []struct {
		name, text string
		escaped    bool
	}{
		{"spaced formula", "  =SUM(A1:A2)", true},
		{"control-prefixed formula", "\x00\x1f +1", true},
		{"unicode whitespace", "\u00a0@SUM(A1:A2)", true},
		{"line feed", "\ntext", true},
		{"tab", "\ttext", true},
		{"carriage return", "\rtext", true},
		{"full-width equals", "＝1+1", true},
		{"full-width plus", " ＋1", true},
		{"full-width minus", "－1", true},
		{"full-width at", "＠SUM(A1:A2)", true},
		{"quoted delimiter", "=1+1\";=2+2", true},
		{"ordinary leading space", "  ordinary", false},
		{"embedded newline", "line\nnext", false},
		{"empty", "", false},
		{"literal apostrophe", "'=1", false},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			keyring, err := crypto.LoadKeyring(base64.StdEncoding.EncodeToString(make([]byte, 32)), "")
			if err != nil {
				t.Fatal(err)
			}
			service, err := result.New(&resultRepository{}, crypto.NewResultCodec(keyring), 1)
			if err != nil {
				t.Fatal(err)
			}
			metadata := query.SnapshotMetadata{ID: uuid.NewString(), OrganizationID: "org", OwnerID: "owner"}
			ctx := context.Background()
			if _, err := service.Save(ctx, metadata, []query.Column{{Name: scenario.text, Logical: query.LogicalString}, {Name: "marker", Logical: query.LogicalString}}, [][]query.CellValue{{{Kind: query.CellString, Text: scenario.text}, {Kind: query.CellString, Text: "marker"}}}, 4096); err != nil {
				t.Fatal(err)
			}
			var exported bytes.Buffer
			if err := service.ExportCSV(ctx, "org", "owner", metadata.ID, &exported); err != nil {
				t.Fatal(err)
			}
			records, err := csv.NewReader(strings.NewReader(exported.String())).ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			want := scenario.text
			if scenario.escaped {
				want = "'" + want
			}
			if len(records) != 2 || len(records[0]) != 2 || len(records[1]) != 2 || records[0][0] != want || records[1][0] != want {
				t.Fatalf("exported records = %q, want header and cell %q", records, want)
			}
			page, err := service.Page(ctx, "org", "owner", metadata.ID, query.ResultPageQuery{SortColumn: -1, FilterColumn: -1})
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Columns) != 2 || len(page.Rows) != 1 || len(page.Rows[0]) != 2 || page.Columns[0].Name != scenario.text || page.Rows[0][0].Text != scenario.text {
				t.Fatalf("export changed snapshot: %#v", page)
			}
		})
	}
}
