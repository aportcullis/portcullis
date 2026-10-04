package connectapi

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	appolicy "github.com/aportcullis/portcullis/internal/app/connectionpolicy"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// Permissions gating the ConnectionPolicies RPCs (ADR-0008: named at the exact enforcement site). The keys were seeded by migration 0002.
const (
	permPoliciesGet    identity.Permission = "policies.get"
	permPoliciesUpdate identity.Permission = "policies.update"
)

// policyApp is the slice of the connection-policy application service this handler consumes (DIP/ISP).
type policyApp interface {
	Get(ctx context.Context, id connection.ConnectionID) (connection.Policy, error)
	Update(ctx context.Context, actor identity.UserID, id connection.ConnectionID, p appolicy.UpdateParams) (connection.Policy, error)
}

// ConnectionPoliciesService implements the ConnectionPolicies RPCs (PRD §4.3, ADR-0015): read the current immutable policy version and replace it with the next one under optimistic concurrency.
type ConnectionPoliciesService struct {
	authz authorizer
	svc   policyApp
}

// NewConnectionPoliciesService builds the handler over the authorizer and the policy app service.
func NewConnectionPoliciesService(az authorizer, svc policyApp) *ConnectionPoliciesService {
	return &ConnectionPoliciesService{authz: az, svc: svc}
}

func (c *ConnectionPoliciesService) Get(
	ctx context.Context,
	req *connect.Request[portcullisv1.GetConnectionPolicyRequest],
) (*connect.Response[portcullisv1.GetConnectionPolicyResponse], error) {
	if err := requirePermission(ctx, c.authz, permPoliciesGet); err != nil {
		return nil, err
	}
	id, err := parseConnectionID(req.Msg.GetConnectionId())
	if err != nil {
		return nil, err
	}
	policy, err := c.svc.Get(ctx, id)
	if err != nil {
		return nil, policyError(err)
	}
	return connect.NewResponse(&portcullisv1.GetConnectionPolicyResponse{Policy: toProtoPolicy(policy)}), nil
}

func (c *ConnectionPoliciesService) Update(
	ctx context.Context,
	req *connect.Request[portcullisv1.UpdateConnectionPolicyRequest],
) (*connect.Response[portcullisv1.UpdateConnectionPolicyResponse], error) {
	if err := requirePermission(ctx, c.authz, permPoliciesUpdate); err != nil {
		return nil, err
	}
	user, ok := userFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errAuthRequired)
	}
	id, err := parseConnectionID(req.Msg.GetConnectionId())
	if err != nil {
		return nil, err
	}
	// All three classes are REQUIRED: the update is a full replacement, and nil-coalescing an omitted class to {false, 0} would silently reset its kept quorum — a later re-enable would then auto-approve (ADR-0015).
	if req.Msg.GetRead() == nil || req.Msg.GetWrite() == nil || req.Msg.GetDdl() == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("read, write, and ddl class policies are required"))
	}
	// Wire uints are bounded far below the int32/int64 domain checks; anything out of the ADR-0015 ranges fails NewPolicy as ErrInvalidPolicy.
	params := appolicy.UpdateParams{
		ExpectedVersion:     req.Msg.GetExpectedVersion(),
		Read:                toClassRuleInput(req.Msg.GetRead()),
		Write:               toClassRuleInput(req.Msg.GetWrite()),
		DDL:                 toClassRuleInput(req.Msg.GetDdl()),
		QueryTimeoutSeconds: int(req.Msg.GetQueryTimeoutSeconds()),
		MaxRows:             int(req.Msg.GetMaxRows()),
		MaxResultBytes:      int64(req.Msg.GetMaxResultBytes()), //nolint:gosec // domain rejects > 64 MiB before use
	}
	policy, err := c.svc.Update(ctx, user.ID, id, params)
	if err != nil {
		return nil, policyError(err)
	}
	return connect.NewResponse(&portcullisv1.UpdateConnectionPolicyResponse{Policy: toProtoPolicy(policy)}), nil
}

// policyError maps the policy use cases' outcomes onto Connect codes. Domain sentinels are safe to surface; anything else collapses to a generic internal error.
func policyError(err error) error {
	switch {
	case errors.Is(err, connection.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, errors.New("connection not found"))
	case errors.Is(err, connection.ErrArchived):
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("connection is archived"))
	case errors.Is(err, connection.ErrPolicyConflict):
		return connect.NewError(connect.CodeAborted, errors.New("policy changed; refresh and retry"))
	case errors.Is(err, connection.ErrInvalidPolicy):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return newInternalError(err)
	}
}

func toClassRuleInput(p *portcullisv1.ClassPolicy) appolicy.ClassRuleInput {
	return appolicy.ClassRuleInput{
		Allowed:           p.GetAllowed(),
		RequiredApprovals: int(p.GetRequiredApprovals()),
	}
}

func toProtoClassPolicy(r connection.ClassRule) *portcullisv1.ClassPolicy {
	return &portcullisv1.ClassPolicy{
		Allowed:           r.Allowed,
		RequiredApprovals: uint32(r.RequiredApprovals), //nolint:gosec // domain-checked 0..100
	}
}

func toProtoPolicy(p connection.Policy) *portcullisv1.ConnectionPolicy {
	pp := &portcullisv1.ConnectionPolicy{
		ConnectionId:        string(p.ConnectionID),
		Version:             p.Version,
		Read:                toProtoClassPolicy(p.Read),
		Write:               toProtoClassPolicy(p.Write),
		Ddl:                 toProtoClassPolicy(p.DDL),
		QueryTimeoutSeconds: uint32(p.Limits.QueryTimeoutSeconds), //nolint:gosec // domain-checked 1..300
		MaxRows:             uint32(p.Limits.MaxRows),             //nolint:gosec // domain-checked 1..10000
		MaxResultBytes:      uint64(p.Limits.MaxResultBytes),      //nolint:gosec // domain-checked ≤ 64 MiB
	}
	if !p.CreatedAt.IsZero() {
		pp.CreatedAt = timestamppb.New(p.CreatedAt)
	}
	return pp
}
