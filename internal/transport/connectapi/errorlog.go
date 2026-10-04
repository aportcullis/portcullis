package connectapi

import (
	"context"
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

// ErrorLogInterceptor logs server faults of unary and streaming RPCs with their procedure, code, and classified cause, never their messages.
type ErrorLogInterceptor struct {
	logger *slog.Logger
}

// NewErrorLogInterceptor builds the error log interceptor. Wire it outermost to observe all interceptor failures.
func NewErrorLogInterceptor(logger *slog.Logger) *ErrorLogInterceptor {
	return &ErrorLogInterceptor{logger: logger}
}

// WrapUnary logs a unary server fault.
func (i *ErrorLogInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		resp, err := next(ctx, req)
		i.logServerFault(ctx, req.Spec().Procedure, err)
		return resp, err
	}
}

// WrapStreamingClient leaves outbound calls unchanged.
func (i *ErrorLogInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// WrapStreamingHandler logs the terminal server fault of a streaming handler.
func (i *ErrorLogInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		err := next(ctx, conn)
		i.logServerFault(ctx, conn.Spec().Procedure, err)
		return err
	}
}

// logServerFault writes one line for a server-fault error and ignores success and client faults.
func (i *ErrorLogInterceptor) logServerFault(ctx context.Context, procedure string, err error) {
	if err == nil {
		return
	}
	code := connect.CodeOf(err)
	if !isServerFault(code) {
		return
	}
	i.logger.ErrorContext(ctx, "rpc failed", append([]any{"procedure", procedure, "code", code.String()}, classifyErrorCause(err)...)...)
}
