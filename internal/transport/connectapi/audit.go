package connectapi

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	appaudit "github.com/aportcullis/portcullis/internal/app/audit"
	domainaudit "github.com/aportcullis/portcullis/internal/domain/audit"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// Per ADR-0008 each key is named at its exact enforcement site: list sees only
// collection summaries, while get unlocks one event's correlation/detail data.
const (
	permAuditList identity.Permission = "audit.list"
	permAuditGet  identity.Permission = "audit.get"
)

// AuditService implements the Audit RPC: reading the append-only trail behind an
// audit.list permission check (ADR-0008), with OFFSET pagination (PRD §7.1). The
// authorizer and the read service are injected so the handler holds no storage or
// authz detail of its own.
type AuditService struct {
	authz  authorizer
	reader auditReader
}

// auditReader is the slice of the audit read service this handler consumes
// (DIP/ISP — depend on the called methods, not the concrete *appaudit.Service).
type auditReader interface {
	List(ctx context.Context, q appaudit.Query) (domainaudit.EventPage, error)
	Get(ctx context.Context, id string) (domainaudit.Event, error)
}

// NewAuditService builds the Audit RPC handler over the authorizer and the audit
// read service.
func NewAuditService(az authorizer, reader auditReader) *AuditService {
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
	// Sort unset ⇒ newest-first (the service default); when set, honor its
	// column, and its direction only when the optional descending was actually
	// sent: a field-only sort keeps the documented descending default, and only
	// an explicit descending=false opts into ascending (a plain proto3 bool
	// could not tell those apart — external review).
	if sort := req.Msg.GetSort(); sort != nil {
		q.SortField = sort.GetField()
		q.SortAscending = sort.Descending != nil && !sort.GetDescending()
	}

	page, err := a.reader.List(ctx, q)
	if err != nil {
		if errors.Is(err, appaudit.ErrInvalidSortField) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid sort field"))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}

	events := make([]*portcullisv1.AuditEventSummary, 0, len(page.Events))
	for _, e := range page.Events {
		events = append(events, toProtoAuditEventSummary(e))
	}
	return connect.NewResponse(&portcullisv1.AuditListResponse{
		Events:     events,
		Page:       uint32(page.Page),
		PageSize:   uint32(page.PageSize),
		TotalCount: uint64(page.TotalCount),
		TotalPages: uint32(page.TotalPages),
	}), nil
}

func (a *AuditService) Get(
	ctx context.Context,
	req *connect.Request[portcullisv1.GetAuditEventRequest],
) (*connect.Response[portcullisv1.GetAuditEventResponse], error) {
	if err := requirePermission(ctx, a.authz, permAuditGet); err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(req.Msg.GetId()); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid audit event id"))
	}
	event, err := a.reader.Get(ctx, req.Msg.GetId())
	if err != nil {
		if errors.Is(err, domainaudit.ErrEventNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("audit event not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}
	return connect.NewResponse(&portcullisv1.GetAuditEventResponse{Event: toProtoAuditEvent(event)}), nil
}

func toProtoAuditEventSummary(e domainaudit.Event) *portcullisv1.AuditEventSummary {
	pe := &portcullisv1.AuditEventSummary{
		Id:         e.ID,
		ActorType:  string(e.ActorType),
		Action:     string(e.Action),
		TargetType: e.TargetType,
		TargetId:   e.TargetID,
		Outcome:    string(e.Outcome),
	}
	if !e.OccurredAt.IsZero() {
		pe.OccurredAt = timestamppb.New(e.OccurredAt)
	}
	return pe
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
