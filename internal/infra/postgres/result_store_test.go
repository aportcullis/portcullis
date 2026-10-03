package postgres_test

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestResultStoreOwnerTTLAndCiphertextBoundaries(t *testing.T) {
	f := newReqFixture(t)
	store := postgres.NewResultStore(f.pool)
	kr, err := crypto.LoadKeyring(base64.StdEncoding.EncodeToString(make([]byte, 32)), "")
	if err != nil {
		t.Fatal(err)
	}
	codec := crypto.NewResultCodec(kr)
	metadata := query.SnapshotMetadata{ID: uuid.NewString(), OrganizationID: f.org, OwnerID: f.requester, RowCount: 1, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(query.ResultTTL)}
	sealed, err := codec.Seal(metadata, []query.Column{{Name: "private-column"}}, [][]query.CellValue{{{Kind: query.CellString, Text: "private-row"}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.Put(ctx, sealed); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		org   identity.OrganizationID
		owner identity.UserID
	}{{f.org, f.user}, {identity.OrganizationID(uuid.NewString()), f.requester}} {
		if _, err := store.Get(ctx, tc.org, tc.owner, metadata.ID); !errors.Is(err, query.ErrResultUnavailable) {
			t.Fatalf("foreign result: %v", err)
		}
	}
	read, err := store.Get(ctx, f.org, f.requester, metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	chunk, err := store.GetChunk(ctx, f.org, f.requester, metadata.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := codec.OpenRows(read, chunk)
	if err != nil || rows[0][0].Text != "private-row" {
		t.Fatalf("round trip: %v", err)
	}
	var leaked bool
	if err := f.pool.QueryRow(ctx, `select exists(select 1 from result_cache.result_chunks where encode(ciphertext,'escape') like '%private-row%' or encode(ciphertext,'escape') like '%private-column%')`).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked {
		t.Fatal("plaintext in result store")
	}
	if _, err := f.pool.Exec(ctx, `update result_cache.result_sets set expires_at=$1 where id=$2`, time.Now().Add(-time.Second), metadata.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetChunk(ctx, f.org, f.requester, metadata.ID, 1); !errors.Is(err, query.ErrResultUnavailable) {
		t.Fatalf("expired chunk: %v", err)
	}
	if err := store.PurgeExpired(ctx, f.org); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := f.pool.QueryRow(ctx, `select count(*) from result_cache.result_chunks where result_id=$1`, metadata.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("expired ciphertext retained")
	}
}
