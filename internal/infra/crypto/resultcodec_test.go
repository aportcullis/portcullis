package crypto_test

import (
	"encoding/base64"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/google/uuid"
	"math"
	"testing"
)

func TestResultCodecAuthenticatesOwnerResultAndChunkOrder(t *testing.T) {
	kr, err := crypto.LoadKeyring(base64.StdEncoding.EncodeToString(make([]byte, 32)), "")
	if err != nil {
		t.Fatal(err)
	}
	codec := crypto.NewResultCodec(kr)
	metadata := query.SnapshotMetadata{ID: uuid.NewString(), OrganizationID: identity.OrganizationID(uuid.NewString())}
	rows := make([][]query.CellValue, 101)
	for idx := range rows {
		rows[idx] = []query.CellValue{{Kind: query.CellString, Text: "private-result"}, {Kind: query.CellFloat, Float: math.Inf(1)}}
	}
	sealed, err := codec.Seal(metadata, []query.Column{{Name: "secret", Logical: query.LogicalString}}, rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(sealed.Chunks) != 3 {
		t.Fatalf("chunks=%d", len(sealed.Chunks))
	}
	opened, err := codec.OpenRows(sealed, sealed.Chunks[1])
	if err != nil {
		t.Fatal(err)
	}
	if len(opened) != 100 || opened[0][0].Text != "private-result" || !math.IsInf(opened[0][1].Float, 1) {
		t.Fatalf("round trip failed")
	}
	for _, tc := range []string{"organization", "result", "chunk"} {
		t.Run(tc, func(t *testing.T) {
			changed := sealed
			chunk := sealed.Chunks[1]
			switch tc {
			case "organization":
				changed.Metadata.OrganizationID = identity.OrganizationID(uuid.NewString())
			case "result":
				changed.Metadata.ID = uuid.NewString()
			case "chunk":
				chunk.Index = 2
			}
			if _, err := codec.OpenRows(changed, chunk); err == nil {
				t.Fatal("swapped ciphertext decrypted")
			}
		})
	}
}
