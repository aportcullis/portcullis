package connectapi

import (
	"context"
	"fmt"
	"log/slog"

	"connectrpc.com/connect"
)

// isServerFault reports whether a Connect code denotes a server-side failure worth logging. Client-fault codes (Unauthenticated, PermissionDenied, InvalidArgument, ResourceExhausted, …) are expected outcomes and would only be noise.
func isServerFault(code connect.Code) bool {
	switch code {
	case connect.CodeInternal, connect.CodeUnavailable, connect.CodeUnknown, connect.CodeDataLoss:
		return true
	default:
		return false
	}
}

// NewErrorLogInterceptor logs server-fault procedure, code, and error type without sensitive messages. Wire it outermost to observe all interceptor failures.
func NewErrorLogInterceptor(logger *slog.Logger) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			resp, err := next(ctx, req)
			if err != nil {
				if code := connect.CodeOf(err); isServerFault(code) {
					logger.ErrorContext(ctx, "rpc failed",
						"procedure", req.Spec().Procedure,
						"code", code.String(),
						"error_type", fmt.Sprintf("%T", err))
				}
			}
			return resp, err
		}
	}
}
