package postgres

import (
	"context"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewAuthorizedAuditStore keeps append-only writes separate from protected trail reads.
func NewAuthorizedAuditStore(pool *pgxpool.Pool, policy *BindingPolicyEvaluator, organization identity.OrganizationID) (*AuditStore, error) {
	reader, err := NewAuthorizedConnectionStore(pool, policy, organization)
	if err != nil {
		return nil, err
	}
	return &AuditStore{pool: pool, q: db.New(pool), reader: reader}, nil
}

// authorizedAuditPage filters each event before exposing count or page position.
func (store *AuditStore) authorizedAuditPage(ctx context.Context, organization identity.OrganizationID, query audit.ListParams) (audit.EventPage, error) {
	var result audit.EventPage
	err := store.reader.withAuthorizedRead(ctx, organization, func(ctx context.Context, scope *policyReadScope) error {
		organizationID, err := policyOrganizationUUID(organization)
		if err != nil {
			return err
		}
		var rows []auditListRow
		if query.SortDescending {
			candidates, err := scope.queries.ListAuditEventsDesc(ctx, db.ListAuditEventsDescParams{OrganizationID: organizationID, PageLimit: int64(scope.policy.candidateQueryLimit())})
			if err != nil {
				return err
			}
			for _, candidate := range candidates {
				rows = append(rows, auditListRow(candidate))
			}
		} else {
			candidates, err := scope.queries.ListAuditEventsAsc(ctx, db.ListAuditEventsAscParams{OrganizationID: organizationID, PageLimit: int64(scope.policy.candidateQueryLimit())})
			if err != nil {
				return err
			}
			for _, candidate := range candidates {
				rows = append(rows, auditListRow(candidate))
			}
		}
		if len(rows) > scope.policy.readSettings.MaxCandidates {
			return authz.ErrBindingLimitExceeded
		}
		visible := make([]audit.Event, 0, len(rows))
		for _, row := range rows {
			allowed, err := scope.candidate(ctx, identity.ResourceAudit, uuidToString(row.ID), identity.PermissionAuditList)
			if err != nil {
				return err
			}
			if !allowed {
				continue
			}
			event, err := auditEventFromRow(row, organization)
			if err != nil {
				return err
			}
			visible = append(visible, event)
		}
		page, start, end := authorizedPageBounds(query.Page, query.PageSize, len(visible))
		result = audit.EventPage{Events: visible[start:end], Page: page, PageSize: query.PageSize, TotalCount: int64(len(visible))}
		return nil
	})
	if err != nil {
		return audit.EventPage{}, err
	}
	return result, nil
}

// authorizedAuditEvent admits the actual event before decoding its protected metadata.
func (store *AuditStore) authorizedAuditEvent(ctx context.Context, organization identity.OrganizationID, identifier string) (audit.Event, error) {
	var result audit.Event
	err := store.reader.withAuthorizedRead(ctx, organization, func(ctx context.Context, scope *policyReadScope) error {
		if scope.operation.ID != identifier {
			return authz.ErrPermissionDenied
		}
		copy := *store
		copy.reader, copy.q = nil, scope.queries
		var err error
		result, err = copy.Get(ctx, organization, identifier)
		return err
	})
	return result, err
}
