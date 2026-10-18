package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/aportcullis/portcullis/internal/app/authz"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/infra/postgres/db"
	"github.com/jackc/pgx/v5/pgtype"
)

// withWorkflowTx serializes workflow admission with policy changes before any row lock.
func (store *AccessRequestStore) withWorkflowTx(ctx context.Context, organization pgtype.UUID, operation func(*db.Queries) error) error {
	if store.organization != "" && store.organization != identity.OrganizationID(uuidToString(organization)) {
		return authz.ErrPermissionDenied
	}
	return store.conns.withTx(ctx, func(queries *db.Queries) error {
		if err := lockPolicyOrganization(ctx, queries, identity.OrganizationID(uuidToString(organization)), false); err != nil {
			return err
		}
		return operation(queries)
	})
}

// stampCurrentApprovalCounts keeps internal workflow list projections on the same transaction snapshot.
func (store *AccessRequestStore) stampCurrentApprovalCounts(ctx context.Context, queries *db.Queries, rows []requestViewRow) error {
	if store.policy == nil {
		return nil
	}
	at, err := observeInstant(ctx, queries)
	if err != nil {
		return err
	}
	for index := range rows {
		count, err := store.countCurrentApprovals(ctx, queries, rows[index].OrganizationID, rows[index].ID, rows[index].RequesterID, at)
		if err != nil {
			return err
		}
		rows[index].ValidApprovals = count
	}
	return nil
}

// authorizeRequest checks a current request and propagates operational failures without granting authority.
func (store *AccessRequestStore) authorizeRequest(ctx context.Context, queries *db.Queries, organization pgtype.UUID, request pgtype.UUID, actor pgtype.UUID, permission identity.Permission, at time.Time) error {
	_, err := store.requestDecision(ctx, queries, organization, request, actor, permission, at)
	return err
}

// requestDecision retains the concrete matched versions for atomic admission evidence.
func (store *AccessRequestStore) requestDecision(ctx context.Context, queries *db.Queries, organization, request, actor pgtype.UUID, permission identity.Permission, at time.Time) (authz.ResourceDecision, error) {
	return store.policy.authorize(ctx, queries, identity.UserID(uuidToString(actor)), permission, authz.ResourceTarget{OrganizationID: identity.OrganizationID(uuidToString(organization)), Type: identity.ResourceRequest, ID: uuidToString(request)}, at)
}

// attachAuthorizationEvidence records versioned grant references without condition source or identity strings.
func attachAuthorizationEvidence(event *audit.Event, decision authz.ResourceDecision, permission identity.Permission) {
	if event.Metadata == nil {
		event.Metadata = map[string]any{}
	}
	event.Metadata["authorization"] = map[string]any{"revision": decision.Revision, "binding_id": decision.BindingID, "binding_version": decision.BindingVersion, "role_version": decision.RoleVersion, "evaluated_at": decision.EvaluatedAt.UTC().Format(time.RFC3339Nano), "permission": permission, "resource_type": decision.Resource.Type, "resource_id": decision.Resource.ID, "reason": "applicable_binding"}
}

// countCurrentApprovals applies the same resource policy to every recorded non-self approval at one instant.
func (store *AccessRequestStore) countCurrentApprovals(ctx context.Context, queries *db.Queries, organization, request, requester pgtype.UUID, at time.Time) (int64, error) {
	candidates, err := queries.ListPolicyApprovalCandidates(ctx, db.ListPolicyApprovalCandidatesParams{OrganizationID: organization, RequestID: request, RequesterID: requester, CandidateLimit: authz.MaxResourceBindings + 1})
	if err != nil {
		return 0, err
	}
	if len(candidates) > authz.MaxResourceBindings {
		return 0, authz.ErrBindingLimitExceeded
	}
	var count int64
	for _, candidate := range candidates {
		if err := store.authorizeRequest(ctx, queries, organization, request, candidate, identity.PermissionRequestsApprove, at); err == nil {
			count++
		} else if !errors.Is(err, authz.ErrPermissionDenied) {
			return 0, err
		}
	}
	return count, nil
}

// currentDecisionError preserves the workflow's refusal contract while retaining unavailable-policy errors.
func currentDecisionError(err error) error {
	if errors.Is(err, authz.ErrPermissionDenied) {
		return access.ErrApproverIneligible
	}
	return err
}
