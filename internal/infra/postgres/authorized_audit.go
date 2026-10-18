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

// authorizedAuditPage evaluates all history batches before exposing an exact count and clamped page.
func (store *AuditStore) authorizedAuditPage(ctx context.Context, organization identity.OrganizationID, query audit.ListParams) (audit.EventPage, error) {
	var result audit.EventPage
	err := store.reader.withAuthorizedHistoryRead(ctx, organization, func(ctx context.Context, scope *policyReadScope) error {
		organizationID, err := policyOrganizationUUID(organization)
		if err != nil {
			return err
		}
		page := authorizedHistoryPage[auditListRow]{requested: query.Page, size: query.PageSize}
		batchSize := int64(scope.policy.readSettings.MaxCandidates)
		for offset := int64(0); ; offset += batchSize {
			var rows []auditListRow
			if query.SortDescending {
				candidates, err := scope.queries.ListAuditEventsDesc(ctx, db.ListAuditEventsDescParams{OrganizationID: organizationID, PageLimit: batchSize, RowOffset: offset})
				if err != nil {
					return err
				}
				rows = make([]auditListRow, 0, len(candidates))
				for _, candidate := range candidates {
					rows = append(rows, auditListRow(candidate))
				}
			} else {
				candidates, err := scope.queries.ListAuditEventsAsc(ctx, db.ListAuditEventsAscParams{OrganizationID: organizationID, PageLimit: batchSize, RowOffset: offset})
				if err != nil {
					return err
				}
				rows = make([]auditListRow, 0, len(candidates))
				for _, candidate := range candidates {
					rows = append(rows, auditListRow(candidate))
				}
			}
			for _, row := range rows {
				resource := identity.ResourceReference{OrganizationID: organization, Type: identity.ResourceAudit, ID: uuidToString(row.ID)}
				allowed, err := scope.historyCandidate(ctx, resource, identity.PermissionAuditList)
				if err != nil {
					return err
				}
				if allowed {
					page.appendVisible(row)
				}
			}
			if int64(len(rows)) < batchSize {
				break
			}
		}
		rows, number := page.result()
		events := make([]audit.Event, 0, len(rows))
		for _, row := range rows {
			event, err := auditEventFromRow(row, organization)
			if err != nil {
				return err
			}
			events = append(events, event)
		}
		result = audit.EventPage{Events: events, Page: number, PageSize: query.PageSize, TotalCount: int64(page.count)}
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
