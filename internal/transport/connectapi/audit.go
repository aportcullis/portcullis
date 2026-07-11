package connectapi

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	appaudit "github.com/aportcullis/portcullis/internal/app/audit"
	domainaudit "github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// permAuditList is the permission required to read the audit trail. Per ADR-0008
// the key is named here, at the exact enforcement site, and nowhere else.
const permAuditList identity.Permission = "audit.list"

// AuditService implements the Audit RPC: reading the append-only trail behind an
// audit.list permission check (ADR-0008), with OFFSET pagination (PRD §7.1). The
// authorizer and the read service are injected so the handler holds no storage or
// authz detail of its own.
type AuditService struct {
	authz  authorizer
	reader *appaudit.Service
}

// NewAuditService builds the Audit RPC handler over the authorizer and the audit
// read service.
func NewAuditService(az authorizer, reader *appaudit.Service) *AuditService {
	return &AuditService{authz: az, reader: reader}
}

func (a *AuditService) List(
	ctx context.Context,
	req *connect.Request[portcullisv1.AuditListRequest],
) (*connect.Response[portcullisv1.AuditListResponse], error) {
	if err := requirePermission(ctx, a.authz, permAuditList); err != nil {
		return nil, err
	}
	q := appaudit.Query{
		Page:     int(req.Msg.GetPage()),
		PageSize: int(req.Msg.GetPageSize()),
	}
	// Sort unset ⇒ newest-first (the service default); when set, honor its column
	// and direction. descending=true is the natural log order, so ascending is the
	// explicit opt-in.
	if sort := req.Msg.GetSort(); sort != nil {
		q.SortField = sort.GetField()
		q.SortAscending = !sort.GetDescending()
	}

	page, err := a.reader.List(ctx, q)
	if err != nil {
		if errors.Is(err, appaudit.ErrInvalidSortField) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid sort field"))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}

	events := make([]*portcullisv1.AuditEvent, 0, len(page.Events))
	for _, e := range page.Events {
		events = append(events, toProtoAuditEvent(e))
	}
	return connect.NewResponse(&portcullisv1.AuditListResponse{
		Events:     events,
		Page:       uint32(page.Page),
		PageSize:   uint32(page.PageSize),
		TotalCount: uint64(page.TotalCount),
		TotalPages: uint32(page.TotalPages),
	}), nil
}

func toProtoAuditEvent(e domainaudit.Event) *portcullisv1.AuditEvent {
	pe := &portcullisv1.AuditEvent{
		Id:           e.ID,
		ActorType:    string(e.ActorType),
		ActorService: e.ActorService,
		Action:       string(e.Action),
		TargetType:   e.TargetType,
		TargetId:     e.TargetID,
		Outcome:      string(e.Outcome),
		SourceIp:     e.SourceIP,
		RequestId:    e.RequestID,
	}
	if e.ActorUserID != nil {
		pe.ActorUserId = string(*e.ActorUserID)
	}
	if !e.OccurredAt.IsZero() {
		pe.OccurredAt = timestamppb.New(e.OccurredAt)
	}
	// Metadata is non-sensitive by contract (ADR-0009); surface it as a Struct. A
	// value the well-known type can't represent is dropped rather than failing the
	// read (the read has already succeeded).
	if len(e.Metadata) > 0 {
		if md, err := structpb.NewStruct(e.Metadata); err == nil {
			pe.Metadata = md
		}
	}
	return pe
}
