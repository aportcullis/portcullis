package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/crypto"
	"github.com/aportcullis/portcullis/internal/infra/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

type actor struct {
	Session string `json:"session"`
	CSRF    string `json:"csrf"`
}
type fixture struct {
	ConnectionID string `json:"connectionId"`
	Requester    actor  `json:"requester"`
	Approver     actor  `json:"approver"`
}

// provisionActor issues a synthetic actor before measurement using production persistence and CSRF adapters.
func provisionActor(ctx context.Context, store *postgres.IdentityStore, pool *pgxpool.Pool, org identity.OrganizationID, keyring *crypto.Keyring, email, role string) (actor, error) {
	user, err := store.CreateUser(ctx, email, "Synthetic load user")
	if err != nil {
		return actor{}, err
	}
	if _, err := pool.Exec(ctx, "insert into organization_memberships (organization_id,user_id,role_id) select $1,$2,id from roles where organization_id=$1 and name=$3", string(org), string(user.ID), role); err != nil {
		return actor{}, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return actor{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	if _, err := store.CreateSession(ctx, identity.Session{UserID: user.ID, IdleExpiresAt: time.Now().Add(2 * time.Hour), AbsoluteExpiresAt: time.Now().Add(8 * time.Hour)}, hash[:]); err != nil {
		return actor{}, err
	}
	csrf, err := crypto.NewCSRFProtector(keyring).Issue(token)
	return actor{token, csrf}, err
}
