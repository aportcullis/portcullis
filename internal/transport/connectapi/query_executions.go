package connectapi

import (
	"connectrpc.com/connect"
	"context"
	"errors"
	portcullisv1 "github.com/aportcullis/portcullis/gen/portcullis/v1"
	"github.com/aportcullis/portcullis/internal/domain/access"
	"github.com/aportcullis/portcullis/internal/domain/identity"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"google.golang.org/protobuf/types/known/timestamppb"
	"io"
	"time"
)

type executionApp interface {
	Execute(context.Context, identity.UserID, access.RequestID) (access.ExecutionCompletion, error)
	Get(context.Context, identity.UserID, access.RequestID) (access.ExecutionCompletion, error)
	Cancel(context.Context, identity.UserID, access.RequestID) error
	ResultOwner(context.Context, identity.UserID, access.RequestID) (identity.OrganizationID, string, error)
}
type resultApp interface {
	Page(context.Context, identity.OrganizationID, identity.UserID, string, query.ResultPageQuery) (query.ResultPage, error)
	ExportCSV(context.Context, identity.OrganizationID, identity.UserID, string, io.Writer) error
}

// QueryExecutionsService exposes requester-only execution and cached results.
type QueryExecutionsService struct {
	authz      authorizer
	executions executionApp
	results    resultApp
}

// NewQueryExecutionsService wires the governed execution and result services.
func NewQueryExecutionsService(authz authorizer, executions executionApp, results resultApp) *QueryExecutionsService {
	return &QueryExecutionsService{authz: authz, executions: executions, results: results}
}

func (s *QueryExecutionsService) Execute(ctx context.Context, req *connect.Request[portcullisv1.ExecuteQueryRequest]) (*connect.Response[portcullisv1.QueryExecution], error) {
	if len(req.Msg.ProtoReflect().GetUnknown()) != 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid execution message"))
	}
	if err := requirePermission(ctx, s.authz, "requests.execute"); err != nil {
		return nil, err
	}
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseRequestID(req.Msg.GetRequestId())
	if err != nil {
		return nil, err
	}
	completion, err := s.executions.Execute(ctx, user.ID, id)
	if err != nil {
		return nil, executionError(err)
	}
	return connect.NewResponse(toProtoExecution(completion)), nil
}

func (s *QueryExecutionsService) Get(ctx context.Context, req *connect.Request[portcullisv1.GetQueryExecutionRequest]) (*connect.Response[portcullisv1.QueryExecution], error) {
	if err := requirePermission(ctx, s.authz, "requests.get"); err != nil {
		return nil, err
	}
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseRequestID(req.Msg.GetRequestId())
	if err != nil {
		return nil, err
	}
	completion, err := s.executions.Get(ctx, user.ID, id)
	if err != nil {
		return nil, executionError(err)
	}
	return connect.NewResponse(toProtoExecution(completion)), nil
}

func (s *QueryExecutionsService) Cancel(ctx context.Context, req *connect.Request[portcullisv1.CancelQueryExecutionRequest]) (*connect.Response[portcullisv1.CancelQueryExecutionResponse], error) {
	if err := requirePermission(ctx, s.authz, "requests.execute"); err != nil {
		return nil, err
	}
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseRequestID(req.Msg.GetRequestId())
	if err != nil {
		return nil, err
	}
	if err := s.executions.Cancel(ctx, user.ID, id); err != nil {
		return nil, executionError(err)
	}
	return connect.NewResponse(&portcullisv1.CancelQueryExecutionResponse{}), nil
}

func (s *QueryExecutionsService) GetResult(ctx context.Context, req *connect.Request[portcullisv1.GetQueryResultRequest]) (*connect.Response[portcullisv1.QueryResultPage], error) {
	if err := requirePermission(ctx, s.authz, "requests.get"); err != nil {
		return nil, err
	}
	user, err := requireUser(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseRequestID(req.Msg.GetRequestId())
	if err != nil {
		return nil, err
	}
	org, resultID, err := s.executions.ResultOwner(ctx, user.ID, id)
	if err != nil {
		return nil, executionError(err)
	}
	sortColumn, filterColumn := -1, -1
	if req.Msg.SortColumn != nil {
		sortColumn = int(*req.Msg.SortColumn)
	}
	if req.Msg.FilterColumn != nil {
		filterColumn = int(*req.Msg.FilterColumn)
	}
	page, err := s.results.Page(ctx, org, user.ID, resultID, query.ResultPageQuery{Page: int(req.Msg.Page), PageSize: int(req.Msg.PageSize), SortColumn: sortColumn, Descending: req.Msg.Descending, FilterColumn: filterColumn, Filter: req.Msg.Filter})
	if err != nil {
		return nil, executionError(err)
	}
	return connect.NewResponse(toProtoResultPage(page)), nil
}

func (s *QueryExecutionsService) ExportCSV(ctx context.Context, req *connect.Request[portcullisv1.ExportQueryCSVRequest], stream *connect.ServerStream[portcullisv1.QueryCSVChunk]) error {
	if err := requirePermission(ctx, s.authz, "requests.get"); err != nil {
		return err
	}
	user, err := requireUser(ctx)
	if err != nil {
		return err
	}
	id, err := parseRequestID(req.Msg.GetRequestId())
	if err != nil {
		return err
	}
	org, resultID, err := s.executions.ResultOwner(ctx, user.ID, id)
	if err != nil {
		return executionError(err)
	}
	writer := csvStreamWriter{write: func(data []byte) error {
		if err := requirePermission(ctx, s.authz, "requests.get"); err != nil {
			return err
		}
		return stream.Send(&portcullisv1.QueryCSVChunk{Data: data})
	}}
	return executionError(s.results.ExportCSV(ctx, org, user.ID, resultID, writer))
}

type csvStreamWriter struct{ write func([]byte) error }

func (w csvStreamWriter) Write(data []byte) (int, error) {
	for offset := 0; offset < len(data); {
		end := min(offset+16384, len(data))
		if err := w.write(data[offset:end]); err != nil {
			return offset, err
		}
		offset = end
	}
	return len(data), nil
}

func executionError(err error) error {
	if err == nil {
		return nil
	}
	var connected *connect.Error
	if errors.As(err, &connected) {
		return connected
	}
	switch {
	case errors.Is(err, access.ErrTargetUnavailable):
		return connect.NewError(connect.CodeUnavailable, errors.New("target temporarily unavailable"))
	case errors.Is(err, query.ErrResultBusy):
		response := connect.NewError(connect.CodeResourceExhausted, errors.New("temporarily busy"))
		response.Meta().Set("Retry-After", "1")
		return response
	case errors.Is(err, query.ErrResultUnavailable):
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("result unavailable or expired"))
	case errors.Is(err, query.ErrInvalidResultQuery):
		return connect.NewError(connect.CodeInvalidArgument, errors.New("invalid result query"))
	case errors.Is(err, access.ErrNotExecutable), errors.Is(err, access.ErrLeaseLost), errors.Is(err, access.ErrPayloadIntegrity):
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("request cannot be executed"))
	default:
		return requestError(err)
	}
}

func toProtoExecution(c access.ExecutionCompletion) *portcullisv1.QueryExecution {
	available := c.ResultID != "" && c.ResultExpiresAt != nil && time.Now().Before(*c.ResultExpiresAt)
	result := &portcullisv1.QueryExecution{State: toProtoState(c.State), RowsAffected: c.RowsAffected, DurationMs: c.DurationMilliseconds, ResultAvailable: available, RowCount: c.RowCount, ByteCount: c.ByteCount, Truncated: c.Truncated}
	if c.ResultExpiresAt != nil {
		result.ResultExpiresAt = timestamppb.New(*c.ResultExpiresAt)
	}
	return result
}

var logicalTypes = map[query.LogicalType]portcullisv1.LogicalType{
	query.LogicalString: portcullisv1.LogicalType_LOGICAL_TYPE_STRING, query.LogicalBool: portcullisv1.LogicalType_LOGICAL_TYPE_BOOL, query.LogicalInt: portcullisv1.LogicalType_LOGICAL_TYPE_INT, query.LogicalDecimal: portcullisv1.LogicalType_LOGICAL_TYPE_DECIMAL, query.LogicalFloat: portcullisv1.LogicalType_LOGICAL_TYPE_FLOAT, query.LogicalBytes: portcullisv1.LogicalType_LOGICAL_TYPE_BYTES, query.LogicalDate: portcullisv1.LogicalType_LOGICAL_TYPE_DATE, query.LogicalTime: portcullisv1.LogicalType_LOGICAL_TYPE_TIME, query.LogicalTimestamp: portcullisv1.LogicalType_LOGICAL_TYPE_TIMESTAMP, query.LogicalTimestamptz: portcullisv1.LogicalType_LOGICAL_TYPE_TIMESTAMPTZ, query.LogicalJSON: portcullisv1.LogicalType_LOGICAL_TYPE_JSON, query.LogicalUUID: portcullisv1.LogicalType_LOGICAL_TYPE_UUID, query.LogicalArray: portcullisv1.LogicalType_LOGICAL_TYPE_ARRAY, query.LogicalUnknown: portcullisv1.LogicalType_LOGICAL_TYPE_UNKNOWN,
}

func toProtoResultPage(page query.ResultPage) *portcullisv1.QueryResultPage {
	response := &portcullisv1.QueryResultPage{Page: int32(page.Page), PageSize: int32(page.PageSize), TotalCount: page.TotalCount, TotalPages: int32(page.TotalPages), Truncated: page.Truncated, ExpiresAt: timestamppb.New(page.ExpiresAt)}
	for _, column := range page.Columns {
		response.Columns = append(response.Columns, &portcullisv1.ColumnMeta{Name: column.Name, LogicalType: logicalTypes[column.Logical], DbTypeName: column.DBTypeName, Nullable: column.Nullable})
	}
	for _, row := range page.Rows {
		converted := &portcullisv1.QueryResultRow{}
		for _, cell := range row {
			converted.Cells = append(converted.Cells, toProtoCell(cell))
		}
		response.Rows = append(response.Rows, converted)
	}
	return response
}
func toProtoCell(cell query.CellValue) *portcullisv1.CellValue {
	value := &portcullisv1.CellValue{}
	switch cell.Kind {
	case query.CellNull:
		value.Kind = &portcullisv1.CellValue_IsNull{IsNull: true}
	case query.CellBool:
		value.Kind = &portcullisv1.CellValue_BoolValue{BoolValue: cell.Bool}
	case query.CellInt:
		value.Kind = &portcullisv1.CellValue_IntValue{IntValue: cell.Text}
	case query.CellDecimal:
		value.Kind = &portcullisv1.CellValue_DecimalValue{DecimalValue: cell.Text}
	case query.CellFloat:
		value.Kind = &portcullisv1.CellValue_DoubleValue{DoubleValue: cell.Float}
	case query.CellBytes:
		value.Kind = &portcullisv1.CellValue_BytesValue{BytesValue: cell.Bytes}
	case query.CellTemporal:
		value.Kind = &portcullisv1.CellValue_TemporalValue{TemporalValue: cell.Text}
	default:
		value.Kind = &portcullisv1.CellValue_StringValue{StringValue: cell.Text}
	}
	return value
}
