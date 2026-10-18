package postgres

import (
	"context"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
)

// readStore shares the server-pinned policy while leaving internal terminal cleanup independent of caller authority.
func (store *AccessRequestStore) readStore() *ConnectionStore {
	return &ConnectionStore{pool: store.conns.pool, q: store.conns.q, policy: store.policy, organization: store.organization}
}

// requestVisible preserves requester privacy in addition to resource permission.
func (scope *policyReadScope) requestVisible(ctx context.Context, request access.Request) (bool, error) {
	if request.RequesterID == scope.operation.Principal {
		return true, nil
	}
	for _, permission := range []identity.Permission{identity.PermissionRequestsApprove, identity.PermissionRequestsReject} {
		allowed, err := scope.candidate(ctx, identity.ResourceRequest, string(request.ID), permission)
		if err != nil || allowed {
			return allowed, err
		}
	}
	return false, nil
}

// authorizedRequest reads sealed content only after actual resource and fixed visibility checks.
func (store *AccessRequestStore) authorizedRequest(ctx context.Context, organization identity.OrganizationID, identifier access.RequestID) (access.RequestView, access.SealedPayload, error) {
	var result access.RequestView
	var sealed access.SealedPayload
	err := store.readStore().withAuthorizedRead(ctx, organization, func(ctx context.Context, scope *policyReadScope) error {
		if scope.operation.ID != string(identifier) {
			return authz.ErrPermissionDenied
		}
		requestID, organizationID, err := parseRequestAndOrganizationUUIDs(identifier, organization)
		if err != nil {
			return err
		}
		row, err := scope.queries.GetAccessRequestView(ctx, db.GetAccessRequestViewParams{ID: requestID, OrganizationID: organizationID})
		if err != nil {
			return notFound(err, access.ErrNotFound)
		}
		request := toAccessRequest(toBaseRequestRow(requestViewRow(row)))
		allowed, err := scope.requestVisible(ctx, request)
		if err != nil {
			return err
		}
		if !allowed {
			return access.ErrNotFound
		}
		result, err = store.assembleViewAt(ctx, scope.queries, requestViewRow(row), scope.at)
		if err != nil {
			return err
		}
		result.StampEffective(scope.at)
		sealed = toSealedPayload(toBaseRequestRow(requestViewRow(row)))
		return nil
	})
	if err != nil {
		return access.RequestView{}, access.SealedPayload{}, err
	}
	return result, sealed, nil
}

// historyRequestVisible preserves ownership and current reviewer privacy for every candidate row.
func (scope *policyReadScope) historyRequestVisible(ctx context.Context, request access.Request) (bool, error) {
	if request.RequesterID == scope.operation.Principal {
		return true, nil
	}
	resource := identity.ResourceReference{OrganizationID: scope.organization, Type: identity.ResourceRequest, ID: string(request.ID), ConnectionID: string(request.ConnectionID)}
	for _, permission := range []identity.Permission{identity.PermissionRequestsApprove, identity.PermissionRequestsReject} {
		allowed, err := scope.historyCandidate(ctx, resource, permission)
		if err != nil || allowed {
			return allowed, err
		}
	}
	return false, nil
}

// authorizedRequestPage filters all history batches before counting and clamping pages.
func (store *AccessRequestStore) authorizedRequestPage(ctx context.Context, organization identity.OrganizationID, query access.ListQuery) (access.RequestPage, error) {
	var result access.RequestPage
	err := store.readStore().withAuthorizedHistoryRead(ctx, organization, func(ctx context.Context, scope *policyReadScope) error {
		organizationID, err := policyOrganizationUUID(organization)
		if err != nil {
			return err
		}
		requester, err := stringToUUID(string(query.RequesterID))
		if query.RequesterID == "" {
			requester.Valid = false
		} else if err != nil {
			return err
		}
		page := authorizedHistoryPage[requestViewRow]{requested: query.Page, size: query.PageSize}
		batchSize := int64(scope.policy.readSettings.MaxCandidates)
		for offset := int64(0); ; offset += batchSize {
			var rows []requestViewRow
			if query.SortDescending {
				candidates, err := scope.queries.ListAccessRequestsDesc(ctx, db.ListAccessRequestsDescParams{OrganizationID: organizationID, RequesterID: requester, PageLimit: batchSize, RowOffset: offset})
				if err != nil {
					return err
				}
				rows = make([]requestViewRow, 0, len(candidates))
				for _, candidate := range candidates {
					rows = append(rows, requestViewRow(candidate))
				}
			} else {
				candidates, err := scope.queries.ListAccessRequestsAsc(ctx, db.ListAccessRequestsAscParams{OrganizationID: organizationID, RequesterID: requester, PageLimit: batchSize, RowOffset: offset})
				if err != nil {
					return err
				}
				rows = make([]requestViewRow, 0, len(candidates))
				for _, candidate := range candidates {
					rows = append(rows, requestViewRow(candidate))
				}
			}
			for _, row := range rows {
				view := viewFromRow(row)
				view.StampEffective(scope.at)
				if query.State != "" && view.EffectiveState != query.State {
					continue
				}
				checkpointStart := len(scope.decisions)
				resource := identity.ResourceReference{OrganizationID: organization, Type: identity.ResourceRequest, ID: uuidToString(row.ID), ConnectionID: uuidToString(row.ConnectionID)}
				allowed, err := scope.historyCandidate(ctx, resource, identity.PermissionRequestsList)
				if err != nil {
					return err
				}
				if !allowed {
					continue
				}
				allowed, err = scope.historyRequestVisible(ctx, view.Request)
				if err != nil {
					return err
				}
				if !allowed {
					scope.decisions = scope.decisions[:checkpointStart]
					continue
				}
				page.appendVisible(row)
			}
			if int64(len(rows)) < batchSize {
				break
			}
		}
		rows, number := page.result()
		items := make([]access.RequestView, 0, len(rows))
		for _, row := range rows {
			count, err := store.countCurrentApprovals(ctx, scope.queries, organizationID, row.ID, row.RequesterID, scope.at)
			if err != nil {
				return err
			}
			view := viewFromRow(row)
			view.ValidApprovals = int(count)
			view.StampEffective(scope.at)
			items = append(items, view)
		}
		result = access.RequestPage{Items: items, Page: number, PageSize: query.PageSize, TotalCount: int64(page.count), TotalPages: (page.count + query.PageSize - 1) / query.PageSize}
		return nil
	})
	if err != nil {
		return access.RequestPage{}, err
	}
	return result, nil
}

// authorizedPageBounds clamps only after visibility has established the total.
func authorizedPageBounds(page, size, count int) (int, int, int) {
	last := (count + size - 1) / size
	if last < 1 {
		last = 1
	}
	if page > last {
		page = last
	}
	if page < 1 {
		page = 1
	}
	start := (page - 1) * size
	end := min(start+size, count)
	return page, start, end
}
