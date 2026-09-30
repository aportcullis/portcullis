package connectapi

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	appconn "github.com/aportcullis/portcullis/internal/app/connection"
	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/identity"
)

// parseConnectionID rejects malformed UUIDs and canonicalizes accepted encodings before they become AEAD associated data.
func parseConnectionID(raw string) (connection.ConnectionID, error) {
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return "", connect.NewError(connect.CodeInvalidArgument, errors.New("invalid connection id"))
	}
	return connection.ConnectionID(parsed.String()), nil
}

// Permissions gating the Connections RPCs. Per ADR-0008 each key is named here, at its exact enforcement site, and nowhere else. The catalog's delete verb gates Archive — "delete" IS archive; no hard delete exists (PRD §4.3).
const (
	permConnectionsList   identity.Permission = "connections.list"
	permConnectionsGet    identity.Permission = "connections.get"
	permConnectionsCreate identity.Permission = "connections.create"
	permConnectionsUpdate identity.Permission = "connections.update"
	permConnectionsTest   identity.Permission = "connections.test"
	permConnectionsDelete identity.Permission = "connections.delete"
)

// connectionApp is the slice of the connection application service this handler consumes (DIP/ISP — depend on the called methods, not the concrete *appconn.Service).
type connectionApp interface {
	List(ctx context.Context, includeArchived bool) ([]connection.Connection, error)
	Get(ctx context.Context, id connection.ConnectionID) (connection.Connection, error)
	Create(ctx context.Context, actor identity.UserID, p appconn.CreateParams) (connection.Connection, error)
	Update(ctx context.Context, actor identity.UserID, id connection.ConnectionID, p appconn.UpdateParams) (connection.Connection, error)
	Archive(ctx context.Context, actor identity.UserID, id connection.ConnectionID) (connection.Connection, error)
	TestByConfig(ctx context.Context, actor identity.UserID, cfg appconn.ConfigInput) error
	TestByID(ctx context.Context, actor identity.UserID, id connection.ConnectionID) error
}

// ConnectionsService implements the Connections RPCs (PRD §4.1/§7.2, ADR-0014): admin-gated registration with a mandatory server-side test-before-save, rename/config updates, in-band connection tests, and archive. Responses never carry a credential or DSN — the read model has no such fields.
type ConnectionsService struct {
	authz authorizer
	svc   connectionApp
}

// NewConnectionsService builds the Connections RPC handler over the authorizer and the connection app service.
func NewConnectionsService(az authorizer, svc connectionApp) *ConnectionsService {
	return &ConnectionsService{authz: az, svc: svc}
}

func (c *ConnectionsService) List(
	ctx context.Context,
	req *connect.Request[portcullisv1.ListConnectionsRequest],
) (*connect.Response[portcullisv1.ListConnectionsResponse], error) {
	if err := requirePermission(ctx, c.authz, permConnectionsList); err != nil {
		return nil, err
	}
	conns, err := c.svc.List(ctx, req.Msg.GetIncludeArchived())
	if err != nil {
		return nil, connectionError(err)
	}
	out := make([]*portcullisv1.ConnectionSummary, 0, len(conns))
	for _, conn := range conns {
		out = append(out, toProtoConnectionSummary(conn))
	}
	return connect.NewResponse(&portcullisv1.ListConnectionsResponse{Connections: out}), nil
}

func (c *ConnectionsService) Get(
	ctx context.Context,
	req *connect.Request[portcullisv1.GetConnectionRequest],
) (*connect.Response[portcullisv1.GetConnectionResponse], error) {
	if err := requirePermission(ctx, c.authz, permConnectionsGet); err != nil {
		return nil, err
	}
	id, err := parseConnectionID(req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	conn, err := c.svc.Get(ctx, id)
	if err != nil {
		return nil, connectionError(err)
	}
	return connect.NewResponse(&portcullisv1.GetConnectionResponse{Connection: toProtoConnection(conn)}), nil
}

func (c *ConnectionsService) Create(
	ctx context.Context,
	req *connect.Request[portcullisv1.CreateConnectionRequest],
) (*connect.Response[portcullisv1.CreateConnectionResponse], error) {
	if err := requirePermission(ctx, c.authz, permConnectionsCreate); err != nil {
		return nil, err
	}
	user, ok := userFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errAuthRequired)
	}
	conn, err := c.svc.Create(ctx, user.ID, appconn.CreateParams{
		DisplayName: req.Msg.GetDisplayName(),
		Environment: req.Msg.GetEnvironment(),
		Description: req.Msg.GetDescription(),
		Config:      toConfigInput(req.Msg.GetConfig()),
	})
	if err != nil {
		return nil, connectionError(err)
	}
	return connect.NewResponse(&portcullisv1.CreateConnectionResponse{Connection: toProtoConnectionSummary(conn)}), nil
}

func (c *ConnectionsService) Update(
	ctx context.Context,
	req *connect.Request[portcullisv1.UpdateConnectionRequest],
) (*connect.Response[portcullisv1.UpdateConnectionResponse], error) {
	if err := requirePermission(ctx, c.authz, permConnectionsUpdate); err != nil {
		return nil, err
	}
	user, ok := userFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errAuthRequired)
	}
	id, err := parseConnectionID(req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	// The descriptor token is required, not defaulted: version 0 does not exist (the column starts at 1 and a CHECK keeps it positive), so accepting it would be accepting an unguarded write — the lost update ADR-0014 defines this column to prevent.
	expectedVersion := req.Msg.GetExpectedVersion()
	if expectedVersion <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("expected_version is required"))
	}
	params := appconn.UpdateParams{
		DisplayName:     req.Msg.GetDisplayName(),
		Environment:     req.Msg.GetEnvironment(),
		ExpectedVersion: expectedVersion,
	}
	if req.Msg.Description != nil { // presence-tracked: absent keeps the stored text
		desc := req.Msg.GetDescription()
		params.Description = &desc
	}
	if cfg := req.Msg.GetConfig(); cfg != nil {
		in := toConfigInput(cfg)
		params.Config = &in
	}
	conn, err := c.svc.Update(ctx, user.ID, id, params)
	if err != nil {
		return nil, connectionError(err)
	}
	return connect.NewResponse(&portcullisv1.UpdateConnectionResponse{Connection: toProtoConnectionSummary(conn)}), nil
}

func (c *ConnectionsService) Test(
	ctx context.Context,
	req *connect.Request[portcullisv1.TestConnectionRequest],
) (*connect.Response[portcullisv1.TestConnectionResponse], error) {
	if err := requirePermission(ctx, c.authz, permConnectionsTest); err != nil {
		return nil, err
	}
	user, ok := userFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errAuthRequired)
	}

	var err error
	switch target := req.Msg.GetTarget().(type) {
	case *portcullisv1.TestConnectionRequest_Config:
		err = c.svc.TestByConfig(ctx, user.ID, toConfigInput(target.Config))
	case *portcullisv1.TestConnectionRequest_Id:
		id, perr := parseConnectionID(target.Id)
		if perr != nil {
			return nil, perr
		}
		err = c.svc.TestByID(ctx, user.ID, id)
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("a config or a connection id is required"))
	}
	if err == nil {
		return connect.NewResponse(&portcullisv1.TestConnectionResponse{Ok: true}), nil
	}
	// A failed test is an in-band result, not an RPC error; the message is the classified bucket only (ADR-0014), safe by construction.
	var te *connection.TestError
	if errors.As(err, &te) {
		return connect.NewResponse(&portcullisv1.TestConnectionResponse{Ok: false, Message: string(te.Bucket)}), nil
	}
	return nil, connectionError(err)
}

func (c *ConnectionsService) Archive(
	ctx context.Context,
	req *connect.Request[portcullisv1.ArchiveConnectionRequest],
) (*connect.Response[portcullisv1.ArchiveConnectionResponse], error) {
	if err := requirePermission(ctx, c.authz, permConnectionsDelete); err != nil {
		return nil, err
	}
	user, ok := userFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errAuthRequired)
	}
	id, err := parseConnectionID(req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	conn, err := c.svc.Archive(ctx, user.ID, id)
	if err != nil {
		return nil, connectionError(err)
	}
	return connect.NewResponse(&portcullisv1.ArchiveConnectionResponse{Connection: toProtoConnectionSummary(conn)}), nil
}

var errAuthRequired = errors.New("authentication required")

// connectionError maps the connection use cases' outcomes onto Connect codes. Domain sentinels and test buckets are safe to surface; anything else collapses to a generic internal error (no driver or storage detail leaks).
func connectionError(err error) error {
	var te *connection.TestError
	switch {
	case errors.Is(err, connection.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, errors.New("connection not found"))
	case errors.Is(err, connection.ErrNameTaken):
		return connect.NewError(connect.CodeAlreadyExists, errors.New("display name already in use"))
	case errors.Is(err, connection.ErrArchived), errors.Is(err, connection.ErrAlreadyArchived):
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("connection is archived"))
	case errors.Is(err, connection.ErrExecutionInFlight):
		// §4.3: archive waits for the in-flight execution — a business refusal the caller can act on, not a server fault.
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("an execution is in flight; retry once it settles"))
	case errors.Is(err, connection.ErrConflict):
		return connect.NewError(connect.CodeAborted, errors.New("connection changed; refresh and retry"))
	case errors.As(err, &te):
		// Create/Update refuse to persist on a failed test (PRD §7.2); the bucket message is the whole story.
		return connect.NewError(connect.CodeFailedPrecondition, te)
	case errors.Is(err, connection.ErrInvalidTLSMode),
		errors.Is(err, connection.ErrInvalidTarget),
		errors.Is(err, connection.ErrInvalidDisplayName),
		errors.Is(err, connection.ErrInvalidEnvironment),
		errors.Is(err, connection.ErrInvalidDescription),
		errors.Is(err, connection.ErrInvalidCredential),
		errors.Is(err, connection.ErrUnsupportedDBType),
		errors.Is(err, connection.ErrInvalidConnection):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}
}

func toConfigInput(cfg *portcullisv1.ConnectionConfigInput) appconn.ConfigInput {
	return appconn.ConfigInput{
		Host:     cfg.GetHost(),
		Port:     int(cfg.GetPort()),
		Database: cfg.GetDatabase(),
		User:     cfg.GetUser(),
		Password: cfg.GetPassword(),
		TLSMode:  cfg.GetTlsMode(),
	}
}

func toProtoConnection(c connection.Connection) *portcullisv1.Connection {
	pc := &portcullisv1.Connection{
		Id:                string(c.ID),
		DisplayName:       c.DisplayName,
		DbType:            string(c.DBType),
		Environment:       string(c.Environment),
		Description:       c.Description,
		Host:              c.Target.Host,
		Port:              uint32(c.Target.Port),
		Database:          c.Target.DatabaseName,
		TlsMode:           string(c.TLSMode),
		TargetFingerprint: c.Fingerprint,
		Version:           c.Version,
	}
	if !c.CreatedAt.IsZero() {
		pc.CreatedAt = timestamppb.New(c.CreatedAt)
	}
	if !c.UpdatedAt.IsZero() {
		pc.UpdatedAt = timestamppb.New(c.UpdatedAt)
	}
	if c.ArchivedAt != nil {
		pc.ArchivedAt = timestamppb.New(*c.ArchivedAt)
	}
	return pc
}

func toProtoConnectionSummary(c connection.Connection) *portcullisv1.ConnectionSummary {
	pc := &portcullisv1.ConnectionSummary{
		Id:          string(c.ID),
		DisplayName: c.DisplayName,
		DbType:      string(c.DBType),
		Environment: string(c.Environment),
		Description: c.Description,
		Version:     c.Version,
	}
	if c.ArchivedAt != nil {
		pc.ArchivedAt = timestamppb.New(*c.ArchivedAt)
	}
	return pc
}
