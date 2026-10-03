package connectapi

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/internal/app/accessrequest"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
)

// Permissions gating the AccessRequests RPCs (ADR-0008: named at the exact enforcement site). All six keys were seeded by migration 0002.
const (
	permRequestsList    identity.Permission = "requests.list"
	permRequestsGet     identity.Permission = "requests.get"
	permRequestsCreate  identity.Permission = "requests.create"
	permRequestsApprove identity.Permission = "requests.approve"
	permRequestsReject  identity.Permission = "requests.reject"
)

// requestApp is the slice of the access-request application service this handler consumes (DIP/ISP). canReviewAll widens visibility from own-requests to org-wide and unlocks payload decryption for non-owners (§8.4).
type requestApp interface {
	Create(ctx context.Context, requester identity.UserID, p accessrequest.CreateParams) (access.RequestView, error)
	UpdateDraft(ctx context.Context, requester identity.UserID, id access.RequestID, p accessrequest.UpdateDraftParams) (access.RequestView, error)
	Submit(ctx context.Context, requester identity.UserID, id access.RequestID, expectedVersion int64) (access.RequestView, error)
	Cancel(ctx context.Context, requester identity.UserID, id access.RequestID) (access.RequestView, error)
	Approve(ctx context.Context, approver identity.UserID, id access.RequestID, reason string) (access.RequestView, error)
	Reject(ctx context.Context, approver identity.UserID, id access.RequestID, reason string) (access.RequestView, error)
	Get(ctx context.Context, viewer identity.UserID, canReviewAll bool, id access.RequestID) (access.RequestView, error)
	List(ctx context.Context, viewer identity.UserID, canReviewAll bool, q access.ListQuery) (access.RequestPage, error)
	ListRequestableConnections(ctx context.Context) ([]access.RequestableConnection, error)
}

// AccessRequestsService implements the AccessRequests RPCs (PRD §4.4, ADR-0018).
type AccessRequestsService struct {
	authz authorizer
	svc   requestApp
}

// NewAccessRequestsService builds the handler over the authorizer and the access-request app service.
func NewAccessRequestsService(az authorizer, svc requestApp) *AccessRequestsService {
	return &AccessRequestsService{authz: az, svc: svc}
}

func (a *AccessRequestsService) Create(
	ctx context.Context,
	req *connect.Request[portcullisv1.CreateAccessRequestRequest],
) (*connect.Response[portcullisv1.CreateAccessRequestResponse], error) {
	if err := requirePermission(ctx, a.authz, permRequestsCreate); err != nil {
		return nil, err
	}
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseConnectionID(req.Msg.GetConnectionId())
	if err != nil {
		return nil, err
	}
	view, err := a.svc.Create(ctx, user.ID, accessrequest.CreateParams{
		ConnectionID: id,
		Title:        req.Msg.GetTitle(),
		Body:         req.Msg.GetBody(),
		SQL:          req.Msg.GetSql(),
		Params:       toParams(req.Msg.GetParams()),
	})
	if err != nil {
		return nil, requestError(err)
	}
	return connect.NewResponse(&portcullisv1.CreateAccessRequestResponse{Request: toProtoRequest(view)}), nil
}

func (a *AccessRequestsService) UpdateDraft(
	ctx context.Context,
	req *connect.Request[portcullisv1.UpdateAccessRequestDraftRequest],
) (*connect.Response[portcullisv1.UpdateAccessRequestDraftResponse], error) {
	if err := requirePermission(ctx, a.authz, permRequestsCreate); err != nil {
		return nil, err
	}
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseRequestID(req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	view, err := a.svc.UpdateDraft(ctx, user.ID, id, accessrequest.UpdateDraftParams{
		Title:           req.Msg.GetTitle(),
		Body:            req.Msg.GetBody(),
		SQL:             req.Msg.GetSql(),
		Params:          toParams(req.Msg.GetParams()),
		ExpectedVersion: req.Msg.GetExpectedVersion(),
	})
	if err != nil {
		return nil, requestError(err)
	}
	return connect.NewResponse(&portcullisv1.UpdateAccessRequestDraftResponse{Request: toProtoRequest(view)}), nil
}

func (a *AccessRequestsService) Submit(
	ctx context.Context,
	req *connect.Request[portcullisv1.SubmitAccessRequestRequest],
) (*connect.Response[portcullisv1.SubmitAccessRequestResponse], error) {
	if err := requirePermission(ctx, a.authz, permRequestsCreate); err != nil {
		return nil, err
	}
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseRequestID(req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	view, err := a.svc.Submit(ctx, user.ID, id, req.Msg.GetExpectedVersion())
	if err != nil {
		return nil, requestError(err)
	}
	return connect.NewResponse(&portcullisv1.SubmitAccessRequestResponse{Request: toProtoRequest(view)}), nil
}

func (a *AccessRequestsService) Cancel(
	ctx context.Context,
	req *connect.Request[portcullisv1.CancelAccessRequestRequest],
) (*connect.Response[portcullisv1.CancelAccessRequestResponse], error) {
	if err := requirePermission(ctx, a.authz, permRequestsCreate); err != nil {
		return nil, err
	}
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseRequestID(req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	view, err := a.svc.Cancel(ctx, user.ID, id)
	if err != nil {
		return nil, requestError(err)
	}
	return connect.NewResponse(&portcullisv1.CancelAccessRequestResponse{Request: toProtoRequest(view)}), nil
}

func (a *AccessRequestsService) Approve(
	ctx context.Context,
	req *connect.Request[portcullisv1.ApproveAccessRequestRequest],
) (*connect.Response[portcullisv1.ApproveAccessRequestResponse], error) {
	if err := requirePermission(ctx, a.authz, permRequestsApprove); err != nil {
		return nil, err
	}
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseRequestID(req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	view, err := a.svc.Approve(ctx, user.ID, id, req.Msg.GetReason())
	if err != nil {
		return nil, requestError(err)
	}
	return connect.NewResponse(&portcullisv1.ApproveAccessRequestResponse{Request: toProtoRequest(view)}), nil
}

func (a *AccessRequestsService) Reject(
	ctx context.Context,
	req *connect.Request[portcullisv1.RejectAccessRequestRequest],
) (*connect.Response[portcullisv1.RejectAccessRequestResponse], error) {
	if err := requirePermission(ctx, a.authz, permRequestsReject); err != nil {
		return nil, err
	}
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseRequestID(req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	view, err := a.svc.Reject(ctx, user.ID, id, req.Msg.GetReason())
	if err != nil {
		return nil, requestError(err)
	}
	return connect.NewResponse(&portcullisv1.RejectAccessRequestResponse{Request: toProtoRequest(view)}), nil
}

func (a *AccessRequestsService) Get(
	ctx context.Context,
	req *connect.Request[portcullisv1.GetAccessRequestRequest],
) (*connect.Response[portcullisv1.GetAccessRequestResponse], error) {
	if err := requirePermission(ctx, a.authz, permRequestsGet); err != nil {
		return nil, err
	}
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	canReviewAll, err := canReview(ctx, a.authz, user)
	if err != nil {
		return nil, err
	}
	id, err := parseRequestID(req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	view, err := a.svc.Get(ctx, user.ID, canReviewAll, id)
	if err != nil {
		return nil, requestError(err)
	}
	resp := &portcullisv1.GetAccessRequestResponse{Request: toProtoRequest(view)}
	if view.Payload != nil {
		resp.Payload = toProtoPayload(*view.Payload)
	}
	return connect.NewResponse(resp), nil
}

func (a *AccessRequestsService) List(
	ctx context.Context,
	req *connect.Request[portcullisv1.ListAccessRequestsRequest],
) (*connect.Response[portcullisv1.ListAccessRequestsResponse], error) {
	if err := requirePermission(ctx, a.authz, permRequestsList); err != nil {
		return nil, err
	}
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	canReviewAll, err := canReview(ctx, a.authz, user)
	if err != nil {
		return nil, err
	}
	// Unset descending defaults to newest-first (the AuditSort lesson); an explicit false selects ascending.
	descending := true
	if req.Msg.Descending != nil {
		descending = req.Msg.GetDescending()
	}
	page, err := a.svc.List(ctx, user.ID, canReviewAll, access.ListQuery{
		Page:           int(req.Msg.GetPage()),
		PageSize:       int(req.Msg.GetPageSize()),
		SortDescending: descending,
		State:          access.State(req.Msg.GetState()),
	})
	if err != nil {
		return nil, requestError(err)
	}
	items := make([]*portcullisv1.AccessRequest, 0, len(page.Items))
	for idx := range page.Items {
		items = append(items, toProtoRequest(page.Items[idx]))
	}
	return connect.NewResponse(&portcullisv1.ListAccessRequestsResponse{
		Items:      items,
		Page:       uint32(page.Page),     //nolint:gosec // clamped ≥ 1 by the service
		PageSize:   uint32(page.PageSize), //nolint:gosec // clamped to ≤ 100
		TotalCount: page.TotalCount,
		TotalPages: uint32(page.TotalPages), //nolint:gosec // derived from clamped counts
	}), nil
}

// ListRequestableConnections is gated by requests.create, NOT connections.list: choosing a target is part of making a request, so the seeded requester and approver roles reach it without connection-administration rights (ADR-0008/0018). The response carries no credentials, target coordinates, description, or archive state.
func (a *AccessRequestsService) ListRequestableConnections(
	ctx context.Context,
	_ *connect.Request[portcullisv1.ListRequestableConnectionsRequest],
) (*connect.Response[portcullisv1.ListRequestableConnectionsResponse], error) {
	if err := requirePermission(ctx, a.authz, permRequestsCreate); err != nil {
		return nil, err
	}
	conns, err := a.svc.ListRequestableConnections(ctx)
	if err != nil {
		return nil, requestError(err)
	}
	out := make([]*portcullisv1.RequestableConnection, 0, len(conns))
	for _, c := range conns {
		out = append(out, &portcullisv1.RequestableConnection{
			Id:          string(c.ID),
			DisplayName: c.DisplayName,
			DbType:      c.DBType,
			Environment: c.Environment,
		})
	}
	return connect.NewResponse(&portcullisv1.ListRequestableConnectionsResponse{Connections: out}), nil
}

// requestError maps the access-request use cases' outcomes onto Connect codes. Domain sentinels are safe to surface; anything else collapses to internal. Nothing here echoes SQL or parameter values (PRD §8.1).
func requestError(err error) error {
	switch {
	case errors.Is(err, access.ErrNotFound), errors.Is(err, connection.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, errors.New("request not found"))
	case errors.Is(err, access.ErrConflict):
		return connect.NewError(connect.CodeAborted, errors.New("request changed; refresh and retry"))
	case errors.Is(err, connection.ErrPolicyConflict):
		// The submit's pinned policy version was superseded under the lock — retryable business outcome, not a server fault (ADR-0018).
		return connect.NewError(connect.CodeAborted, errors.New("policy changed; refresh and retry"))
	case errors.Is(err, access.ErrConnectionChanged):
		// The target's configuration was replaced under the lock: what was classified and digested is not the database this would run on.
		return connect.NewError(connect.CodeAborted, errors.New("connection configuration changed; refresh and retry"))
	case errors.Is(err, access.ErrApproverIneligible):
		// The coarse permission gate passed but the under-lock re-check found the approver no longer eligible (deactivated / permission revoked).
		return connect.NewError(connect.CodePermissionDenied, errors.New("permission denied"))
	case errors.Is(err, access.ErrNotDraft),
		errors.Is(err, access.ErrNotPending),
		errors.Is(err, access.ErrNotCancellable),
		errors.Is(err, access.ErrSelfApproval),
		errors.Is(err, access.ErrAlreadyDecided),
		errors.Is(err, access.ErrClassNotAllowed),
		errors.Is(err, access.ErrConnectionArchived):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, access.ErrInvalidPayload),
		errors.Is(err, access.ErrUnclassifiable),
		errors.Is(err, access.ErrReasonRequired),
		errors.Is(err, access.ErrReasonTooLong),
		errors.Is(err, access.ErrInvalidRequest):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}
}

// canReview widens visibility for either requests.approve or requests.reject; resolver errors fail closed (ADR-0018).
func canReview(ctx context.Context, az authorizer, user identity.User) (bool, error) {
	approve, err := hasPermission(ctx, az, user, permRequestsApprove)
	if err != nil {
		return false, err
	}
	if approve {
		return true, nil
	}
	return hasPermission(ctx, az, user, permRequestsReject)
}

// requireUser returns the interceptor-injected user or an Unauthenticated error (a defensive backstop; the interceptor guarantees one).
func requireUser(ctx context.Context) (identity.User, error) {
	user, ok := userFromContext(ctx)
	if !ok {
		return identity.User{}, connect.NewError(connect.CodeUnauthenticated, errAuthRequired)
	}
	return user, nil
}

// parseRequestID normalizes an access-request id to a canonical UUID at the boundary (the id becomes AEAD AAD, like connection ids — ADR-0018/0014).
func parseRequestID(raw string) (access.RequestID, error) {
	id, err := parseConnectionID(raw)
	if err != nil {
		return "", connect.NewError(connect.CodeInvalidArgument, errors.New("invalid request id"))
	}
	return access.RequestID(id), nil
}

func toParams(in []*portcullisv1.TypedParam) []query.Parameter {
	if len(in) == 0 {
		return nil
	}
	out := make([]query.Parameter, 0, len(in))
	for _, p := range in {
		out = append(out, query.Parameter{
			Name:  p.GetName(),
			Value: query.TypedValue{Type: query.ParamType(p.GetType()), Text: p.GetValue()},
		})
	}
	return out
}

func toProtoPayload(p access.Payload) *portcullisv1.AccessRequestPayload {
	params := make([]*portcullisv1.TypedParam, 0, len(p.Params))
	for _, param := range p.Params {
		params = append(params, &portcullisv1.TypedParam{
			Name:  param.Name,
			Type:  string(param.Value.Type),
			Value: param.Value.Text,
		})
	}
	return &portcullisv1.AccessRequestPayload{Title: p.Title, Body: p.Body, Sql: p.SQL, Params: params}
}

func toProtoRequest(v access.RequestView) *portcullisv1.AccessRequest {
	r := v.Request
	out := &portcullisv1.AccessRequest{
		Title:          r.Title,
		Id:             string(r.ID),
		ConnectionId:   string(r.ConnectionID),
		ConnectionName: v.ConnectionName,
		Requester:      &portcullisv1.RequestActor{Id: string(r.RequesterID), Email: v.RequesterEmail, DisplayName: v.RequesterDisplayName},
		State:          toProtoState(r.State),
		EffectiveState: toProtoState(effectiveOr(v)),
		StateReason:    string(effectiveReasonOr(v)),
		RedactedSql:    r.RedactedSQL,
		StatementClass: string(r.Class),
		PolicyVersion:  r.PolicyVersion,
		// The submit-time target snapshot, so a reviewer sees the database this was approved against and not whatever the connection is called today (PRD §4.3; OWASP transaction authorization).
		ConnectionDbType:        r.ConnectionDBType,
		ConnectionFingerprint:   r.ConnectionFingerprint,
		ConnectionConfigVersion: r.ConnectionConfigVersion,
		RequiredApprovals:       uint32(clampNonNeg(r.RequiredApprovals)), //nolint:gosec // 0..100
		ValidApprovals:          uint32(clampNonNeg(v.ValidApprovals)),    //nolint:gosec // ≤ approvals
		Version:                 r.Version,
	}
	for _, a := range v.Approvals {
		pa := &portcullisv1.RequestApproval{
			Approver: &portcullisv1.RequestActor{Id: string(a.Approval.ApproverID), Email: a.ApproverEmail, DisplayName: a.ApproverDisplayName},
			Decision: string(a.Approval.Decision),
			Reason:   a.Approval.Reason,
			Valid:    a.Valid,
		}
		if !a.Approval.DecidedAt.IsZero() {
			pa.DecidedAt = timestamppb.New(a.Approval.DecidedAt)
		}
		out.Approvals = append(out.Approvals, pa)
	}
	if r.SubmittedAt != nil {
		out.SubmittedAt = timestamppb.New(*r.SubmittedAt)
	}
	if r.ExpiresAt != nil {
		out.ExpiresAt = timestamppb.New(*r.ExpiresAt)
	}
	if !r.CreatedAt.IsZero() {
		out.CreatedAt = timestamppb.New(r.CreatedAt)
	}
	if !r.UpdatedAt.IsZero() {
		out.UpdatedAt = timestamppb.New(r.UpdatedAt)
	}
	return out
}

// effectiveOr falls back to the stored state when the view was not stamped (the mutation responses build a view from the bare request).
func effectiveOr(v access.RequestView) access.State {
	if v.EffectiveState != "" {
		return v.EffectiveState
	}
	return v.Request.State
}

func effectiveReasonOr(v access.RequestView) access.Reason {
	if v.EffectiveState != "" {
		return v.EffectiveReason
	}
	return v.Request.Reason
}

func clampNonNeg(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

func toProtoState(s access.State) portcullisv1.AccessRequestState {
	switch s {
	case access.StateDraft:
		return portcullisv1.AccessRequestState_ACCESS_REQUEST_STATE_DRAFT
	case access.StatePending:
		return portcullisv1.AccessRequestState_ACCESS_REQUEST_STATE_PENDING
	case access.StateApproved:
		return portcullisv1.AccessRequestState_ACCESS_REQUEST_STATE_APPROVED
	case access.StateRejected:
		return portcullisv1.AccessRequestState_ACCESS_REQUEST_STATE_REJECTED
	case access.StateExpired:
		return portcullisv1.AccessRequestState_ACCESS_REQUEST_STATE_EXPIRED
	case access.StateCancelled:
		return portcullisv1.AccessRequestState_ACCESS_REQUEST_STATE_CANCELLED
	case access.StateExecuting:
		return portcullisv1.AccessRequestState_ACCESS_REQUEST_STATE_EXECUTING
	case access.StateSucceeded:
		return portcullisv1.AccessRequestState_ACCESS_REQUEST_STATE_SUCCEEDED
	case access.StateFailed:
		return portcullisv1.AccessRequestState_ACCESS_REQUEST_STATE_FAILED
	case access.StateOutcomeUnknown:
		return portcullisv1.AccessRequestState_ACCESS_REQUEST_STATE_OUTCOME_UNKNOWN
	}
	return portcullisv1.AccessRequestState_ACCESS_REQUEST_STATE_UNSPECIFIED
}
