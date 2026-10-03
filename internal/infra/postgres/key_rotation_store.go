package postgres

import (
	"context"
	"errors"
	"math"

	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/encryption"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// KeyRotationStore replaces encryption envelopes with atomic batch audit evidence.
type KeyRotationStore struct{ conns *ConnectionStore }

// NewKeyRotationStore constructs the eager rotation persistence adapter.
func NewKeyRotationStore(pool *pgxpool.Pool) *KeyRotationStore {
	return &KeyRotationStore{conns: NewConnectionStore(pool)}
}

// RemainingEncryptionRows checks every org for nonactive envelopes before administrative completion.
func (s *KeyRotationStore) RemainingEncryptionRows(ctx context.Context, active uint32) (int64, error) {
	if active == 0 || active > math.MaxInt32 {
		return 0, safeErrorf("invalid encryption version")
	}
	return s.conns.q.CountRemainingEncryptionRows(ctx, int32(active)) //nolint:gosec // checked above
}

// RotateBatch locks old envelopes, replaces them, and commits one event per affected organization.
func (s *KeyRotationStore) RotateBatch(ctx context.Context, active uint32, rotate func(encryption.Record) (encryption.Record, error)) (int, error) {
	if active == 0 || active > math.MaxInt32 {
		return 0, safeErrorf("invalid encryption version")
	}
	count := 0
	err := s.conns.withTx(ctx, func(q *db.Queries) error {
		organizations := map[string]int{}
		organizationID, err := q.GetNextRotationOrganization(ctx, int32(active)) //nolint:gosec // checked above
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		credentials, err := q.LockRotationCredentials(ctx, db.LockRotationCredentialsParams{ActiveVersion: int32(active), OrganizationID: organizationID}) //nolint:gosec // checked above
		if err != nil {
			return err
		}
		for _, row := range credentials {
			value, err := rotate(encryption.Record{Kind: "connection_credential", ID: uuidToString(row.ID), OrganizationID: uuidToString(row.OrganizationID), KeyVersion: uint32(*row.CredentialKeyVersion), WrappedDEK: row.CredentialWrappedDek, Nonce: row.CredentialNonce, Ciphertext: row.CredentialCiphertext}) //nolint:gosec // positive DB CHECK
			if err != nil {
				return err
			}
			version := int32(active) //nolint:gosec // checked above
			if err = q.RotateCredentialEnvelope(ctx, db.RotateCredentialEnvelopeParams{ID: row.ID, OrganizationID: row.OrganizationID, KeyVersion: &version, WrappedDek: value.WrappedDEK, Nonce: value.Nonce, Ciphertext: value.Ciphertext}); err != nil {
				return err
			}
			organizations[value.OrganizationID]++
			count++
		}
		payloads, err := q.LockRotationPayloads(ctx, db.LockRotationPayloadsParams{ActiveVersion: int32(active), OrganizationID: organizationID}) //nolint:gosec // checked above
		if err != nil {
			return err
		}
		for _, row := range payloads {
			value, err := rotate(encryption.Record{Kind: "access_request_payload", ID: uuidToString(row.ID), OrganizationID: uuidToString(row.OrganizationID), KeyVersion: uint32(row.PayloadKeyVersion), WrappedDEK: row.PayloadWrappedDek, Nonce: row.PayloadNonce, Ciphertext: row.PayloadCiphertext}) //nolint:gosec // positive DB CHECK
			if err != nil {
				return err
			}
			if err = q.RotatePayloadEnvelope(ctx, db.RotatePayloadEnvelopeParams{ID: row.ID, OrganizationID: row.OrganizationID, KeyVersion: int32(active), WrappedDek: value.WrappedDEK, Nonce: value.Nonce, Ciphertext: value.Ciphertext}); err != nil {
				return err
			} //nolint:gosec // checked above
			organizations[value.OrganizationID]++
			count++
		}
		results, err := q.LockRotationResultKeys(ctx, db.LockRotationResultKeysParams{ActiveVersion: int32(active), OrganizationID: organizationID}) //nolint:gosec // checked above
		if err != nil {
			return err
		}
		for _, row := range results {
			value, err := rotate(encryption.Record{Kind: "result_set", ID: uuidToString(row.ID), OrganizationID: uuidToString(row.OrganizationID), KeyVersion: uint32(row.KeyVersion), WrappedDEK: row.WrappedDek}) //nolint:gosec // positive DB CHECK
			if err != nil {
				return err
			}
			if err = q.RotateResultEnvelope(ctx, db.RotateResultEnvelopeParams{ID: row.ID, OrganizationID: row.OrganizationID, KeyVersion: int32(active), WrappedDek: value.WrappedDEK}); err != nil {
				return err
			} //nolint:gosec // checked above
			organizations[value.OrganizationID]++
			count++
		}
		for org, rows := range organizations {
			if err = insertAuditTx(ctx, q, audit.Event{OrganizationID: identity.OrganizationID(org), ActorType: audit.ActorSystem, ActorService: "system:key-rotation", Action: audit.Action("KEY_ROTATION_BATCH"), TargetType: "encryption", Outcome: audit.OutcomeSucceeded, Metadata: map[string]any{"active_version": active, "rows": rows}}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
