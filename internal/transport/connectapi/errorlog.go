package connectapi

import (
	"context"
	"fmt"
	"log/slog"

	"connectrpc.com/connect"
)

// isServerFault reports whether a Connect code denotes a server-side failure worth
// logging. Client-fault codes (Unauthenticated, PermissionDenied, InvalidArgument,
// ResourceExhausted, …) are expected outcomes and would only be noise.
func isServerFault(code connect.Code) bool {
	switch code {
	case connect.CodeInternal, connect.CodeUnavailable, connect.CodeUnknown, connect.CodeDataLoss:
		return true
	default:
		return false
	}
}

// NewErrorLogInterceptor logs any RPC that fails with a server-fault code, by
// procedure, code, and error TYPE only — never the message body, which may carry
// sensitive detail. Returned errors are otherwise invisible server-side
// (NewRecoverOption only fires on panics), so without this an internal authz error
// (a typo'd permission key) or a DB blip leaves no trace to diagnose (ADR-0008/0010).
// Wire it outermost of the interceptors so it observes errors from every inner
// interceptor and the handler.
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
