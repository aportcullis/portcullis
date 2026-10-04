package crypto_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"github.com/aportcullis/portcullis/internal/domain/encryption"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/google/uuid"
	"math"
	"testing"
)

func TestResultCodecBindsManifestAndSurvivesKeyRotation(t *testing.T) {
	oldKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))
	newKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32))
	original, err := crypto.LoadKeyring(oldKey, "")
	if err != nil {
		t.Fatal(err)
	}
	rotatedRing, err := crypto.LoadVersionedKeyring(newKey, "", "1:"+oldKey, "")
	if err != nil {
		t.Fatal(err)
	}
	rows := make([][]query.CellValue, 101)
	for idx := range rows {
		rows[idx] = []query.CellValue{{Kind: query.CellString, Text: "private-result"}}
	}
	metadata := query.SnapshotMetadata{ID: uuid.NewString(), OrganizationID: identity.OrganizationID(uuid.NewString()), RowCount: 101, Truncated: true}
	sealed, err := crypto.NewResultCodec(original).Seal(metadata, []query.Column{{Name: "secret", Logical: query.LogicalString}}, rows)
	if err != nil {
		t.Fatal(err)
	}
	rewrapped, err := rotatedRing.RotateRecord(encryption.Record{Kind: crypto.RecordTypeResultSet, ID: metadata.ID, OrganizationID: string(metadata.OrganizationID), KeyVersion: sealed.KeyVersion, WrappedDEK: sealed.WrappedDEK})
	if err != nil {
		t.Fatalf("RotateRecord: %v", err)
	}
	rotated := sealed
	rotated.KeyVersion, rotated.WrappedDEK = rewrapped.KeyVersion, rewrapped.WrappedDEK
	// Success: the untouched manifest opens the schema and both row chunks, before and after the key rotation rewraps the result key.
	for _, opened := range []struct {
		name   string
		codec  *crypto.ResultCodec
		result query.SealedResult
	}{{"original key", crypto.NewResultCodec(original), sealed}, {"rotated key", crypto.NewResultCodec(rotatedRing), rotated}} {
		if _, err := opened.codec.OpenColumns(opened.result, opened.result.Chunks[0]); err != nil {
			t.Errorf("%s: OpenColumns = %v", opened.name, err)
		}
		for _, chunk := range opened.result.Chunks[1:] {
			if _, err := opened.codec.OpenRows(opened.result, chunk); err != nil {
				t.Errorf("%s: OpenRows(%d) = %v", opened.name, chunk.Index, err)
			}
		}
	}
	// Refusal: any change to the authenticated manifest fails the schema and row chunks alike.
	for _, tc := range []struct {
		name   string
		tamper func(*query.SnapshotMetadata)
	}{
		{"row count lowered", func(changed *query.SnapshotMetadata) { changed.RowCount = 100 }},
		{"row count raised", func(changed *query.SnapshotMetadata) { changed.RowCount = 102 }},
		{"truncation cleared", func(changed *query.SnapshotMetadata) { changed.Truncated = false }},
		{"row count past a chunk boundary", func(changed *query.SnapshotMetadata) { changed.RowCount = 201 }},
	} {
		changed := rotated
		tc.tamper(&changed.Metadata)
		codec := crypto.NewResultCodec(rotatedRing)
		if _, err := codec.OpenColumns(changed, changed.Chunks[0]); !errors.Is(err, crypto.ErrDecrypt) {
			t.Errorf("%s: OpenColumns = %v, want ErrDecrypt", tc.name, err)
		}
		if _, err := codec.OpenRows(changed, changed.Chunks[2]); !errors.Is(err, crypto.ErrDecrypt) {
			t.Errorf("%s: OpenRows = %v, want ErrDecrypt", tc.name, err)
		}
	}
}

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
