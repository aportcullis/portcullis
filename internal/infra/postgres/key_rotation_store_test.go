package postgres_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/app/keyrotation"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/google/uuid"
)

func TestKeyRotationPreservesCredentialsPayloadsResultsAndDigestEvidence(t *testing.T) {
	f := newReqFixtureFresh(t)
	ctx := context.Background()
	oldKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	newKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	old, err := crypto.LoadKeyring(oldKey, "")
	if err != nil {
		t.Fatal(err)
	}
	c := f.newConn(t, unique("rotation"))
	credential, err := crypto.NewConnectionCredentialCodec(old).Seal(f.org, c.ID, connection.Credential{User: "dbuser", Password: "dbsecret"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.Create(ctx, c, credential); err != nil {
		t.Fatal(err)
	}
	draft, err := access.NewDraft(access.RequestID(uuid.NewString()), f.org, c.ID, f.requester, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	payload, err := crypto.NewAccessRequestPayloadCodec(old).Seal(f.org, draft.ID, access.Payload{SQL: "SELECT 'secret'"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.requests.CreateDraft(ctx, draft, payload); err != nil {
		t.Fatal(err)
	}
	metadata := query.SnapshotMetadata{ID: uuid.NewString(), OrganizationID: f.org, OwnerID: f.requester, RowCount: 1}
	snapshot, err := crypto.NewResultCodec(old).Seal(metadata, []query.Column{{Name: "private"}}, [][]query.CellValue{{{Kind: query.CellString, Text: "private row"}}})
	if err != nil {
		t.Fatal(err)
	}
	results := postgres.NewResultStore(f.pool)
	if err = results.Put(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	ring, err := crypto.LoadVersionedKeyring(newKey, "", "1:"+oldKey, "")
	if err != nil {
		t.Fatal(err)
	}
	service, err := keyrotation.New(postgres.NewKeyRotationStore(f.pool), ring)
	if err != nil {
		t.Fatal(err)
	}
	count, err := service.Rotate(ctx)
	if err != nil || count != 3 {
		t.Fatalf("rotate count=%d err=%v", count, err)
	}
	if count, err := service.Rotate(ctx); err != nil || count != 0 {
		t.Fatalf("resume count=%d err=%v", count, err)
	}
	_, rotatedCredential, err := f.store.TestMaterial(ctx, f.org, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	decodedCredential, err := crypto.NewConnectionCredentialCodec(ring).Open(f.org, c.ID, rotatedCredential)
	if err != nil || decodedCredential.Password != "dbsecret" || rotatedCredential.KeyVersion != 2 {
		t.Fatalf("rotated credential: %v", err)
	}
	_, rotatedPayload, err := f.requests.GetSealed(ctx, f.org, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	decodedPayload, err := crypto.NewAccessRequestPayloadCodec(ring).Open(f.org, draft.ID, rotatedPayload)
	if err != nil || decodedPayload.SQL != "SELECT 'secret'" || rotatedPayload.KeyVersion != 2 {
		t.Fatalf("rotated payload: %v", err)
	}
	rotatedResult, err := results.Get(ctx, f.org, f.requester, metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	chunk, err := results.GetChunk(ctx, f.org, f.requester, metadata.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := crypto.NewResultCodec(ring).OpenRows(rotatedResult, chunk)
	if err != nil || rows[0][0].Text != "private row" || rotatedResult.KeyVersion != 2 {
		t.Fatalf("rotated result: %v", err)
	}
	var batches int
	if err = f.pool.QueryRow(ctx, `select count(*) from public.audit_events where action='KEY_ROTATION_BATCH'`).Scan(&batches); err != nil {
		t.Fatal(err)
	}
	if batches != 1 {
		t.Fatal("rotation evidence missing")
	}
}
